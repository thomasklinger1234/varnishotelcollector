package varnishcachelogreceiver

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thomasklinger1234/varnishotelcollector/receiver/varnishcachelogreceiver/internal/metadata"
	varnishlog "gitlab.com/uplex/varnish/varnishapi/pkg/log"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

func tagByName(t *testing.T, name string) varnishlog.Tag {
	t.Helper()
	for i, td := range varnishlog.Tags {
		if td.String == name {
			return varnishlog.Tag(i)
		}
	}
	t.Fatalf("unknown VSL tag %q", name)
	return 0
}

func beginRecord(t *testing.T, vxid uint, txType, parentVXID, reason string) varnishlog.Record {
	t.Helper()
	return varnishlog.Record{
		Type:    varnishlog.Client,
		Tag:     tagByName(t, "Begin"),
		VXID:    vxid,
		Payload: varnishlog.Payload(fmt.Sprintf("%s %s %s", txType, parentVXID, reason)),
	}
}

func reqHeaderRecord(t *testing.T, vxid uint, name, value string) varnishlog.Record {
	t.Helper()
	return varnishlog.Record{
		Type:    varnishlog.Client,
		Tag:     tagByName(t, "ReqHeader"),
		VXID:    vxid,
		Payload: varnishlog.Payload(fmt.Sprintf("%s: %s", name, value)),
	}
}

func bereqHeaderRecord(t *testing.T, vxid uint, name, value string) varnishlog.Record {
	t.Helper()
	return varnishlog.Record{
		Type:    varnishlog.Backend,
		Tag:     tagByName(t, "BereqHeader"),
		VXID:    vxid,
		Payload: varnishlog.Payload(fmt.Sprintf("%s: %s", name, value)),
	}
}

func hexTraceID(t *testing.T, hexStr string) pcommon.TraceID {
	t.Helper()
	require.Len(t, hexStr, 32, "trace-id hex must be 32 chars")
	b, err := hex.DecodeString(hexStr)
	require.NoError(t, err)
	var id [16]byte
	copy(id[:], b)
	return pcommon.TraceID(id)
}

func hexSpanID(t *testing.T, hexStr string) pcommon.SpanID {
	t.Helper()
	require.Len(t, hexStr, 16, "span-id hex must be 16 chars")
	b, err := hex.DecodeString(hexStr)
	require.NoError(t, err)
	var id [8]byte
	copy(id[:], b)
	return pcommon.SpanID(id)
}

func newTestReceiver(t *testing.T) varnishcachelogReceiver {
	t.Helper()
	return varnishcachelogReceiver{
		set: receivertest.NewNopSettings(metadata.Type),
		cfg: createDefaultConfig().(*Config),
	}
}

// TestBuildTraces_CacheHitRxreqNoChildren is the regression test for the
// "two top-level requests merged into one trace" bug: when an rxreq has
// no sub-transactions the vendored library does not normalise
// tx.ParentVXID to 0, so the receiver must not use ParentVXID to detect
// the trace root. See buildTraces() for the invariant this guards.
func TestBuildTraces_CacheHitRxreqNoChildren(t *testing.T) {
	const (
		sessionVXID uint32 = 1
		rxreqVXID   uint32 = 42
	)

	rcv := newTestReceiver(t)

	txGrp := []varnishlog.Tx{
		{
			Type:       varnishlog.Req,
			Reason:     varnishlog.RxReq,
			Level:      1,
			VXID:       rxreqVXID,
			ParentVXID: sessionVXID,
			Records: []varnishlog.Record{
				beginRecord(t, uint(rxreqVXID), "req", "1", "rxreq"),
			},
		},
	}

	traces := rcv.buildTraces(txGrp)

	require.Equal(t, 1, traces.ResourceSpans().Len())
	spans := traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	require.Equal(t, 1, spans.Len())

	span := spans.At(0)

	wantTraceID := generateTraceID(uint64(rxreqVXID))
	assert.Equal(t, wantTraceID, span.TraceID(), "cache-hit rxreq must derive trace ID from its own VXID, not fall back to empty")
	assert.NotEqual(t, pcommon.NewTraceIDEmpty(), span.TraceID(), "cache-hit rxreq must not emit the all-zero trace ID")

	assert.Equal(t, generateSpanID(uint64(rxreqVXID)), span.SpanID())
	assert.Equal(t, pcommon.NewSpanIDEmpty(), span.ParentSpanID(), "root rxreq must not reference a parent span (the session is never emitted)")

	_, hasVxidParent := span.Attributes().Get("varnish.vxid_parent")
	assert.False(t, hasVxidParent, "root rxreq must not carry varnish.vxid_parent (would point at a phantom session span)")
}

// TestBuildTraces_TwoCacheHitRxreqsSameSession reproduces the exact
// user-observed symptom: two independent top-level requests on the same
// TCP session must end up in two distinct traces, not merged into one
// via a shared all-zero trace ID.
func TestBuildTraces_TwoCacheHitRxreqsSameSession(t *testing.T) {
	const sessionVXID uint32 = 1

	rcv := newTestReceiver(t)

	buildOne := func(rxreqVXID uint32) pcommon.TraceID {
		txGrp := []varnishlog.Tx{
			{
				Type:       varnishlog.Req,
				Reason:     varnishlog.RxReq,
				Level:      1,
				VXID:       rxreqVXID,
				ParentVXID: sessionVXID,
				Records: []varnishlog.Record{
					beginRecord(t, uint(rxreqVXID), "req", "1", "rxreq"),
				},
			},
		}
		traces := rcv.buildTraces(txGrp)
		require.Equal(t, 1, traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans().Len())
		return traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).TraceID()
	}

	traceA := buildOne(42)
	traceB := buildOne(43)

	assert.NotEqual(t, traceA, traceB, "two rxreqs on the same session must produce distinct traces")
	assert.NotEqual(t, pcommon.NewTraceIDEmpty(), traceA)
	assert.NotEqual(t, pcommon.NewTraceIDEmpty(), traceB)
}

// TestBuildTraces_RxreqWithBereqChild covers the happy path the library
// already normalises: all spans in the group must share the trace ID
// derived from the root rxreq's VXID, descendants must reference the
// root as their parent span.
func TestBuildTraces_RxreqWithBereqChild(t *testing.T) {
	const (
		rxreqVXID uint32 = 2
		bereqVXID uint32 = 3
	)

	rcv := newTestReceiver(t)

	txGrp := []varnishlog.Tx{
		{
			Type:       varnishlog.Req,
			Reason:     varnishlog.RxReq,
			Level:      1,
			VXID:       rxreqVXID,
			ParentVXID: 0,
			Records: []varnishlog.Record{
				beginRecord(t, uint(rxreqVXID), "req", "1", "rxreq"),
			},
		},
		{
			Type:       varnishlog.BeReq,
			Reason:     varnishlog.Fetch,
			Level:      2,
			VXID:       bereqVXID,
			ParentVXID: rxreqVXID,
			Records: []varnishlog.Record{
				beginRecord(t, uint(bereqVXID), "bereq", fmt.Sprintf("%d", rxreqVXID), "fetch"),
			},
		},
	}

	traces := rcv.buildTraces(txGrp)

	spans := traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	require.Equal(t, 2, spans.Len())

	wantTraceID := generateTraceID(uint64(rxreqVXID))

	rootSpan := spans.At(0)
	assert.Equal(t, wantTraceID, rootSpan.TraceID())
	assert.Equal(t, generateSpanID(uint64(rxreqVXID)), rootSpan.SpanID())
	assert.Equal(t, pcommon.NewSpanIDEmpty(), rootSpan.ParentSpanID(), "root must not reference a parent span")
	_, rootHasVxidParent := rootSpan.Attributes().Get("varnish.vxid_parent")
	assert.False(t, rootHasVxidParent, "root must not carry varnish.vxid_parent")

	childSpan := spans.At(1)
	assert.Equal(t, wantTraceID, childSpan.TraceID(), "descendant must share the root trace ID")
	assert.Equal(t, generateSpanID(uint64(bereqVXID)), childSpan.SpanID())
	assert.Equal(t, generateSpanID(uint64(rxreqVXID)), childSpan.ParentSpanID(), "bereq must reference the rxreq as its parent span")
	vxidParent, childHasVxidParent := childSpan.Attributes().Get("varnish.vxid_parent")
	require.True(t, childHasVxidParent, "descendant must carry varnish.vxid_parent")
	assert.Equal(t, int64(rxreqVXID), vxidParent.Int())
}

// TestBuildTraces_SessionRootedGroup_Varnish9 reproduces the shape of the
// txGrp the vendored library actually returns on Varnish 9: the session
// is at index 0 (library failed to identify it as Sess due to trailing
// NUL bytes in the Begin payload), rxreqs are children of the session,
// and each rxreq has its own descendants. buildTraces must:
//   - drop the session span (not a real trace root)
//   - emit one trace per rxreq keyed on the rxreq's VXID
//   - keep each descendant in its rxreq ancestor's trace
//   - NOT merge two rxreqs on the same TCP session into one trace
func TestBuildTraces_SessionRootedGroup_Varnish9(t *testing.T) {
	const (
		sessVXID   uint32 = 100
		rxreqAVXID uint32 = 101
		rxreqBVXID uint32 = 103
		bereqAVXID uint32 = 102
		bereqBVXID uint32 = 104
	)

	rcv := newTestReceiver(t)

	txGrp := []varnishlog.Tx{
		{
			Type: varnishlog.TxUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 1, VXID: sessVXID, ParentVXID: 0,
			Records: []varnishlog.Record{beginRecord(t, uint(sessVXID), "sess", "0", "HTTP/1")},
		},
		{
			Type: varnishlog.TxUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 2, VXID: rxreqAVXID, ParentVXID: sessVXID,
			Records: []varnishlog.Record{beginRecord(t, uint(rxreqAVXID), "req", fmt.Sprintf("%d", sessVXID), "rxreq")},
		},
		{
			Type: varnishlog.TxUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 2, VXID: rxreqBVXID, ParentVXID: sessVXID,
			Records: []varnishlog.Record{beginRecord(t, uint(rxreqBVXID), "req", fmt.Sprintf("%d", sessVXID), "rxreq")},
		},
		{
			Type: varnishlog.TxUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 3, VXID: bereqAVXID, ParentVXID: rxreqAVXID,
			Records: []varnishlog.Record{beginRecord(t, uint(bereqAVXID), "bereq", fmt.Sprintf("%d", rxreqAVXID), "fetch")},
		},
		{
			Type: varnishlog.TxUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 3, VXID: bereqBVXID, ParentVXID: rxreqBVXID,
			Records: []varnishlog.Record{beginRecord(t, uint(bereqBVXID), "bereq", fmt.Sprintf("%d", rxreqBVXID), "fetch")},
		},
	}

	traces := rcv.buildTraces(txGrp)

	spans := traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	require.Equal(t, 4, spans.Len(), "session must be dropped; 2 rxreqs + 2 bereqs = 4 spans")

	find := func(vxid uint32) (ptrace.Span, bool) {
		for i := 0; i < spans.Len(); i++ {
			s := spans.At(i)
			v, has := s.Attributes().Get("varnish.vxid")
			if has && uint32(v.Int()) == vxid {
				return s, true
			}
		}
		return ptrace.Span{}, false
	}

	_, sessEmitted := find(sessVXID)
	assert.False(t, sessEmitted, "session VXID must not appear in emitted spans")

	rxreqA, ok := find(rxreqAVXID)
	require.True(t, ok, "rxreq A must be emitted")
	traceA := generateTraceID(uint64(rxreqAVXID))
	assert.Equal(t, traceA, rxreqA.TraceID(), "rxreq A trace ID must derive from its own VXID")
	assert.Equal(t, pcommon.NewSpanIDEmpty(), rxreqA.ParentSpanID(), "rxreq A must be a trace root")
	_, rootAHasParent := rxreqA.Attributes().Get("varnish.vxid_parent")
	assert.False(t, rootAHasParent, "rxreq root must not carry varnish.vxid_parent")

	rxreqB, ok := find(rxreqBVXID)
	require.True(t, ok, "rxreq B must be emitted")
	traceB := generateTraceID(uint64(rxreqBVXID))
	assert.Equal(t, traceB, rxreqB.TraceID())
	assert.NotEqual(t, traceA, traceB, "two rxreqs on the same session must NOT be merged into one trace")
	assert.Equal(t, pcommon.NewSpanIDEmpty(), rxreqB.ParentSpanID())

	bereqA, ok := find(bereqAVXID)
	require.True(t, ok)
	assert.Equal(t, traceA, bereqA.TraceID(), "bereq A must inherit rxreq A's trace ID")
	assert.Equal(t, generateSpanID(uint64(rxreqAVXID)), bereqA.ParentSpanID())

	bereqB, ok := find(bereqBVXID)
	require.True(t, ok)
	assert.Equal(t, traceB, bereqB.TraceID(), "bereq B must inherit rxreq B's trace ID")
	assert.Equal(t, generateSpanID(uint64(rxreqBVXID)), bereqB.ParentSpanID())
}

// TestBuildTraces_TraceparentDrivesIDs exercises Fix B: when the VCL
// (see dev/varnish/otel.vcl) puts a W3C traceparent on every req-side
// transaction, the receiver must derive trace_id / span_id / parent_span_id
// from that header so the collector's spans stitch with upstream RUM and
// downstream backend traces.
//
// The scenario models one browser navigation whose main page contains one
// ESI include. The VCL is expected to have propagated the top-level
// traceparent into the ESI sub-request (Fix A) so all four transactions
// share one trace_id.
func TestBuildTraces_TraceparentDrivesIDs(t *testing.T) {
	const (
		sessVXID  uint32 = 100
		rxreqVXID uint32 = 101
		bereqVXID uint32 = 102
		esiVXID   uint32 = 103
		esiBqVXID uint32 = 104

		traceIDHex     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		rxreqSpanIDHex = "1111111111111111"
		esiSpanIDHex   = "2222222222222222"
	)
	rxreqTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, rxreqSpanIDHex)
	esiTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, esiSpanIDHex)

	rcv := newTestReceiver(t)

	txGrp := []varnishlog.Tx{
		{
			Type: varnishlog.TxUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 1, VXID: sessVXID, ParentVXID: 0,
			Records: []varnishlog.Record{beginRecord(t, uint(sessVXID), "sess", "0", "HTTP/1")},
		},
		{
			Type: varnishlog.TxUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 2, VXID: rxreqVXID, ParentVXID: sessVXID,
			Records: []varnishlog.Record{
				beginRecord(t, uint(rxreqVXID), "req", fmt.Sprintf("%d", sessVXID), "rxreq"),
				reqHeaderRecord(t, uint(rxreqVXID), "traceparent", rxreqTP),
			},
		},
		{
			Type: varnishlog.TxUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 3, VXID: bereqVXID, ParentVXID: rxreqVXID,
			Records: []varnishlog.Record{
				beginRecord(t, uint(bereqVXID), "bereq", fmt.Sprintf("%d", rxreqVXID), "fetch"),
				bereqHeaderRecord(t, uint(bereqVXID), "traceparent", rxreqTP),
			},
		},
		{
			Type: varnishlog.TxUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 3, VXID: esiVXID, ParentVXID: rxreqVXID,
			Records: []varnishlog.Record{
				beginRecord(t, uint(esiVXID), "req", fmt.Sprintf("%d", rxreqVXID), "esi"),
				reqHeaderRecord(t, uint(esiVXID), "traceparent", esiTP),
			},
		},
		{
			Type: varnishlog.TxUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 4, VXID: esiBqVXID, ParentVXID: esiVXID,
			Records: []varnishlog.Record{
				beginRecord(t, uint(esiBqVXID), "bereq", fmt.Sprintf("%d", esiVXID), "fetch"),
				bereqHeaderRecord(t, uint(esiBqVXID), "traceparent", esiTP),
			},
		},
	}

	traces := rcv.buildTraces(txGrp)

	spans := traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	require.Equal(t, 4, spans.Len(), "session dropped; rxreq + bereq + esi + esi's bereq = 4 spans")

	find := func(vxid uint32) (ptrace.Span, bool) {
		for i := 0; i < spans.Len(); i++ {
			s := spans.At(i)
			v, has := s.Attributes().Get("varnish.vxid")
			if has && uint32(v.Int()) == vxid {
				return s, true
			}
		}
		return ptrace.Span{}, false
	}

	wantTraceID := hexTraceID(t, traceIDHex)
	wantRxreqSpanID := hexSpanID(t, rxreqSpanIDHex)
	wantEsiSpanID := hexSpanID(t, esiSpanIDHex)

	rxreq, ok := find(rxreqVXID)
	require.True(t, ok)
	assert.Equal(t, wantTraceID, rxreq.TraceID(), "trace ID must come from rxreq's traceparent, not from VXID")
	assert.Equal(t, wantRxreqSpanID, rxreq.SpanID(), "rxreq span ID must come from its traceparent parent_id (VCL-generated Varnish span)")
	assert.Equal(t, pcommon.NewSpanIDEmpty(), rxreq.ParentSpanID())

	bereq, ok := find(bereqVXID)
	require.True(t, ok)
	assert.Equal(t, wantTraceID, bereq.TraceID(), "bereq must share the rxreq trace ID")
	assert.Equal(t, generateSpanID(uint64(bereqVXID)), bereq.SpanID(), "bereq span ID stays synthetic (VCL does not mint a bereq-specific span)")
	assert.Equal(t, wantRxreqSpanID, bereq.ParentSpanID(), "bereq parent must be the parent req's Varnish span (from bereq's own traceparent parent_id)")

	esi, ok := find(esiVXID)
	require.True(t, ok)
	assert.Equal(t, wantTraceID, esi.TraceID(), "ESI sub must share the rxreq trace ID (VCL propagated traceparent from req_top)")
	assert.Equal(t, wantEsiSpanID, esi.SpanID(), "ESI span ID from its own traceparent parent_id")
	assert.Equal(t, wantRxreqSpanID, esi.ParentSpanID(), "ESI parent must be the parent req's Varnish span, looked up in the txGrp")

	esiBq, ok := find(esiBqVXID)
	require.True(t, ok)
	assert.Equal(t, wantTraceID, esiBq.TraceID())
	assert.Equal(t, generateSpanID(uint64(esiBqVXID)), esiBq.SpanID())
	assert.Equal(t, wantEsiSpanID, esiBq.ParentSpanID(), "ESI's bereq parent = ESI's Varnish span (from bereq traceparent)")
}

// TestBuildTraces_TraceparentFallback guards against silent breakage:
// when traceparent is missing or malformed, the receiver must fall back
// to VXID-derived synthetic IDs and still emit a well-formed trace.
func TestBuildTraces_TraceparentFallback(t *testing.T) {
	const (
		rxreqVXID uint32 = 200
		bereqVXID uint32 = 201
	)

	rcv := newTestReceiver(t)

	txGrp := []varnishlog.Tx{
		{
			Type: varnishlog.TxUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 1, VXID: rxreqVXID, ParentVXID: 0,
			Records: []varnishlog.Record{
				beginRecord(t, uint(rxreqVXID), "req", "0", "rxreq"),
				reqHeaderRecord(t, uint(rxreqVXID), "traceparent", "not-a-valid-traceparent"),
			},
		},
		{
			Type: varnishlog.TxUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 2, VXID: bereqVXID, ParentVXID: rxreqVXID,
			Records: []varnishlog.Record{
				beginRecord(t, uint(bereqVXID), "bereq", fmt.Sprintf("%d", rxreqVXID), "fetch"),
			},
		},
	}

	traces := rcv.buildTraces(txGrp)
	spans := traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	require.Equal(t, 2, spans.Len())

	rxreq := spans.At(0)
	assert.Equal(t, generateTraceID(uint64(rxreqVXID)), rxreq.TraceID(), "malformed traceparent must fall back to synthetic trace ID")
	assert.Equal(t, generateSpanID(uint64(rxreqVXID)), rxreq.SpanID(), "malformed traceparent must fall back to synthetic span ID")

	bereq := spans.At(1)
	assert.Equal(t, generateTraceID(uint64(rxreqVXID)), bereq.TraceID())
	assert.Equal(t, generateSpanID(uint64(bereqVXID)), bereq.SpanID())
	assert.Equal(t, generateSpanID(uint64(rxreqVXID)), bereq.ParentSpanID(), "no traceparent on either tx → parent span-id from synthetic lookup on parent VXID")
}

// TestBuildTraces_NoOtelLinks locks in the removal of OTel Links from
// emitted spans. The parent-child structure the receiver builds is
// already fully expressed via SetParentSpanID, so emitting OTel Links
// (which are meant for non-hierarchical references) only duplicated
// that data — and reintroducing them was the origin of a Grafana
// waterfall crash. This test would have caught the original bug and
// stops anyone from re-adding a links loop by mistake.
func TestBuildTraces_NoOtelLinks(t *testing.T) {
	const (
		rxreqVXID uint32 = 300
		esiVXID   uint32 = 301
		bereqVXID uint32 = 302
	)

	rcv := newTestReceiver(t)

	linkRec := varnishlog.Record{
		Type:    varnishlog.Client,
		Tag:     tagByName(t, "Link"),
		VXID:    uint(rxreqVXID),
		Payload: varnishlog.Payload(fmt.Sprintf("req %d esi", esiVXID)),
	}

	txGrp := []varnishlog.Tx{
		{
			Type: varnishlog.TxUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 1, VXID: rxreqVXID, ParentVXID: 0,
			Records: []varnishlog.Record{
				beginRecord(t, uint(rxreqVXID), "req", "0", "rxreq"),
				linkRec,
			},
		},
		{
			Type: varnishlog.TxUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 2, VXID: esiVXID, ParentVXID: rxreqVXID,
			Records: []varnishlog.Record{
				beginRecord(t, uint(esiVXID), "req", fmt.Sprintf("%d", rxreqVXID), "esi"),
			},
		},
		{
			Type: varnishlog.TxUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 3, VXID: bereqVXID, ParentVXID: esiVXID,
			Records: []varnishlog.Record{
				beginRecord(t, uint(bereqVXID), "bereq", fmt.Sprintf("%d", esiVXID), "fetch"),
			},
		},
	}

	traces := rcv.buildTraces(txGrp)
	spans := traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans()

	for i := 0; i < spans.Len(); i++ {
		assert.Equal(t, 0, spans.At(i).Links().Len(), "span %v must not carry OTel Links (parent-child already expresses the same relationship via parent_span_id)", spans.At(i).SpanID())
	}
}

// TestBuildTraces_EmptyGroupProducesEmptyTraces guards against emitting
// a ptrace.Traces with a ResourceSpans/ScopeSpans that has zero spans —
// downstream consumers (notably Grafana's trace viewer) crash on empty
// spans arrays.
func TestBuildTraces_EmptyGroupProducesEmptyTraces(t *testing.T) {
	rcv := newTestReceiver(t)

	txGrp := []varnishlog.Tx{
		{
			Type: varnishlog.TxUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 1, VXID: 400, ParentVXID: 0,
			Records: []varnishlog.Record{beginRecord(t, 400, "sess", "0", "HTTP/1")},
		},
	}

	traces := rcv.buildTraces(txGrp)
	assert.Equal(t, 0, traces.SpanCount(), "session-only txGrp must emit zero spans")
	assert.Equal(t, 0, traces.ResourceSpans().Len(), "must not emit an empty ResourceSpans block")
}
