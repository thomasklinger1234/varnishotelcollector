package varnishcachelogreceiver

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/thomasklinger1234/varnishotelcollector/receiver/varnishcachelogreceiver/internal/metadata"
	varnishlog "github.com/varnish/varnish-go/log"
	varnishversion "github.com/varnish/varnish-go/version"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componentstatus"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/receiver"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.uber.org/zap"
)

var _ receiver.Traces = &varnishcachelogTraceReceiver{}

// requiredTraceparentHeader is captured on every transaction regardless of
// user configuration and is required on every transaction. If the traceparent
// is missing we will drop the trace/span and print a log message.
const requiredTraceparentHeader = "traceparent"

type varnishcachelogTraceReceiver struct {
	set          receiver.Settings
	cfg          *Config
	nextConsumer consumer.Traces
	spanOpts     spanOpts
	wg           sync.WaitGroup
	cancel       context.CancelFunc
}

type headerMapping struct {
	HdrName     string
	OtelAttrKey string
}

type spanOpts struct {
	requestHdrMapping  []headerMapping
	responseHdrMapping []headerMapping
}

func (v *varnishcachelogTraceReceiver) Start(ctx context.Context, host component.Host) error {
	vsmReader, err := varnishlog.New().
		SetGrouping(varnishlog.GroupingRequest).
		SetName(v.cfg.WorkingDirectory).
		SetFile(v.cfg.VSLBinaryFile).
		SetQuery(v.cfg.VSLQuery).
		SetTimeout(v.cfg.Timeout).
		SetBacklog(false).
		SetErrHandler(func(err varnishlog.LogErr) {
			switch err {
			case varnishlog.ErrCursorLost:
			case varnishlog.ErrIO:
				v.set.Logger.Warn("encountered recoverable VSL error", zap.String("error", err.String()))
			default:
				v.set.Logger.Debug("encountered recoverable VSL error", zap.String("error", err.String()))
			}
		}).
		Attach()
	if err != nil {
		return fmt.Errorf("failed to attach to VSL: %s", err.Error())
	}

	if v.cfg.VSLBinaryFile != "" {
		v.set.Logger.Info("successfully attached to VSL",
			zap.String("vsl_binary_file", v.cfg.VSLBinaryFile))

	} else {
		v.set.Logger.Info("successfully attached to VSL",
			zap.String("working_directory", v.cfg.WorkingDirectory),
			zap.String("vsl_query", v.cfg.VSLQuery))
	}

	ctx, cancel := context.WithCancel(ctx) // varnish-go/log requires a cancelable context
	v.cancel = cancel

	v.wg.Go(func() {
		defer func() {
			v.set.Logger.Info("closing VSM reader")
			vsmReader.Close()
		}()
		defer func() {
			if r := recover(); r != nil {
				v.set.Logger.Error(
					"varnishcachelog receiver goroutine panicked; stopping trace ingestion",
					zap.Any("panic", r),
					zap.ByteString("stack", debug.Stack()),
				)
			}
		}()

		if err := vsmReader.Run(ctx, func(txGrp []varnishlog.Transaction) error {
			traces := v.buildTraces(txGrp)
			if err := v.nextConsumer.ConsumeTraces(ctx, traces); err != nil {
				v.set.Logger.Error("failed to consume traces", zap.Error(err))
				return err
			}
			return nil
		}); err != nil {
			if errors.Is(err, context.Canceled) {
				v.set.Logger.Info("context was canceled", zap.Error(err))
			} else {
				componentstatus.ReportStatus(host, componentstatus.NewRecoverableErrorEvent(fmt.Errorf("failed to run vsm reader: %w", err)))
			}
		}
	})

	return nil
}

func (v *varnishcachelogTraceReceiver) Shutdown(_ context.Context) error {
	if v.cancel != nil {
		v.cancel()
	}
	v.wg.Wait()
	return nil
}

func (v *varnishcachelogTraceReceiver) buildTraces(txGrp []varnishlog.Transaction) ptrace.Traces {
	traces := ptrace.NewTraces()
	resourceSpans := traces.ResourceSpans().AppendEmpty()
	v.set.Resource.CopyTo(resourceSpans.Resource())
	resourceSpans.Resource().Attributes().PutStr(string(semconv.ServiceNameKey), "varnish")
	resourceSpans.Resource().Attributes().PutStr(string(semconv.ServiceVersionKey), varnishversion.Version())
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()
	scopeSpans.Scope().SetName(metadata.ScopeName)
	scopeSpans.Scope().SetVersion(varnishversion.Version())

	// build mapping VXID -> TX
	vtxs := map[int64]*varnishTransaction{}
	var vtxStart *varnishTransaction
	for _, tx := range txGrp {
		vtx := v.buildVtx(tx)
		// TODO(thomasklinger1234): centralize logic at one point
		vtx.VXID = tx.VXID
		vtx.VXIDParent = tx.ParentVXID
		vtx.Level = uint64(tx.Level)
		vtx.Reason = tx.Reason.String()
		vtx.Type = tx.Type.String()
		vtxs[tx.VXID] = vtx

		if vtx.VXIDParent == 0 {
			if vtxStart != nil {
				v.set.Logger.Warn("found multiple VTXs with VXIDParent=0", zap.Int64("vxid", vtx.VXID))
			}
			vtxStart = vtx
		}
	}

	if vtxStart != nil {
		v.visitVtxLinks(scopeSpans, vtxs, vtxStart, nil)
	}

	if scopeSpans.Spans().Len() == 0 {
		return ptrace.NewTraces()
	}
	return traces
}

// visitVtxLinks is the heart of the correlation logic to transform varnishTransaction objects into a full trace by following
// the extracted [`Links`] until we reach a leaf transaction (i.e. no further Links).
func (v *varnishcachelogTraceReceiver) visitVtxLinks(scopeSpans ptrace.ScopeSpans, vtxs map[int64]*varnishTransaction, vtx *varnishTransaction, vtxParent *varnishTransaction) {
	traceID, spanID, flags, tpOK := extractTraceContext(vtx)
	if !tpOK {
		v.set.Logger.Warn("failed to extract trace context", zap.Any("vtx", vtx))
		return
	}

	span := scopeSpans.Spans().AppendEmpty()
	span.Attributes().PutInt("varnish.vxid", vtx.VXID)
	span.Attributes().PutInt("varnish.vxid_parent", vtx.VXIDParent)
	span.Attributes().PutStr("varnish.tx.type", vtx.Type)
	span.Attributes().PutStr("varnish.tx.reason", vtx.Reason)
	span.Attributes().PutInt("varnish.tx.level", int64(vtx.Level))
	span.Status().SetCode(ptrace.StatusCodeOk)
	span.SetFlags(uint32(flags))
	span.SetTraceID(traceID)
	span.SetSpanID(spanID)

	if vtxParent != nil {
		span.SetParentSpanID(vtxParent.tpSID)
	} else {
		span.SetParentSpanID(vtx.tpUpstreamSID)
	}

	updateSpan(span, vtx, v.spanOpts)

	for _, link := range vtx.Links {
		if vtxNext, ok := vtxs[link.VXID]; ok {
			v.visitVtxLinks(scopeSpans, vtxs, vtxNext, vtx)
		}
	}
}

func extractTraceContext(vtx *varnishTransaction) (pcommon.TraceID, pcommon.SpanID, byte, bool) {
	if vtx.tpParsed {
		return vtx.tpTID, vtx.tpSID, vtx.tpFlags, vtx.tpOK
	}
	vtx.tpParsed = true
	tp := vtx.traceparent()
	if tp == "" {
		return pcommon.TraceID{}, pcommon.SpanID{}, 0, false
	}
	tid, sid, flags, err := extractTraceparent(tp)
	if err != nil {
		return pcommon.TraceID{}, pcommon.SpanID{}, 0, false
	}
	vtx.tpTID = tid
	vtx.tpSID = sid
	vtx.tpFlags = flags
	vtx.tpOK = true
	return tid, sid, flags, true
}

func (v *varnishcachelogTraceReceiver) buildVtx(tx varnishlog.Transaction) *varnishTransaction {
	vtx := &varnishTransaction{
		VXID:       tx.VXID,
		VXIDParent: tx.ParentVXID,
		Level:      uint64(tx.Level),
		Type:       tx.Type.String(),
		Reason:     tx.Reason.String(),
		Req: varnishTransactionReq{
			Headers: make(map[string]string),
		},
		Resp: varnishTransactionResp{
			Headers: make(map[string]string),
		},
		Events: make([]varnishTransactionEvent, 0),
		Errors: make([]string, 0),
		Logs:   make([]string, 0),
		Links:  make([]varnishTransactionLink, 0),
	}
	// track if VCL processing has already been started by waiting for the first VCL_Call tag. Before, all records
	// (ReqHeader in particular) have been set by the client. All traceparent headers found before that, can be considered
	// upstream.
	vclProcessingStarted := false

	for _, txRec := range tx.Records {
		if txRec.IsClient {
			vtx.Side = "client"
		} else if txRec.IsBackend {
			vtx.Side = "backend"
		} else {
			vtx.Side = "unknown"
		}
		if !vclProcessingStarted {
			switch txRec.Tag {
			case varnishlog.TagVCLCall:
				vclProcessingStarted = true
			case varnishlog.TagReqHeader, varnishlog.TagBereqHeader:
				if vtx.tpUpstreamSID.IsEmpty() {
					if name, val, err := parseHeader(txRec.Data); err == nil && name == requiredTraceparentHeader {
						if _, sid, _, err := extractTraceparent(val); err == nil {
							vtx.tpUpstreamSID = sid
						}
					}
				}
			}
		}
		if trans, ok := transformFuncs[txRec.Tag.String()]; ok {
			if err := trans(vtx, txRec); err != nil {
				v.set.Logger.Warn("failed to translate record",
					zap.Uint64("vxid", txRec.VXID),
					zap.String("tag", txRec.Tag.String()),
					zap.Error(err))
			}
		}
	}
	return vtx
}

func newVarnishcacheLogReceiver(set receiver.Settings, config *Config, nextConsumer consumer.Traces) receiver.Traces {
	return &varnishcachelogTraceReceiver{
		set:          set,
		cfg:          config,
		nextConsumer: nextConsumer,
		spanOpts:     buildSpanOpts(config),
	}
}

func buildHeaderMapping(mapping map[string]string) []headerMapping {
	var result []headerMapping
	for hdrName, otelAttr := range mapping {
		hdrName = strings.ToLower(strings.TrimSpace(hdrName))
		otelAttr = strings.TrimSpace(otelAttr)
		if hdrName == "" || otelAttr == "" || hdrName == requiredTraceparentHeader {
			continue
		}
		result = append(result, headerMapping{
			HdrName:     hdrName,
			OtelAttrKey: otelAttr,
		})
	}

	return result
}

func buildSpanOpts(cfg *Config) spanOpts {
	return spanOpts{
		requestHdrMapping:  buildHeaderMapping(cfg.CaptureRequestHeaders),
		responseHdrMapping: buildHeaderMapping(cfg.CaptureResponseHeaders),
	}
}
