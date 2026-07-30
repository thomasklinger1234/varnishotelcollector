package varnishcachelogreceiver

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/thomasklinger1234/varnishotelcollector/receiver/varnishcachelogreceiver/internal/metadata"
	varnishlog "gitlab.com/uplex/varnish/varnishapi/pkg/log"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"
)

// vslIdleSleep bounds the sleep between polls when the live VSL buffer
// is momentarily empty (NextTxGroup returned EOL). Without this sleep
// the CPU usage will stay at 100%, because the loop will query the VSL
// as often as possible.
const vslIdleSleep = 10 * time.Millisecond

// VSM Retry with exponential backoff on attach failures.
const (
	vslAttachMaxAttempts    = 10
	vslAttachInitialBackoff = 100 * time.Millisecond
)

// vslFatalStatus maps terminal VSL statuses to their log messages.
var vslFatalStatus = map[varnishlog.Status]string{
	varnishlog.EOF:       "VSL EOF",
	varnishlog.Abandoned: "VSL abandoned",
	varnishlog.IOErr:     "VSL IOErr",
	varnishlog.WriteErr:  "VSL WriteErr",
	varnishlog.Overrun:   "VSL overrun",
}

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

	if err := attachVSMWithRetry(ctx, v.set.Logger, vsm, v.cfg.WorkingDirectory); err != nil {
		return fmt.Errorf("failed to attach to vsm after %d attempts: %w", vslAttachMaxAttempts, err)
	}

	vsmCursor, err := vsm.NewCursor()
	if err != nil {
		return fmt.Errorf("failed to create vsm cursor: %w", err)
	}

	vsmQuery, err := vsmCursor.NewQuery(varnishlog.Request, v.cfg.VSLQuery)
	if err != nil {
		return fmt.Errorf("failed to create vsm query: %w", err)
	}
	v.set.Logger.Info("varnishcachelog receiver successfully attached to VSM", zap.String("working_directory", v.cfg.WorkingDirectory), zap.String("vsl_query", v.cfg.VSLQuery))

	v.wg.Go(func() {
		defer func() {
			vsmCursor.Delete()
			vsm.Release()
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
		for {
			select {
			case <-ctx.Done():
				return
			default:
				txGrp, txStatus := vsmQuery.NextTxGroup()
				if txStatus == varnishlog.EOL {
					select {
					case <-ctx.Done():
						return
					case <-time.After(vslIdleSleep):
					}
					continue
				}
				if msg, fatal := vslFatalStatus[txStatus]; fatal {
					v.set.Logger.Error(msg, zap.String("error", vsm.Error()))
					return
				}
				traces := v.buildTraces(txGrp)
				if err := v.nextConsumer.ConsumeTraces(ctx, traces); err != nil {
					v.set.Logger.Error("failed to consume traces", zap.Error(err))
				}
			}
		}
	})
	return nil
}

func (v varnishcachelogReceiver) Shutdown(_ context.Context) error {
	v.wg.Wait()
	return nil
}

// Retry on the same VSM handle: vsm.Attach calls VSM_ResetError each
// invocation (see vendored pkg/vsm/vsm.go).
func attachVSMWithRetry(ctx context.Context, logger *zap.Logger, vsm *varnishlog.Log, workingDirectory string) error {
	backoff := vslAttachInitialBackoff
	var lastErr error
	for attempt := 1; attempt <= vslAttachMaxAttempts; attempt++ {
		lastErr = vsm.Attach(workingDirectory)
		if lastErr == nil {
			if attempt > 1 {
				logger.Info("vsm attach succeeded after retry", zap.Int("attempt", attempt))
			}
			return nil
		}
		if attempt == vslAttachMaxAttempts {
			break
		}
		logger.Warn("vsm attach failed, retrying",
			zap.Int("attempt", attempt),
			zap.Int("max_attempts", vslAttachMaxAttempts),
			zap.Duration("backoff", backoff),
			zap.Error(lastErr),
		)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return lastErr
}

func (v varnishcachelogReceiver) buildTraces(txGrp []varnishlog.Tx) ptrace.Traces {
	v.logTxGroupDebug(txGrp)
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

		spanID := resolveSpanID(vtx)

		var parentSpanID pcommon.SpanID
		if !isRoot {
			// Every tx (rxreq, ESI sub, bereq) carries its OWN span-id
			// via its traceparent header.
			//parentSpanID = generateSpanID(uint64(tx.ParentVXID))
			if parentIdx, ok := idxByVXID[tx.ParentVXID]; ok {
				parentSpanID = resolveSpanID(vtxs[parentIdx])
			}
		}

		span := scopeSpans.Spans().AppendEmpty()
		if tx.VXID > 0 {
			span.Attributes().PutInt("varnish.vxid", int64(vtx.VXID))
		}
		if !isRoot {
			span.Attributes().PutInt("varnish.vxid_parent", int64(vtx.VXIDParent))
		}
		span.Attributes().PutStr("varnish.tx.type", vtx.Type)
		span.Attributes().PutStr("varnish.tx.reason", vtx.Reason)
		span.Status().SetCode(ptrace.StatusCodeOk)

		if err := updateSpan(span, vtx); err != nil {
			v.set.Logger.Error("failed to update span", zap.String("span", txTraceID.String()), zap.Error(err))
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

func (v varnishcachelogReceiver) logTxGroupDebug(txGrp []varnishlog.Tx) {
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
		vxids[i] = int64(tx.VXID)
		parents[i] = int64(tx.ParentVXID)
		types[i] = tx.Type.String()
		reasons[i] = tx.Reason.String()
		for _, r := range tx.Records {
			if r.Tag.String() == "Begin" {
				beginPayloads[i] = strconv.Quote(string(r.Payload))
				beginPayloadHex[i] = hex.EncodeToString(r.Payload)
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

func (v varnishcachelogReceiver) buildVtxs(txGrp []varnishlog.Tx) ([]*varnishTransaction, map[uint32]int) {
	vtxs := make([]*varnishTransaction, len(txGrp))
	idxByVXID := make(map[uint32]int, len(txGrp))
	for i, tx := range txGrp {
		vtxs[i] = v.buildVtx(tx)
		idxByVXID[tx.VXID] = i
	}
	return vtxs, idxByVXID
}

// Walk each tx up its ParentVXID chain until we hit a client rxreq — that
// rxreq is the trace root. This bypasses the vendored library's broken
// reason parsing on Varnish 9 (payloads carry a trailing NUL byte which
// makes bytes.Equal fail for rxreq/fetch/HTTP/1 but not for esi/restart,
// so the library ends up building session-rooted txGrps that merge every
// keep-alive request into one trace). vtx.Type and vtx.Reason come from
// our own strings.Fields + trimUnprintableChars pass, which handles NUL.
func computeTraceRoots(txGrp []varnishlog.Tx, vtxs []*varnishTransaction, idxByVXID map[uint32]int) []uint32 {
	traceRootVXID := make([]uint32, len(txGrp))
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

// Prefer trace IDs and per-hop span IDs from the W3C traceparent header
// the VCL propagates (see dev/varnish/otel.vcl). The receiver falls back
// to VXID-derived synthetic IDs when no traceparent is present so unit
// tests and non-VCL deployments keep working. Only the root rxreq's
// traceparent supplies the trace ID for the whole subtree — descendants
// (ESI subs, bereqs) never override it, so keep-alive requests and ESI
// cascades stay in one trace with the same ID nginx/backends see.
func assignTraceIDs(txGrp []varnishlog.Tx, vtxs []*varnishTransaction, traceRootVXID []uint32) map[uint32]pcommon.TraceID {
	traceIDByRoot := make(map[uint32]pcommon.TraceID, len(txGrp))
	for i, tx := range txGrp {
		root := traceRootVXID[i]
		if root == 0 || tx.VXID != root {
			continue
		}
		if tid, _, ok := extractTraceContext(vtxs[i]); ok {
			traceIDByRoot[root] = tid
		} else {
			traceIDByRoot[root] = generateTraceID(uint64(root))
		}
	}
	return traceIDByRoot
}

func resolveSpanID(vtx *varnishTransaction) pcommon.SpanID {
	if _, tpSpanID, ok := extractTraceContext(vtx); ok {
		return tpSpanID
	}
	return generateSpanID(vtx.VXID)
}

func extractTraceContext(vtx *varnishTransaction) (pcommon.TraceID, pcommon.SpanID, bool) {
	tp, ok := vtx.Req.Headers["traceparent"]
	if !ok {
		return pcommon.TraceID{}, pcommon.SpanID{}, false
	}
	tid, sid, err := extractTraceparent(tp)
	if err != nil {
		return pcommon.TraceID{}, pcommon.SpanID{}, false
	}
	return tid, sid, true
}

func (v varnishcachelogReceiver) buildVtx(tx varnishlog.Tx) *varnishTransaction {
	vtx := &varnishTransaction{
		VXID: uint64(tx.VXID),
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
		switch txRec.Type {
		case varnishlog.Client:
			vtx.Side = "client"
		case varnishlog.Backend:
			vtx.Side = "backend"
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
	return &varnishcachelogReceiver{
		set:          set,
		cfg:          config,
		nextConsumer: nextConsumer,
	}
}

func generateTraceID(vxid uint64) pcommon.TraceID {
	var id [16]byte
	binary.BigEndian.PutUint64(id[0:16], vxid)
	return pcommon.TraceID(id)
}

func generateSpanID(vxid uint64) pcommon.SpanID {
	var id [8]byte
	binary.BigEndian.PutUint64(id[0:8], vxid)
	return pcommon.SpanID(id)
}
