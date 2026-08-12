package varnishcachelogreceiver

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	"github.com/thomasklinger1234/varnishotelcollector/receiver/varnishcachelogreceiver/internal/metadata"
	varnishlog "github.com/varnish/varnish-go/log"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componentstatus"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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
		return fmt.Errorf("failed to attach to VSM: %s", err.Error())
	}

	v.set.Logger.Info("successfully attached to VSM",
		zap.String("working_directory", v.cfg.WorkingDirectory),
		zap.String("vsl_query", v.cfg.VSLQuery))

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
	if v.set.Logger.Level() == zapcore.DebugLevel {
		v.logTxGroupDebug(txGrp)
	}
	vtxs, idxByVXID := v.buildVtxs(txGrp)
	traceRootVXID := computeTraceRoots(txGrp, vtxs, idxByVXID)
	traceIDByRoot := assignTraceIDs(txGrp, vtxs, traceRootVXID)

	traces := ptrace.NewTraces()
	resourceSpans := traces.ResourceSpans().AppendEmpty()
	v.set.Resource.CopyTo(resourceSpans.Resource())
	scopeSpans := resourceSpans.ScopeSpans().AppendEmpty()
	scopeSpans.Scope().SetName(metadata.ScopeName)

	for i, tx := range txGrp {
		if traceRootVXID[i] == 0 {
			continue
		}
		vtx := vtxs[i]
		isRoot := tx.VXID == traceRootVXID[i]
		txTraceID := traceIDByRoot[traceRootVXID[i]]
		_, spanID, flags, tpOK := extractTraceContext(vtx)
		if !tpOK {
			v.set.Logger.Debug("dropping tx: no traceparent header (synthetic IDs not supported)",
				zap.Int64("vxid", tx.VXID),
				zap.Int64("root_vxid", traceRootVXID[i]),
			)
			continue
		}
		if txTraceID.IsEmpty() {
			v.set.Logger.Debug("dropping tx: root has no traceparent (synthetic IDs not supported)",
				zap.Int64("vxid", tx.VXID),
				zap.Int64("root_vxid", traceRootVXID[i]),
			)
			continue
		}
		if v.cfg.RespectUpstreamSampling {
			if flags&0x01 == 0 {
				continue
			}
		}

		var parentSpanID pcommon.SpanID
		if !isRoot {
			parentIdx, ok := idxByVXID[tx.ParentVXID]
			if !ok {
				v.set.Logger.Warn("dropping tx: parent tx not found in txGrp",
					zap.Int64("vxid", tx.VXID),
					zap.Int64("parent_vxid", tx.ParentVXID),
				)
				continue
			}
			_, pSpanID, _, pOK := extractTraceContext(vtxs[parentIdx])
			if !pOK {
				v.set.Logger.Warn("dropping tx: parent tx has no traceparent (parent_span_id unresolvable)",
					zap.Int64("vxid", tx.VXID),
					zap.Int64("parent_vxid", tx.ParentVXID),
				)
				continue
			}
			parentSpanID = pSpanID
		}

		span := scopeSpans.Spans().AppendEmpty()
		if tx.VXID > 0 {
			span.Attributes().PutInt("varnish.vxid", vtx.VXID)
		}
		if !isRoot {
			span.Attributes().PutInt("varnish.vxid_parent", vtx.VXIDParent)
		}
		span.Attributes().PutStr("varnish.tx.type", vtx.Type)
		span.Attributes().PutStr("varnish.tx.reason", vtx.Reason)
		span.Status().SetCode(ptrace.StatusCodeOk)

		updateSpan(span, vtx, v.spanOpts)
		if flags&0x01 == 1 {
			span.SetFlags(0x01)
		}
		span.SetTraceID(txTraceID)
		span.SetSpanID(spanID)
		if !isRoot {
			span.SetParentSpanID(parentSpanID)
		}
	}

	if scopeSpans.Spans().Len() == 0 {
		return ptrace.NewTraces()
	}
	return traces
}

func (v *varnishcachelogTraceReceiver) logTxGroupDebug(txGrp []varnishlog.Transaction) {
	ce := v.set.Logger.Check(zap.DebugLevel, "buildTraces")
	if ce == nil {
		return
	}
	vxids := make([]int64, len(txGrp))
	parents := make([]int64, len(txGrp))
	types := make([]string, len(txGrp))
	reasons := make([]string, len(txGrp))
	beginPayloads := make([]string, len(txGrp))
	beginPayloadHex := make([]string, len(txGrp))
	for i, tx := range txGrp {
		vxids[i] = tx.VXID
		parents[i] = tx.ParentVXID
		types[i] = tx.Type.String()
		reasons[i] = tx.Reason.String()
		for _, r := range tx.Records {
			if r.Tag.String() == "Begin" {
				beginPayloads[i] = strconv.Quote(r.Data)
				beginPayloadHex[i] = hex.EncodeToString([]byte(r.Data))
				break
			}
		}
	}
	ce.Write(
		zap.Int("txGrp.len", len(txGrp)),
		zap.Int64s("vxids", vxids),
		zap.Int64s("parents", parents),
		zap.Strings("types", types),
		zap.Strings("reasons", reasons),
		zap.Strings("beginPayloads", beginPayloads),
		zap.Strings("beginPayloadHex", beginPayloadHex),
	)
}

func (v *varnishcachelogTraceReceiver) buildVtxs(txGrp []varnishlog.Transaction) ([]*varnishTransaction, map[int64]int) {
	vtxs := make([]*varnishTransaction, len(txGrp))
	idxByVXID := make(map[int64]int, len(txGrp))
	for i, tx := range txGrp {
		vtxs[i] = v.buildVtx(tx)
		idxByVXID[tx.VXID] = i
	}
	return vtxs, idxByVXID
}

// computeTraceRoots walks each tx up its ParentVXID chain until we hit a client rxreq — that
// rxreq is the trace root.
func computeTraceRoots(txGrp []varnishlog.Transaction, vtxs []*varnishTransaction, idxByVXID map[int64]int) []int64 {
	traceRootVXID := make([]int64, len(txGrp))
	for i := range txGrp {
		j := i
		for hop := 0; hop <= len(txGrp); hop++ {
			if vtxs[j].Type == "req" && vtxs[j].Reason == "rxreq" {
				traceRootVXID[i] = txGrp[j].VXID
				break
			}
			parent := txGrp[j].ParentVXID
			if parent == 0 {
				break
			}
			k, ok := idxByVXID[parent]
			if !ok {
				break
			}
			j = k
		}
	}
	return traceRootVXID
}

func assignTraceIDs(txGrp []varnishlog.Transaction, vtxs []*varnishTransaction, traceRootVXID []int64) map[int64]pcommon.TraceID {
	traceIDByRoot := make(map[int64]pcommon.TraceID, len(txGrp))
	for i, tx := range txGrp {
		root := traceRootVXID[i]
		if root == 0 || tx.VXID != root {
			continue
		}
		if tid, _, _, ok := extractTraceContext(vtxs[i]); ok {
			traceIDByRoot[root] = tid
		}
	}
	return traceIDByRoot
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
		VXID: tx.VXID,
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
		Side:   tx.Type.String(),
		Reason: tx.Reason.String(),
	}
	for _, txRec := range tx.Records {
		if txRec.IsClient {
			vtx.Side = "client"
		} else if txRec.IsBackend {
			vtx.Side = "backend"
		} else {
			vtx.Side = "unknown"
		}
		if trans, ok := transformFuncs[txRec.Tag.String()]; ok {
			if err := trans(vtx, txRec); err != nil {
				v.set.Logger.Error("failed to translate record", zap.String("record", txRec.Tag.String()), zap.Error(err))
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
		spanOpts:     builSpanOpts(config),
	}
}

func buildHeaderMapping(mapping map[string]string) []headerMapping {
	var result []headerMapping
	for k, v := range mapping {
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		if k == "" || v == "" || k == requiredTraceparentHeader {
			continue
		}
		result = append(result, headerMapping{})
	}

	return result
}

func builSpanOpts(cfg *Config) spanOpts {
	return spanOpts{
		requestHdrMapping:  buildHeaderMapping(cfg.CaptureRequestHeaders),
		responseHdrMapping: buildHeaderMapping(cfg.CaptureResponseHeaders),
	}
}
