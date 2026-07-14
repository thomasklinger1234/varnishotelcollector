package varnishcachelogreceiver

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/thomasklinger1234/varnishotelcollector/receiver/varnishcachelogreceiver/internal/metadata"
	varnishlog "gitlab.com/uplex/varnish/varnishapi/pkg/log"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/receiver"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.uber.org/zap"
)

var _ receiver.Traces = &varnishcachelogReceiver{}

type varnishcachelogReceiver struct {
	set          receiver.Settings
	cfg          *Config
	nextConsumer consumer.Traces
	wg           sync.WaitGroup
}

func (v varnishcachelogReceiver) Start(ctx context.Context, host component.Host) error {
	vsm := varnishlog.New()
	if err := vsm.Timeout(v.cfg.Timeout); err != nil {
		return fmt.Errorf("failed to set vsm timeout: %w", err)
	}

	if err := vsm.Attach(v.cfg.WorkingDirectory); err != nil {
		return fmt.Errorf("failed to attach to vsm: %w", err)
	}

	vsmCursor, err := vsm.NewCursor()
	if err != nil {
		return fmt.Errorf("failed to create vsm cursor: %w", err)
	}

	vsmQuery, err := vsmCursor.NewQuery(varnishlog.Request, v.cfg.VSLQuery)
	if err != nil {
		return fmt.Errorf("failed to create vsm query: %w", err)
	}

	v.wg.Go(func() {
		defer func() {
			vsmCursor.Delete()
			vsm.Release()
		}()

		for {
			select {
			case <-ctx.Done():
				return
			default:
				txGrp, txStatus := vsmQuery.NextTxGroup()
				switch txStatus {
				case varnishlog.EOL:
					continue
				case varnishlog.EOF:
					v.set.Logger.Error("VSL EOF", zap.String("error", vsm.Error()))
					return
				case varnishlog.Abandoned:
					v.set.Logger.Error("VSL abandoned", zap.String("error", vsm.Error()))
					return
				case varnishlog.IOErr:
					v.set.Logger.Error("VSL IOErr", zap.String("error", vsm.Error()))
					return
				case varnishlog.WriteErr:
					v.set.Logger.Error("VSL WriteErr", zap.String("error", vsm.Error()))
					return
				case varnishlog.Overrun:
					v.set.Logger.Error("VSL overrun", zap.String("error", vsm.Error()))
					return
				default:
					traces := ptrace.NewTraces()

					resourceSpans := traces.ResourceSpans().AppendEmpty()
					v.set.Resource.CopyTo(resourceSpans.Resource())

					scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()
					scopeSpans.Scope().SetName(metadata.ScopeName)

					for txIdx, tx := range txGrp {
						span := scopeSpans.Spans().AppendEmpty()
						span.Attributes().PutInt("varnish.vxid", int64(tx.VXID))
						span.Attributes().PutStr("varnish.side", tx.Type.String())
						span.Attributes().PutStr("varnish.reason", tx.Reason.String())

						// TODO(thomasklinger1234): find a better way to determine the type of request
						if txIdx == 0 { // session
							span.SetName("Varnish request handling")
						} else if txIdx == 1 { // client request
							span.SetName("Varnish client request")
							span.Attributes().PutStr("varnish.side", "client")
						} else { // backend requests
							span.SetName(fmt.Sprintf("Varnish backend request"))
							span.Attributes().PutStr("varnish.side", "backend")
						}

						for _, txRec := range tx.Records {
							switch txRec.Type {
							case varnishlog.Client:
							case varnishlog.Backend:
							}

							switch txRec.Tag.String() {
							case "Begin":
								if err := convertBeginTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							case "VCL_use":
								if err := convertVCLUseTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							case "Storage":
								if err := convertStorageTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							case "ReqStart":
								if err := convertReqStartTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							case "ReqURL":
								if err := convertReqURLTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							case "BereqURL":
								if err := convertBereqURLTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							case "ReqMethod":
								if err := convertReqMethodTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							case "BereqMethod":
								if err := convertBereqMethodTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							case "RespStatus":
								if err := convertRespStatusTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							case "BerespStatus":
								if err := convertBerespStatusTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							// TODO(thomaskligner1234): ReqUnset?
							case "ReqHeader":
								if err := convertReqHeaderTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							// TODO(thomaskligner1234): BereqUnset?
							case "BereqHeader":
								if err := convertReqHeaderTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							case "ReqProtocol":
								if err := convertReqProtocolTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							case "BereqProtocol":
								if err := convertBereqProtocolTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							case "RespProtocol":
								if err := convertRespProtocolTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							case "Timestamp":
								if err := convertTimestampTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							case "Filters":
								if err := convertFiltersTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							case "BackendOpen":
								if err := convertBackendOpenTag(span, txRec); err != nil {
									v.set.Logger.Error("failed to convert tag", zap.Error(err), zap.String("tag", txRec.Tag.String()))
								}
							}
						}

						if span.TraceID().IsEmpty() {
							tidBuf := make([]byte, 16)
							binary.BigEndian.PutUint64(tidBuf[0:16], rand.Uint64())
							span.SetTraceID(pcommon.TraceID(tidBuf))
						}
						if span.SpanID().IsEmpty() {
							sidBuf := make([]byte, 8)
							binary.BigEndian.PutUint64(sidBuf[0:8], uint64(tx.VXID))
							span.SetSpanID(pcommon.SpanID(sidBuf))
						}
						if span.ParentSpanID().IsEmpty() && txIdx > 0 {
							// initial request has no parent
							sidBuf := make([]byte, 8)
							binary.BigEndian.PutUint64(sidBuf[0:8], uint64(tx.ParentVXID))
							span.SetParentSpanID(pcommon.SpanID(sidBuf))
						}

					}

					if err := v.nextConsumer.ConsumeTraces(ctx, traces); err != nil {
						v.set.Logger.Error("failed to consume traces", zap.Error(err))
					}
				}
			}
		}

	})
	return nil
}

func (v varnishcachelogReceiver) Shutdown(ctx context.Context) error {
	v.wg.Wait()
	return nil
}

func newVarnishcacheLogReceiver(set receiver.Settings, config *Config, nextConsumer consumer.Traces) receiver.Traces {
	return &varnishcachelogReceiver{
		set:          set,
		cfg:          config,
		nextConsumer: nextConsumer,
	}
}

func convertReqHeaderTag(span ptrace.Span, rec varnishlog.Record) error {
	hdrName, hdrVal, found := strings.Cut(rec.Payload.String(), ": ")
	if !found {
		return fmt.Errorf("invalid header")
	}

	switch strings.ToLower(hdrName) {
	case "user-agent":
		span.Attributes().PutStr(string(semconv.UserAgentOriginalKey), hdrVal)
	case "host":
		span.Attributes().PutStr(string(semconv.HostNameKey), hdrVal)
	case "traceparent":
		tpParts := strings.Split(cleanUnprintableChars(hdrVal), "-")
		if len(tpParts) < 4 {
			return fmt.Errorf("invalid traceparent (%d length)", len(tpParts))
		}
		if len(tpParts[0]) != 2 {
			return fmt.Errorf("invalid traceparent version (%d)", len(tpParts[0]))
		}
		if len(tpParts[1]) != 32 {
			return fmt.Errorf("invalid traceparent trace-id (%d)", len(tpParts[1]))
		}
		if len(tpParts[2]) != 16 {
			return fmt.Errorf("invalid traceparent span-id (%d)", len(tpParts[2]))
		}
		if len(tpParts[3]) != 2 {
			return fmt.Errorf("invalid traceparent flags (%s)", tpParts[3])
		}
		_, err := hex.DecodeString(tpParts[0])
		if err != nil {
			return err
		}
		tpTraceID, err := hex.DecodeString(tpParts[1])
		if err != nil || len(tpTraceID) != 16 {
			return err
		}
		tpSpanID, err := hex.DecodeString(tpParts[2])
		if err != nil || len(tpSpanID) != 8 {
			return err
		}
		// TODO(thomasklinger1234):utilize version
		_, err = hex.DecodeString(tpParts[3])
		if err != nil {
			return err
		}

		traceID := pcommon.NewTraceIDEmpty()
		copy(traceID[:], tpTraceID)

		spanID := pcommon.NewSpanIDEmpty()
		copy(spanID[:], tpSpanID)

		span.SetTraceID(traceID)
		span.SetSpanID(spanID)
	}

	return nil
}

func convertBeginTag(span ptrace.Span, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) < 3 {
		return fmt.Errorf("invalid tag: %s", rec.Tag.String())
	}
	switch parts[0] {
	case "sess":
		break
	case "req":
		span.SetKind(ptrace.SpanKindServer)
	case "bereq":
		span.SetKind(ptrace.SpanKindClient)
	}

	fmt.Printf("convertBeginTag:(%s)", rec.Tag)
	span.Attributes().PutStr("varnish.handling", parts[2])

	return nil
}

func convertReqMethodTag(span ptrace.Span, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) != 1 {
		return fmt.Errorf("invalid tag: %s", rec.Tag.String())
	}
	span.Attributes().PutStr(string(semconv.HTTPMethodKey), parts[0])
	return nil
}

func convertBereqMethodTag(span ptrace.Span, rec varnishlog.Record) error {
	return convertReqMethodTag(span, rec)
}

func convertReqURLTag(span ptrace.Span, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) != 1 {
		return fmt.Errorf("invalid tag: %s", rec.Tag.String())
	}
	span.Attributes().PutStr(string(semconv.URLFullKey), parts[0])
	return nil
}

func convertBereqURLTag(span ptrace.Span, rec varnishlog.Record) error {
	return convertReqURLTag(span, rec)
}

func convertReqStartTag(span ptrace.Span, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) != 3 {
		return fmt.Errorf("invalid tag: %s", rec.Tag.String())
	}
	span.Attributes().PutStr(string(semconv.NetPeerNameKey), parts[0])
	if port, err := strconv.Atoi(parts[1]); err != nil {
		return fmt.Errorf("invalid port number: %s. %s", parts[1], err)
	} else {
		span.Attributes().PutInt(string(semconv.NetPeerPortKey), int64(port))
	}
	return nil
}

func convertRespStatusTag(span ptrace.Span, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) != 1 {
		return fmt.Errorf("invalid tag: %s", rec.Tag.String())
	}
	if status, err := strconv.Atoi(strings.TrimRight(parts[0], "\x00")); err != nil { // RespStatus contains some weird chars
		return fmt.Errorf("invalid status number: %s. %s", parts[0], err)
	} else {
		span.Attributes().PutInt(string(semconv.HTTPStatusCodeKey), int64(status))
	}
	return nil
}

func convertBerespStatusTag(span ptrace.Span, rec varnishlog.Record) error {
	return convertRespStatusTag(span, rec)
}

func convertReqProtocolTag(span ptrace.Span, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) != 1 {
		return fmt.Errorf("invalid tag: %s", rec.Tag.String())
	}
	proto, protoVer, found := strings.Cut(parts[0], "/")
	if !found {
		return fmt.Errorf("invalid tag: %s", rec.Tag.String())
	}
	span.Attributes().PutStr(string(semconv.NetworkProtocolNameKey), proto)
	span.Attributes().PutStr(string(semconv.NetworkProtocolVersionKey), protoVer)
	return nil
}

func convertRespProtocolTag(span ptrace.Span, rec varnishlog.Record) error {
	return convertReqProtocolTag(span, rec)
}

func convertBereqProtocolTag(span ptrace.Span, rec varnishlog.Record) error {
	return convertReqProtocolTag(span, rec)
}

func convertTimestampTag(span ptrace.Span, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) != 4 {
		return fmt.Errorf("invalid tag: %s", rec.Tag.String())
	}
	tsRaw, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp number: %s. %s", parts[1], err)
	}
	tsSec, tsFrac := math.Modf(tsRaw)
	ts := time.Unix(int64(tsSec), int64(tsFrac*1e9))
	eventLabel := strings.Replace(parts[0], ":", "", 1)
	event := span.Events().AppendEmpty()
	event.SetName(eventLabel)
	event.SetTimestamp(pcommon.NewTimestampFromTime(ts))
	if eventLabel == "Start" {
		span.SetStartTimestamp(pcommon.NewTimestampFromTime(ts))
	}
	if eventLabel == "End" {
		span.SetEndTimestamp(pcommon.NewTimestampFromTime(ts))
	}

	return nil
}

func convertStorageTag(span ptrace.Span, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) != 2 {
		return fmt.Errorf("invalid tag: %s", rec.Tag.String())
	}
	span.Attributes().PutStr("varnish.storage_name", parts[0])
	span.Attributes().PutStr("varnish.storage_type", parts[1])
	return nil
}

func convertVCLUseTag(span ptrace.Span, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) != 1 {
		return fmt.Errorf("invalid tag: %s", rec.Tag.String())
	}
	span.Attributes().PutStr("varnish.vcl", parts[0])
	return nil
}

func convertFiltersTag(span ptrace.Span, rec varnishlog.Record) error {
	span.Attributes().PutStr("varnish.filters", strings.TrimSpace(rec.Payload.String()))
	return nil
}

func convertBackendOpenTag(span ptrace.Span, rec varnishlog.Record) error {
	parts := strings.Fields(rec.Payload.String())
	if len(parts) < 7 {
		return fmt.Errorf("invalid tag: %s", rec.Tag.String())
	}
	beName := parts[1]
	beAddr := parts[2]
	bePort, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid port number: %s. %s", parts[3], err)
	}
	beConnStatus := parts[6]
	beConnReused := false
	if beConnStatus == "reused" {
		beConnReused = true
	}

	span.Attributes().PutStr("varnish.backend", beName)
	span.Attributes().PutBool("varnish.backend.conn_reused", beConnReused)
	span.Attributes().PutStr(string(semconv.NetPeerNameKey), beAddr)
	span.Attributes().PutInt(string(semconv.NetPeerPortKey), bePort)

	status := beConnStatus
	if handling, found := span.Attributes().Get("varnish.handling"); found {
		status = handling.Str()
	}
	span.SetName(fmt.Sprintf("Varnish to %s %s", beName, status))

	return nil
}

func cleanUnprintableChars(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return !unicode.IsGraphic(r)
	})
}
