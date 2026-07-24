package varnishcachelogreceiver

import (
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	varnishlog "gitlab.com/uplex/varnish/varnishapi/pkg/log"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

// splitPayload returns the space-separated fields of rec.Payload.
// It returns an "invalid tag" error when the field count is outside [min, max].
// Pass max = -1 to skip the upper bound check. Pass min = 0 to skip the lower bound.
func splitPayload(rec varnishlog.Record, min, max int) ([]string, error) {
	parts := strings.Fields(rec.Payload.String())
	n := len(parts)
	if min > 0 && n < min {
		return nil, fmt.Errorf("invalid tag: %s", rec.Tag.String())
	}
	if max >= 0 && n > max {
		return nil, fmt.Errorf("invalid tag: %s", rec.Tag.String())
	}
	return parts, nil
}

type varnishTransactionLink struct {
	VXID   uint64
	Type   string
	Reason string
}

type varnishTransactionEvent struct {
	Timestamp time.Time
	Name      string
}

type varnishTransactionStorage struct {
	Name string
	Type string
}

type varnishTransactionClientConn struct {
	Addr string
	Port uint64
	Sock string
}

type varnishTransactionBackendConn struct {
	Addr       string
	Port       uint64
	ConnReused bool
	Name       string
}

type varnishTransactionReq struct {
	URL          string
	Method       string
	Proto        string
	ProtoVersion string
	HdrBytes     uint64
	BodyBytes    uint64
	Headers      map[string]string
}

type varnishTransactionResp struct {
	Status       uint64
	StatusReason string
	Filters      []string
	Proto        string
	ProtoVersion string
	HdrBytes     uint64
	BodyBytes    uint64
	Headers      map[string]string
}

type varnishTransaction struct {
	VXID       uint64
	VXIDParent uint64
	VCL        string
	Handling   string
	Reason     string
	Req        varnishTransactionReq
	Resp       varnishTransactionResp
	Side       string
	Client     varnishTransactionClientConn
	Backend    varnishTransactionBackendConn
	Storage    *varnishTransactionStorage
	Logs       []string
	Errors     []string
	Links      []varnishTransactionLink
	Events     []varnishTransactionEvent
	// SLT_Begin: sess, req, bereq
	Type  string
	Level uint64
}

type varnishTagTransformerFunc func(vtx *varnishTransaction, rec varnishlog.Record) error

func transformVCLLog(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) > 0 {
		tx.Logs = append(tx.Logs, trimUnprintableChars(rec.Payload.String()))
	}
	return nil
}

func transformReqURL(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) == 0 {
		return nil
	}
	tx.Req.URL = trimUnprintableChars(parts[0])
	return nil
}

func transformReqMethod(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) == 0 {
		return nil
	}
	tx.Req.Method = trimUnprintableChars(parts[0])
	return nil
}

func transformVCLUse(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) == 0 {
		return nil
	}
	tx.VCL = trimUnprintableChars(parts[0])
	return nil
}

func transformLink(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) < 3 {
		return fmt.Errorf("Invalid link received: %s", rec.Payload.String())
	}
	id, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return err
	}
	tx.Links = append(tx.Links, varnishTransactionLink{
		VXID:   id,
		Type:   trimUnprintableChars(parts[0]),
		Reason: trimUnprintableChars(parts[2]),
	})
	return nil
}

func transformStorage(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) != 2 {
		return fmt.Errorf("Invalid storage received: %s", rec.Payload.String())
	}
	tx.Storage = &varnishTransactionStorage{
		Name: trimUnprintableChars(parts[0]),
		Type: trimUnprintableChars(parts[1]),
	}
	return nil
}

func transformRespReason(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) == 0 {
		return nil
	}
	tx.Resp.StatusReason = trimUnprintableChars(parts[0])
	return nil
}

func transformRespStatus(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) == 0 {
		return nil
	}
	status, err := strconv.ParseUint(strings.TrimRight(parts[0], "\x00"), 10, 64)
	if err != nil {
		return err
	}
	tx.Resp.Status = status
	return nil
}

func transformFilters(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) == 0 {
		return nil
	}
	for _, f := range parts {
		if f != "" {
			tx.Resp.Filters = append(tx.Resp.Filters, trimUnprintableChars(f))
		}
	}
	return nil
}

func transformTimestamp(tx *varnishTransaction, rec varnishlog.Record) error {
	parts, err := splitPayload(rec, 4, 4)
	if err != nil {
		return err
	}
	tsRaw, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp number: %s. %s", parts[1], err)
	}
	tsSec, tsFrac := math.Modf(tsRaw)
	ts := time.Unix(int64(tsSec), int64(tsFrac*1e9))
	name := strings.Replace(parts[0], ":", "", 1)
	tx.Events = append(tx.Events, varnishTransactionEvent{
		Timestamp: ts,
		Name:      trimUnprintableChars(name),
	})
	return nil
}

func transformAnyProtocol(rec varnishlog.Record) (string, string, error) {
	if _, err := splitPayload(rec, 1, 1); err != nil {
		return "", "", err
	}
	proto, protoVer, found := strings.Cut(trimUnprintableChars(rec.Payload.String()), "/")
	if !found {
		return "", "", fmt.Errorf("invalid tag: %s", rec.Tag.String())
	}
	return proto, protoVer, nil
}

func transformReqProtocol(tx *varnishTransaction, rec varnishlog.Record) error {
	p, pv, err := transformAnyProtocol(rec)
	if err != nil {
		return err
	}
	tx.Req.Proto = p
	tx.Req.ProtoVersion = pv
	return nil
}

func transformRespProtocol(tx *varnishTransaction, rec varnishlog.Record) error {
	p, pv, err := transformAnyProtocol(rec)
	if err != nil {
		return err
	}
	tx.Resp.Proto = p
	tx.Resp.ProtoVersion = pv
	return nil
}

func transformReqStart(tx *varnishTransaction, rec varnishlog.Record) error {
	parts, err := splitPayload(rec, 3, 3)
	if err != nil {
		return err
	}
	rPort, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return err
	}
	tx.Client.Addr = trimUnprintableChars(parts[0])
	tx.Client.Port = rPort
	tx.Client.Sock = trimUnprintableChars(parts[2])
	return nil
}

func transformBackendOpen(tx *varnishTransaction, rec varnishlog.Record) error {
	parts, err := splitPayload(rec, 7, -1)
	if err != nil {
		return err
	}
	bePort, err := strconv.ParseUint(parts[3], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid port number: %s. %s", parts[3], err)
	}
	tx.Backend.Name = trimUnprintableChars(parts[1])
	tx.Backend.Addr = trimUnprintableChars(parts[2])
	tx.Backend.Port = bePort
	tx.Backend.ConnReused = trimUnprintableChars(parts[6]) == "reused"
	return nil
}

func transformAnyAcct(tx *varnishTransaction, rec varnishlog.Record) error {
	if _, err := splitPayload(rec, 6, 6); err != nil {
		return err
	}
	var reqHdrLen, reqBodyLen, reqLen, respHdrLen, respBodyLen, respLen uint64
	if scanned, err := fmt.Sscanf(trimUnprintableChars(rec.Payload.String()), "%d %d %d %d %d %d", &reqHdrLen, &reqBodyLen, &reqLen, &respHdrLen, &respBodyLen, &respLen); err != nil || scanned != 6 {
		return fmt.Errorf("failed to parse Acct: %s", err)
	}
	tx.Req.HdrBytes = reqHdrLen
	tx.Req.BodyBytes = reqBodyLen
	tx.Resp.HdrBytes = respHdrLen
	tx.Resp.BodyBytes = respBodyLen
	return nil
}

func transformVCLReturn(tx *varnishTransaction, rec varnishlog.Record) error {
	parts, err := splitPayload(rec, 1, 1)
	if err != nil {
		return err
	}
	tx.Handling = trimUnprintableChars(parts[0])
	return nil
}

func transformVCLCall(tx *varnishTransaction, rec varnishlog.Record) error {
	parts, err := splitPayload(rec, 1, 1)
	if err != nil {
		return err
	}
	h := trimUnprintableChars(parts[0])
	switch h {
	case "MISS":
		tx.Handling = "miss"
	case "PASS":
		tx.Handling = "pass"
	case "PIPE":
		tx.Handling = "pipe"
	case "BACKEND_RESPONSE":
		tx.Handling = "fetch"
	case "BACKEND_ERROR":
		tx.Handling = "fetch_error"
	case "SYNTH":
		tx.Handling = "synth"
	default:
		tx.Handling = strings.ToLower(h)
	}
	return nil
}

func transformAnyError(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) == 0 {
		return nil
	}
	tx.Errors = append(tx.Errors, trimUnprintableChars(rec.Tag.String()))
	return nil
}

func transformAnyHeader(_ *varnishTransaction, rec varnishlog.Record) (string, string, error) {
	hdrName, hdrVal, found := strings.Cut(rec.Payload.String(), ": ")
	if !found || hdrName == "" || hdrVal == "" {
		return "", "", fmt.Errorf("invalid tag: %s", rec.Tag.String())
	}
	return strings.ToLower(hdrName), hdrVal, nil
}

func transformReqHeader(tx *varnishTransaction, rec varnishlog.Record) error {
	hdrName, hdrVal, err := transformAnyHeader(tx, rec)
	if err != nil {
		return err
	}
	tx.Req.Headers[hdrName] = trimUnprintableChars(hdrVal)
	return nil
}

func transformRespHeader(tx *varnishTransaction, rec varnishlog.Record) error {
	hdrName, hdrVal, err := transformAnyHeader(tx, rec)
	if err != nil {
		return err
	}
	tx.Resp.Headers[hdrName] = trimUnprintableChars(hdrVal)
	return nil
}

func transformBegin(tx *varnishTransaction, rec varnishlog.Record) error {
	parts, err := splitPayload(rec, 3, -1)
	if err != nil {
		return err
	}
	xid, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return err
	}
	if len(parts) > 3 {
		lvl, err := strconv.ParseUint(trimUnprintableChars(parts[3]), 10, 64)
		if err != nil {
			return err
		}
		tx.Level = lvl
	}
	tx.VXIDParent = xid
	tx.Type = parts[0]
	tx.Reason = trimUnprintableChars(parts[2])
	return nil
}

func transformHit(tx *varnishTransaction, rec varnishlog.Record) error {
	var xid, fetched, clen uint64
	var ttl, grace, keep float64

	scanned, err := fmt.Sscanf(trimUnprintableChars(rec.Payload.String()), "%d %f %f %f %d %d",
		&xid, &ttl, &grace, &keep, &fetched, &clen)
	if err != nil || scanned < 4 {
		return fmt.Errorf("failed to parse Hit: %s", err)
	}
	if scanned == 6 {
		tx.Handling = "streaming-hit"
	} else {
		tx.Handling = "hit"
	}
	return nil
}

var (
	transformFuncs = map[string]varnishTagTransformerFunc{
		"VCL_Log": transformVCLLog,
		"VCL_use": transformVCLUse,
		//"VCL_return":    transformVCLReturn, // TODO(thomasklinger1234): VCL_return or VCL_call for handling?
		"VCL_call":      transformVCLCall,
		"VCL_Error":     transformAnyError,
		"ReqURL":        transformReqURL,
		"BereqURL":      transformReqURL,
		"ReqMethod":     transformReqMethod,
		"BereqMethod":   transformReqMethod,
		"Link":          transformLink,
		"RespReason":    transformRespReason,
		"BerespReason":  transformRespReason,
		"RespStatus":    transformRespStatus,
		"BerespStatus":  transformRespStatus,
		"Filters":       transformFilters,
		"Timestamp":     transformTimestamp,
		"Storage":       transformStorage,
		"ReqProtocol":   transformReqProtocol,
		"RespProtocol":  transformRespProtocol,
		"BereqProtocol": transformReqProtocol,
		"ReqStart":      transformReqStart,
		"BackendOpen":   transformBackendOpen,
		"ReqAcct":       transformAnyAcct,
		"BereqAcct":     transformAnyAcct,
		"Error":         transformAnyError,
		"FetchError":    transformAnyError,
		"ReqHeader":     transformReqHeader,
		"BereqHeader":   transformReqHeader,
		"RespHeader":    transformRespHeader,
		"BerespHeader":  transformRespHeader,
		"Begin":         transformBegin,
		"Hit":           transformHit,
	}
)

// remove control characters from VSL entries. those are found on the last "part" of
// VSL records
func trimUnprintableChars(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return !unicode.IsGraphic(r)
	})
}

func extractTraceparent(tp string) (pcommon.TraceID, pcommon.SpanID, error) {
	tid := pcommon.TraceID{}
	sid := pcommon.SpanID{}

	parts := strings.Split(tp, "-")
	if len(parts) != 4 {
		return tid, sid, fmt.Errorf("traceparent has wrong length %d", len(parts))
	}
	if parts[0] != "00" {
		return tid, sid, fmt.Errorf("traceparent has wrong version %q", parts[0])
	}
	if len(parts[1]) != 32 {
		return tid, sid, fmt.Errorf("trace-id has wrong length %d", len(parts[1]))
	}
	if len(parts[2]) != 16 {
		return tid, sid, fmt.Errorf("span-id has wrong length %d", len(parts[1]))
	}
	if len(parts[3]) != 2 {
		return tid, sid, fmt.Errorf("flags has wrong length %d", len(parts[1]))
	}
	tidBuf, err := hex.DecodeString(parts[1])
	if err != nil || len(tidBuf) != 16 {
		return tid, sid, fmt.Errorf("failed to parse trace-id: %s", err)
	}
	sidBuf, err := hex.DecodeString(parts[2])
	if err != nil || len(sidBuf) != 8 {
		return tid, sid, fmt.Errorf("failed to parse span-id: %s", err)
	}

	copy(tid[:], tidBuf)
	copy(sid[:], sidBuf)

	return tid, sid, nil
}

func setHeaderSpanAttrs(span ptrace.Span, tx *varnishTransaction) {
	if ua, ok := tx.Req.Headers["user-agent"]; ok {
		span.Attributes().PutStr(string(semconv.UserAgentOriginalKey), ua)
	}
	if host, ok := tx.Req.Headers["host"]; ok {
		span.Attributes().PutStr(string(semconv.HostNameKey), host)
	}
}

func setVarnishSpanAttrs(span ptrace.Span, tx *varnishTransaction) {
	if tx.VCL != "" {
		span.Attributes().PutStr("varnish.vcl", tx.VCL)
	}
	if tx.Storage != nil {
		if tx.Storage.Name != "" {
			span.Attributes().PutStr("varnish.storage.name", tx.Storage.Name)
		}
		if tx.Storage.Type != "" {
			span.Attributes().PutStr("varnish.storage.type", tx.Storage.Type)
		}
	}
	if tx.Side != "" {
		span.Attributes().PutStr("varnish.side", tx.Side)
		switch tx.Side {
		case "client":
			span.SetKind(ptrace.SpanKindServer)
		case "backend":
			span.SetKind(ptrace.SpanKindClient)
		}
	}
	if tx.Handling != "" {
		span.Attributes().PutStr("varnish.handling", tx.Handling)
	}
	if len(tx.Resp.Filters) > 0 {
		span.Attributes().PutStr("varnish.filters", strings.Join(tx.Resp.Filters, " "))
	}
}

func setBackendSpanAttrs(span ptrace.Span, tx *varnishTransaction) {
	if tx.Backend.Name != "" {
		span.Attributes().PutStr("varnish.backend.name", tx.Backend.Name)
		span.Attributes().PutBool("varnish.backend.conn_reused", tx.Backend.ConnReused)
		if tx.Backend.Port > 0 {
			span.Attributes().PutInt(string(semconv.ClientPortKey), int64(tx.Backend.Port))
		}
		if tx.Backend.Addr != "" {
			span.Attributes().PutStr(string(semconv.ClientAddressKey), tx.Backend.Addr)
		}
	}
}

func setRequestSpanAttrs(span ptrace.Span, tx *varnishTransaction) {
	if tx.Req.URL != "" {
		span.Attributes().PutStr(string(semconv.URLFullKey), tx.Req.URL)
	}
	if tx.Req.Method != "" {
		span.Attributes().PutStr(string(semconv.HTTPRequestMethodKey), tx.Req.Method)
	}
	if tx.Req.Proto != "" {
		span.Attributes().PutStr(string(semconv.NetworkProtocolNameKey), tx.Req.Proto)
	}
	if tx.Req.ProtoVersion != "" {
		span.Attributes().PutStr(string(semconv.NetworkProtocolVersionKey), tx.Req.ProtoVersion)
	}
	if tx.Req.HdrBytes > 0 || tx.Req.BodyBytes > 0 {
		span.Attributes().PutInt(string(semconv.HTTPRequestSizeKey), int64(tx.Req.HdrBytes+tx.Req.BodyBytes))
	}
}

func setResponseSpanAttrs(span ptrace.Span, tx *varnishTransaction) {
	if tx.Resp.Proto != "" {
		span.Attributes().PutStr(string(semconv.NetworkProtocolNameKey), tx.Resp.Proto)
	}
	if tx.Resp.ProtoVersion != "" {
		span.Attributes().PutStr(string(semconv.NetworkProtocolVersionKey), tx.Resp.ProtoVersion)
	}
	if tx.Resp.Status > 0 && tx.Resp.Status < 1000 {
		span.Attributes().PutInt(string(semconv.HTTPResponseStatusCodeKey), int64(tx.Resp.Status))
	}
	if tx.Resp.HdrBytes > 0 || tx.Resp.BodyBytes > 0 {
		span.Attributes().PutInt(string(semconv.HTTPResponseSizeKey), int64(tx.Resp.HdrBytes+tx.Resp.BodyBytes))
	}
}

func setSpanName(span ptrace.Span, tx *varnishTransaction) {
	if tx.Side == "client" {
		// TODO(thomasklinger1234): Find suitable naming
		span.SetName(fmt.Sprintf("Varnish %s %s", tx.Reason, tx.Type))
	}
	if tx.Side == "backend" {
		span.SetName(fmt.Sprintf("Varnish to %s %s", tx.Backend.Name, tx.Handling))
	}
}

func setSpanTimestamps(span ptrace.Span, tx *varnishTransaction) {
	for _, event := range tx.Events {
		e := span.Events().AppendEmpty()
		e.SetName(event.Name)
		e.SetTimestamp(pcommon.NewTimestampFromTime(event.Timestamp))
	}
	if span.Events().Len() > 0 {
		span.SetStartTimestamp(span.Events().At(0).Timestamp())
		span.SetEndTimestamp(span.Events().At(span.Events().Len() - 1).Timestamp())
	} else {
		span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now()))
		span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	}
}

func updateSpan(span ptrace.Span, tx *varnishTransaction) error {
	if tp, ok := tx.Req.Headers["traceparent"]; ok {
		sid, tid, err := extractTraceparent(tp)
		if err != nil {
			return err
		}
		span.SetTraceID(sid)
		span.SetSpanID(tid)
	}
	setHeaderSpanAttrs(span, tx)
	setVarnishSpanAttrs(span, tx)
	setBackendSpanAttrs(span, tx)
	setRequestSpanAttrs(span, tx)
	setResponseSpanAttrs(span, tx)
	setSpanName(span, tx)
	setSpanTimestamps(span, tx)
	return nil
}
