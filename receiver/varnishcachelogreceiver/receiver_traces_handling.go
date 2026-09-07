package varnishcachelogreceiver

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	varnishlog "github.com/varnish/varnish-go/log"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

const (
	// vclLogAttributeKey can be used in VCL_Log messages to set custom attributes (see customizeSpanFromVCLLog(...))
	vclLogAttributeKey = "OTEL_Attribute:"
	// vclLogAttributeKey can be used in VCL_Log messages to set a custom span name (see customizeSpanFromVCLLog(...))
	vclLogSpanNameKey = "OTEL_SpanName:"
)

// splitPayload returns the space-separated fields of rec.Payload.
// It returns an "invalid tag" error when the field count is outside [min, max].
// Pass max = -1 to skip the upper bound check. Pass min = 0 to skip the lower bound.
func splitPayload(rec varnishlog.Record, min, max int) ([]string, error) {
	parts := strings.Fields(rec.Data)
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
	VXID   int64
	Type   string
	Reason string
}

type varnishTransactionEvent struct {
	Timestamp time.Time
	Name      string
}

type varnishTransactionCacheObj struct {
	TTL   float64
	Grace float64
	Keep  float64
}

type varnishTransactionStorage struct {
	Name string
	Type string
}

type varnishTransactionClientConn struct {
	Addr  string
	Port  uint64
	Sock  string
	Proto string
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
	VXID       int64
	VXIDParent int64
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
	CacheObj   *varnishTransactionCacheObj
	// SLT_Begin: sess, req, bereq
	Type  string
	Level uint64

	// todo(Aljoscha): re-evaluate this
	// traceparent parse cache. Set once by extractTraceContext;
	// buildTraces hits this path repeatedly for the same tx (self
	// context + parent lookups).
	tpParsed      bool
	tpOK          bool
	tpTID         pcommon.TraceID
	tpSID         pcommon.SpanID
	tpFlags       byte
	tpUpstreamSID pcommon.SpanID
}

func (tx *varnishTransaction) traceparent() string {
	if v, ok := tx.Req.Headers[requiredTraceparentHeader]; ok {
		return v
	}
	return ""
}

// upstreamSpanID returns the span-id parsed from the client-sent
// traceparent header, if any, and is used as parent_id on the root span of this request.
func (tx *varnishTransaction) upstreamSpanID() pcommon.SpanID {
	return tx.tpUpstreamSID
}

type varnishTagTransformerFunc func(vtx *varnishTransaction, rec varnishlog.Record) error

func transformVCLLog(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Data)
	if len(parts) > 0 {
		tx.Logs = append(tx.Logs, rec.Data)
	}
	return nil
}

func transformReqURL(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Data)
	if len(parts) == 0 {
		return nil
	}

	// for client requests: record what the client sees aka "first ReqURL wins"
	// for backend requests: record what the backend sees aka "last ReqURL wins"
	if tx.Side == "client" && tx.Req.URL != "" {
		return nil
	}
	tx.Req.URL = parts[0]
	return nil
}

func transformReqUnset(tx *varnishTransaction, rec varnishlog.Record) error {
	hdrName, _, err := parseHeader(rec.Data)
	if err != nil {
		return fmt.Errorf("could not parse ReqUnset header: %s", err)
	}
	delete(tx.Req.Headers, hdrName)
	return nil
}

func transformRespUnset(tx *varnishTransaction, rec varnishlog.Record) error {
	hdrName, _, err := parseHeader(rec.Data)
	if err != nil {
		return fmt.Errorf("could not parse RespUnset header: %s", err)
	}
	delete(tx.Resp.Headers, hdrName)
	return nil
}

func transformReqMethod(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Data)
	if len(parts) == 0 {
		return nil
	}
	tx.Req.Method = parts[0]
	return nil
}

func transformVCLUse(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Data)
	if len(parts) == 0 {
		return nil
	}
	tx.VCL = parts[0]
	return nil
}

func transformLink(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Data)
	if len(parts) < 3 {
		return fmt.Errorf("invalid link received: %s", rec.Data)
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return err
	}
	tx.Links = append(tx.Links, varnishTransactionLink{
		VXID:   id,
		Type:   parts[0],
		Reason: parts[2],
	})
	return nil
}

func transformStorage(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Data)
	if len(parts) != 2 {
		return fmt.Errorf("invalid storage received: %s", rec.Data)
	}
	tx.Storage = &varnishTransactionStorage{
		Name: parts[0],
		Type: parts[1],
	}
	return nil
}

func transformRespReason(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Data)
	if len(parts) == 0 {
		return nil
	}
	tx.Resp.StatusReason = parts[0]
	return nil
}

func transformRespStatus(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Data)
	if len(parts) == 0 {
		return nil
	}
	status, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return err
	}
	tx.Resp.Status = status
	return nil
}

func transformFilters(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Data)
	if len(parts) == 0 {
		return nil
	}
	for _, f := range parts {
		if f != "" {
			tx.Resp.Filters = append(tx.Resp.Filters, f)
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
		return fmt.Errorf("invalid timestamp received: %s", rec.Data)
	}
	tsSecs := int64(tsRaw)
	tsNanos := int64((tsRaw - float64(tsSecs)) * 1e9)
	ts := time.Unix(tsSecs, tsNanos).UTC()
	name := strings.Replace(parts[0], ":", "", 1)
	tx.Events = append(tx.Events, varnishTransactionEvent{
		Timestamp: ts,
		Name:      name,
	})
	return nil
}

func transformAnyProtocol(rec varnishlog.Record) (string, string, error) {
	if _, err := splitPayload(rec, 1, 1); err != nil {
		return "", "", err
	}
	proto, protoVer, found := strings.Cut(rec.Data, "/")
	if !found {
		return "", "", fmt.Errorf("invalid protocol tag: %s", rec.Tag.String())
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
	parts, err := splitPayload(rec, 3, 4)
	if err != nil {
		return err
	}
	rPort, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return err
	}
	tx.Client.Addr = parts[0]
	tx.Client.Port = rPort
	tx.Client.Sock = parts[2]
	if len(parts) > 3 {
		tx.Client.Proto = parts[3]
	}
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
	tx.Backend.Name = parts[1]
	tx.Backend.Addr = parts[2]
	tx.Backend.Port = bePort
	tx.Backend.ConnReused = parts[6] == "reused"
	return nil
}

func transformAnyAcct(tx *varnishTransaction, rec varnishlog.Record) error {
	if _, err := splitPayload(rec, 6, 6); err != nil {
		return err
	}
	var reqHdrLen, reqBodyLen, reqLen, respHdrLen, respBodyLen, respLen uint64
	if scanned, err := fmt.Sscanf(rec.Data, "%d %d %d %d %d %d", &reqHdrLen, &reqBodyLen, &reqLen, &respHdrLen, &respBodyLen, &respLen); err != nil || scanned != 6 {
		return fmt.Errorf("failed to parse Acct: %s", err)
	}
	tx.Req.HdrBytes = reqHdrLen
	tx.Req.BodyBytes = reqBodyLen
	tx.Resp.HdrBytes = respHdrLen
	tx.Resp.BodyBytes = respBodyLen
	return nil
}

const (
	handlingMiss         = "miss"
	handlingPass         = "pass"
	handlingPipe         = "pipe"
	handlingPurge        = "purge"
	handlingFetch        = "fetch"
	handlingFetchError   = "fetch_error"
	handlingSynth        = "synth"
	handlingHit          = "hit"
	handlingHitStreaming = "streaming-hit"
)

func transformVCLCall(tx *varnishTransaction, rec varnishlog.Record) error {
	parts, err := splitPayload(rec, 1, 1)
	if err != nil {
		return err
	}
	h := parts[0]
	switch h {
	case "MISS":
		tx.Handling = handlingMiss
	case "PASS":
		tx.Handling = handlingPass
	case "PIPE":
		tx.Handling = handlingPipe
	case "PURGE":
		tx.Handling = handlingPurge
	case "BACKEND_RESPONSE":
		tx.Handling = handlingFetch
	case "BACKEND_ERROR":
		tx.Handling = handlingFetchError
	case "SYNTH":
		tx.Handling = handlingSynth
	case "HIT":
		if tx.Handling == "" {
			tx.Handling = handlingHit
		}
	}
	return nil
}

func transformAnyError(tx *varnishTransaction, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Data)
	if len(parts) == 0 {
		return nil
	}
	tx.Errors = append(tx.Errors, rec.Tag.String())
	return nil
}

func parseHeader(data string) (string, string, error) {
	hdrName, hdrVal, found := strings.Cut(data, ":")
	if !found || hdrName == "" {
		var safeOutput string
		if len(data) > 5 {
			safeOutput = data[:5] + "..."
		} else {
			safeOutput = data
		}
		return "", "", fmt.Errorf("malformed header: %s", safeOutput)
	}
	return strings.ToLower(hdrName), strings.TrimLeft(hdrVal, " "), nil
}

func transformReqHeader(tx *varnishTransaction, rec varnishlog.Record) error {
	hdrName, hdrVal, err := parseHeader(rec.Data)
	if err != nil {
		return fmt.Errorf("failed to parse tag '%s': %w", rec.Tag.String(), err)
	}
	tx.Req.Headers[hdrName] = hdrVal
	return nil
}

func transformRespHeader(tx *varnishTransaction, rec varnishlog.Record) error {
	hdrName, hdrVal, err := parseHeader(rec.Data)
	if err != nil {
		return fmt.Errorf("failed to parse tag '%s': %w", rec.Tag.String(), err)
	}
	tx.Resp.Headers[hdrName] = hdrVal
	return nil
}

func transformHit(tx *varnishTransaction, rec varnishlog.Record) error {
	var xid, fetched, clen uint64
	var ttl, grace, keep float64

	scanned, _ := fmt.Sscanf(rec.Data, "%d %f %f %f %d %d",
		&xid, &ttl, &grace, &keep, &fetched, &clen)
	if scanned < 4 {
		return fmt.Errorf("failed to parse Hit: scanned=%d payload=%q", scanned, rec.Data)
	}
	if scanned == 6 {
		tx.Handling = handlingHitStreaming
	} else {
		tx.Handling = handlingHit
	}
	tx.CacheObj = &varnishTransactionCacheObj{
		TTL:   ttl,
		Grace: grace,
		Keep:  keep,
	}
	return nil
}

var (
	transformFuncs = map[string]varnishTagTransformerFunc{
		varnishlog.TagVCLLog.String():        transformVCLLog,
		varnishlog.TagVCLUse.String():        transformVCLUse,
		varnishlog.TagVCLCall.String():       transformVCLCall,
		varnishlog.TagVCLError.String():      transformAnyError,
		varnishlog.TagReqURL.String():        transformReqURL,
		varnishlog.TagBereqURL.String():      transformReqURL,
		varnishlog.TagReqUnset.String():      transformReqUnset,
		varnishlog.TagBereqUnset.String():    transformReqUnset,
		varnishlog.TagReqMethod.String():     transformReqMethod,
		varnishlog.TagRespUnset.String():     transformRespUnset,
		varnishlog.TagBerespUnset.String():   transformRespUnset,
		varnishlog.TagBereqMethod.String():   transformReqMethod,
		varnishlog.TagLink.String():          transformLink,
		varnishlog.TagRespReason.String():    transformRespReason,
		varnishlog.TagBerespReason.String():  transformRespReason,
		varnishlog.TagRespStatus.String():    transformRespStatus,
		varnishlog.TagBerespStatus.String():  transformRespStatus,
		varnishlog.TagFilters.String():       transformFilters,
		varnishlog.TagTimestamp.String():     transformTimestamp,
		varnishlog.TagStorage.String():       transformStorage,
		varnishlog.TagReqProtocol.String():   transformReqProtocol,
		varnishlog.TagRespProtocol.String():  transformRespProtocol,
		varnishlog.TagBereqProtocol.String(): transformReqProtocol,
		varnishlog.TagReqStart.String():      transformReqStart,
		varnishlog.TagBackendOpen.String():   transformBackendOpen,
		varnishlog.TagReqAcct.String():       transformAnyAcct,
		varnishlog.TagBereqAcct.String():     transformAnyAcct,
		varnishlog.TagError.String():         transformAnyError,
		varnishlog.TagFetchError.String():    transformAnyError,
		varnishlog.TagRespHeader.String():    transformRespHeader,
		varnishlog.TagBerespHeader.String():  transformRespHeader,
		varnishlog.TagReqHeader.String():     transformReqHeader,
		varnishlog.TagBereqHeader.String():   transformReqHeader,
		varnishlog.TagHit.String():           transformHit,
	}
)

func extractTraceparent(tp string) (pcommon.TraceID, pcommon.SpanID, byte, error) {
	var tid pcommon.TraceID
	var sid pcommon.SpanID

	parts := strings.Split(tp, "-")
	if len(parts) != 4 {
		return tid, sid, 0, fmt.Errorf("traceparent has wrong length %d", len(parts))
	}
	if parts[0] != "00" {
		return tid, sid, 0, fmt.Errorf("traceparent has wrong version %q", parts[0])
	}
	if len(parts[1]) != 32 {
		return tid, sid, 0, fmt.Errorf("trace-id has wrong length %d", len(parts[1]))
	}
	if len(parts[2]) != 16 {
		return tid, sid, 0, fmt.Errorf("span-id has wrong length %d", len(parts[2]))
	}
	if len(parts[3]) != 2 {
		return tid, sid, 0, fmt.Errorf("flags has wrong length %d", len(parts[3]))
	}
	if n, err := hex.Decode(tid[:], []byte(parts[1])); err != nil || n != 16 {
		return pcommon.TraceID{}, pcommon.SpanID{}, 0, fmt.Errorf("failed to parse trace-id: %w", err)
	}
	if n, err := hex.Decode(sid[:], []byte(parts[2])); err != nil || n != 8 {
		return pcommon.TraceID{}, pcommon.SpanID{}, 0, fmt.Errorf("failed to parse span-id: %w", err)
	}
	var flagsBuf [1]byte
	if n, err := hex.Decode(flagsBuf[:], []byte(parts[3])); err != nil || n != 1 {
		return pcommon.TraceID{}, pcommon.SpanID{}, 0, fmt.Errorf("failed to parse flags: %w", err)
	}
	return tid, sid, flagsBuf[0], nil
}

func setHeaderSpanAttrs(span ptrace.Span, tx *varnishTransaction, opts spanOpts) {
	for _, h := range opts.requestHdrMapping {
		if v, ok := tx.Req.Headers[h.HdrName]; ok {
			span.Attributes().PutStr(h.OtelAttrKey, v)
		}
	}
	for _, h := range opts.responseHdrMapping {
		if v, ok := tx.Resp.Headers[h.HdrName]; ok {
			span.Attributes().PutStr(h.OtelAttrKey, v)
		}
	}

	for hdrName, hdrVal := range tx.Req.Headers {
		switch hdrName {
		case "user-agent":
			span.Attributes().PutStr(string(semconv.UserAgentNameKey), hdrVal)
		case "host":
			span.Attributes().PutStr(string(semconv.HostNameKey), hdrVal)
		}
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
	}
	if tx.Handling != "" {
		span.Attributes().PutStr("varnish.handling", tx.Handling)
	}
	if len(tx.Resp.Filters) > 0 {
		span.Attributes().PutStr("varnish.filters", strings.Join(tx.Resp.Filters, " "))
	}
}

func setCacheHitSpanAttrs(span ptrace.Span, tx *varnishTransaction) {
	isHit := tx.Handling == handlingHit || tx.Handling == handlingHitStreaming
	span.Attributes().PutBool("varnish.obj.hit", isHit)
	if tx.CacheObj == nil {
		return
	}
	span.Attributes().PutDouble("varnish.obj.ttl", tx.CacheObj.TTL)
	span.Attributes().PutDouble("varnish.obj.grace", tx.CacheObj.Grace)
	span.Attributes().PutDouble("varnish.obj.keep", tx.CacheObj.Keep)
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
	if tx.Req.Proto != "" {
		span.Attributes().PutStr(string(semconv.NetworkProtocolNameKey), tx.Req.Proto)
	}
	if tx.Req.ProtoVersion != "" {
		span.Attributes().PutStr(string(semconv.NetworkProtocolVersionKey), tx.Req.ProtoVersion)
	}
	if tx.Client.Proto != "" {
		span.Attributes().PutStr(string(semconv.URLSchemeKey), tx.Client.Proto)
	}
	if tx.Req.URL != "" {
		if urlParsed, err := url.Parse(tx.Req.URL); err == nil {
			span.Attributes().PutStr(string(semconv.URLPathKey), urlParsed.EscapedPath())
			span.Attributes().PutStr(string(semconv.URLQueryKey), urlParsed.Query().Encode())
		}
		span.Attributes().PutStr(string(semconv.URLFullKey), tx.Req.URL)
	}
	if tx.Req.Method != "" {
		span.Attributes().PutStr(string(semconv.HTTPRequestMethodKey), tx.Req.Method)
	}
	if tx.Req.HdrBytes > 0 || tx.Req.BodyBytes > 0 {
		span.Attributes().PutInt(string(semconv.HTTPRequestSizeKey), int64(tx.Req.HdrBytes+tx.Req.BodyBytes))
	}
	if host, ok := tx.Req.Headers["host"]; ok {
		span.Attributes().PutStr(string(semconv.ServerAddressKey), host)
	}
}

func setResponseSpanAttrs(span ptrace.Span, tx *varnishTransaction) {
	if tx.Resp.Proto != "" {
		span.Attributes().PutStr(string(semconv.NetworkProtocolNameKey), tx.Resp.Proto)
	}
	if tx.Resp.ProtoVersion != "" {
		span.Attributes().PutStr(string(semconv.NetworkProtocolVersionKey), tx.Resp.ProtoVersion)
	}
	if tx.Resp.Status > 0 {
		// using HTTP status codes > 1000 is a common practice in the Varnish community (e.g. for vcl_synth).
		span.Attributes().PutInt(string(semconv.HTTPResponseStatusCodeKey), int64(tx.Resp.Status%1000))
	}
	if tx.Resp.HdrBytes > 0 || tx.Resp.BodyBytes > 0 {
		span.Attributes().PutInt(string(semconv.HTTPResponseSizeKey), int64(tx.Resp.HdrBytes+tx.Resp.BodyBytes))
	}
}

func setSpanMeta(span ptrace.Span, tx *varnishTransaction) {
	switch tx.Reason {
	case "esi":
		span.SetName(fmt.Sprintf("ESI %s", tx.Req.Method))
		span.SetKind(ptrace.SpanKindInternal)
	case "rxreq":
		span.SetName(fmt.Sprintf("HTTP %s", tx.Req.Method))
		span.SetKind(ptrace.SpanKindServer)
	default:
		span.SetName(fmt.Sprintf("%s %s", tx.Side, tx.Reason))
		span.SetKind(ptrace.SpanKindClient)
	}
}

func setSpanCode(span ptrace.Span, tx *varnishTransaction) {
	if tx.Handling == handlingFetchError {
		span.Status().SetCode(ptrace.StatusCodeError)
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

func customizeSpanFromVCLLog(span ptrace.Span, tx *varnishTransaction) {
	for _, logMsg := range tx.Logs {
		if strings.HasPrefix(logMsg, vclLogAttributeKey) {
			key, value, found := strings.Cut(logMsg[len(vclLogAttributeKey):], "=")
			attrKey := strings.TrimSpace(key)
			attrVal := strings.TrimSpace(value)
			if found && attrKey != "" && attrVal != "" {
				span.Attributes().PutStr(attrKey, attrVal)
			}
		}
		if strings.HasPrefix(logMsg, vclLogSpanNameKey) {
			spanName := strings.TrimSpace(logMsg[len(vclLogSpanNameKey):])
			if spanName != "" {
				span.SetName(spanName)
			}
		}
	}
}

func updateSpan(span ptrace.Span, tx *varnishTransaction, opts spanOpts) {
	setHeaderSpanAttrs(span, tx, opts)
	setVarnishSpanAttrs(span, tx)
	setBackendSpanAttrs(span, tx)
	setCacheHitSpanAttrs(span, tx)
	setRequestSpanAttrs(span, tx)
	setResponseSpanAttrs(span, tx)
	setSpanTimestamps(span, tx)

	setSpanMeta(span, tx)
	setSpanCode(span, tx)

	// keep this at the end
	customizeSpanFromVCLLog(span, tx)
}
