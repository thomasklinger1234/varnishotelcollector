package varnishcachelogreceiver

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thomasklinger1234/varnishotelcollector/receiver/varnishcachelogreceiver/internal/metadata"
	varnishlog "github.com/varnish/varnish-go/log"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

func tagByName(t *testing.T, name string) varnishlog.Tag {
	t.Helper()
	if tag, err := varnishlog.TagByName(name); err != nil {
		t.Fatalf("unknown VSL tag %q", name)
		return 0
	} else {
		return tag
	}
}

func beginRecord(t *testing.T, vxid uint64, txType, parentVXID, reason string) varnishlog.Record {
	t.Helper()
	return varnishlog.Record{
		Tag:       varnishlog.TagBegin,
		VXID:      vxid,
		IsClient:  false,
		IsBackend: false,
		Data:      fmt.Sprintf("%s %s %s", txType, parentVXID, reason),
	}
}

func reqHeaderRecord(t *testing.T, vxid uint64, name, value string) varnishlog.Record {
	t.Helper()
	return varnishlog.Record{
		Tag:       varnishlog.TagReqHeader,
		VXID:      vxid,
		IsClient:  false,
		IsBackend: false,
		Data:      fmt.Sprintf("%s: %s", name, value),
	}
}

func bereqHeaderRecord(t *testing.T, vxid uint64, name, value string) varnishlog.Record {
	t.Helper()
	return varnishlog.Record{
		Tag:       varnishlog.TagBereqHeader,
		VXID:      vxid,
		IsClient:  false,
		IsBackend: false,
		Data:      fmt.Sprintf("%s: %s", name, value),
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

func newTestReceiver(t *testing.T) *varnishcachelogTraceReceiver {
	t.Helper()
	cfg := createDefaultConfig().(*Config)
	return &varnishcachelogTraceReceiver{
		set:      receivertest.NewNopSettings(metadata.Type),
		cfg:      cfg,
		spanOpts: buildSpanOpts(cfg),
	}
}

func newTestReceiverWithHeaders(t *testing.T, captured map[string]string) *varnishcachelogTraceReceiver {
	t.Helper()
	cfg := createDefaultConfig().(*Config)
	cfg.CaptureRequestHeaders = captured
	return &varnishcachelogTraceReceiver{
		set:      receivertest.NewNopSettings(metadata.Type),
		cfg:      cfg,
		spanOpts: buildSpanOpts(cfg),
	}
}

// TestBuildTraces_CacheHitRxreqNoChildren is the regression test for the
// "two top-level requests merged into one trace" bug: when an rxreq has
// no sub-transactions the vendored library does not normalise
// tx.ParentVXID to 0, so the receiver must not use ParentVXID to detect
// the trace root. See buildTraces() for the invariant this guards.
func TestBuildTraces_CacheHitRxreqNoChildren(t *testing.T) {
	const (
		sessionVXID uint64 = 1
		rxreqVXID   uint64 = 42

		traceIDHex     = "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"
		rxreqSpanIDHex = "b1b1b1b1b1b1b1b1"
	)
	rxreqTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, rxreqSpanIDHex)

	rcv := newTestReceiver(t)

	txGrp := []varnishlog.Transaction{{
		Level:      1,
		VXID:       int64(rxreqVXID),
		ParentVXID: int64(sessionVXID),
		Type:       varnishlog.TypeRequest,
		Reason:     varnishlog.ReasonRxReq,
		Records: []varnishlog.Record{
			beginRecord(t, rxreqVXID, "req", "1", "rxreq"),
			reqHeaderRecord(t, rxreqVXID, "traceparent", rxreqTP),
		},
	}}

	traces := rcv.buildTraces(txGrp)

	require.Equal(t, 1, traces.ResourceSpans().Len())
	spans := traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	require.Equal(t, 1, spans.Len())

	span := spans.At(0)

	wantTraceID := hexTraceID(t, traceIDHex)
	assert.Equal(t, wantTraceID, span.TraceID(), "cache-hit rxreq must derive trace ID from its traceparent")
	assert.NotEqual(t, pcommon.NewTraceIDEmpty(), span.TraceID(), "cache-hit rxreq must not emit the all-zero trace ID")

	assert.Equal(t, hexSpanID(t, rxreqSpanIDHex), span.SpanID())
	assert.Equal(t, pcommon.NewSpanIDEmpty(), span.ParentSpanID(), "root rxreq must not reference a parent span (the session is never emitted)")

	_, hasVxidParent := span.Attributes().Get("varnish.vxid_parent")
	assert.False(t, hasVxidParent, "root rxreq must not carry varnish.vxid_parent (would point at a phantom session span)")
}

// TestBuildTraces_TwoCacheHitRxreqsSameSession reproduces the exact
// user-observed symptom: two independent top-level requests on the same
// TCP session must end up in two distinct traces, not merged into one
// via a shared all-zero trace ID.
func TestBuildTraces_TwoCacheHitRxreqsSameSession(t *testing.T) {
	const sessionVXID uint64 = 1

	rcv := newTestReceiver(t)

	buildOne := func(rxreqVXID uint64, traceIDHex, spanIDHex string) pcommon.TraceID {
		tp := fmt.Sprintf("00-%s-%s-01", traceIDHex, spanIDHex)
		txGrp := []varnishlog.Transaction{
			{
				Type:       varnishlog.TypeRequest,
				Reason:     varnishlog.ReasonRxReq,
				Level:      1,
				VXID:       int64(rxreqVXID),
				ParentVXID: int64(sessionVXID),
				Records: []varnishlog.Record{
					beginRecord(t, rxreqVXID, "req", "1", "rxreq"),
					reqHeaderRecord(t, rxreqVXID, "traceparent", tp),
				},
			},
		}
		traces := rcv.buildTraces(txGrp)
		require.Equal(t, 1, traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans().Len())
		return traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).TraceID()
	}

	traceA := buildOne(42, "a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2", "b2b2b2b2b2b2b2b2")
	traceB := buildOne(43, "c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2", "d2d2d2d2d2d2d2d2")

	assert.NotEqual(t, traceA, traceB, "two rxreqs on the same session must produce distinct traces")
	assert.NotEqual(t, pcommon.NewTraceIDEmpty(), traceA)
	assert.NotEqual(t, pcommon.NewTraceIDEmpty(), traceB)
}

// TestBuildTraces_RxreqWithBereqChild covers the happy path the library
// already normalises: all spans in the group must share the trace ID
// derived from the root rxreq's traceparent, descendants must reference
// the root as their parent span.
func TestBuildTraces_RxreqWithBereqChild(t *testing.T) {
	const (
		rxreqVXID uint64 = 2
		bereqVXID uint64 = 3

		traceIDHex     = "a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3a3"
		rxreqSpanIDHex = "b3b3b3b3b3b3b3b3"
		bereqSpanIDHex = "c3c3c3c3c3c3c3c3"
	)
	rxreqTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, rxreqSpanIDHex)
	bereqTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, bereqSpanIDHex)

	rcv := newTestReceiver(t)

	txGrp := []varnishlog.Transaction{
		{
			Type:       varnishlog.TypeRequest,
			Reason:     varnishlog.ReasonRxReq,
			Level:      1,
			VXID:       int64(rxreqVXID),
			ParentVXID: 0,
			Records: []varnishlog.Record{
				beginRecord(t, rxreqVXID, "req", "1", "rxreq"),
				reqHeaderRecord(t, rxreqVXID, "traceparent", rxreqTP),
			},
		},
		{
			Type:       varnishlog.TypeBackend,
			Reason:     varnishlog.ReasonFetch,
			Level:      2,
			VXID:       int64(bereqVXID),
			ParentVXID: int64(rxreqVXID),
			Records: []varnishlog.Record{
				beginRecord(t, bereqVXID, "bereq", fmt.Sprintf("%d", rxreqVXID), "fetch"),
				bereqHeaderRecord(t, bereqVXID, "traceparent", bereqTP),
			},
		},
	}

	traces := rcv.buildTraces(txGrp)

	spans := traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	require.Equal(t, 2, spans.Len())

	wantTraceID := hexTraceID(t, traceIDHex)
	wantRxreqSpanID := hexSpanID(t, rxreqSpanIDHex)
	wantBereqSpanID := hexSpanID(t, bereqSpanIDHex)

	rootSpan := spans.At(0)
	assert.Equal(t, wantTraceID, rootSpan.TraceID())
	assert.Equal(t, wantRxreqSpanID, rootSpan.SpanID())
	assert.Equal(t, pcommon.NewSpanIDEmpty(), rootSpan.ParentSpanID(), "root must not reference a parent span")
	_, rootHasVxidParent := rootSpan.Attributes().Get("varnish.vxid_parent")
	assert.False(t, rootHasVxidParent, "root must not carry varnish.vxid_parent")

	childSpan := spans.At(1)
	assert.Equal(t, wantTraceID, childSpan.TraceID(), "descendant must share the root trace ID")
	assert.Equal(t, wantBereqSpanID, childSpan.SpanID())
	assert.Equal(t, wantRxreqSpanID, childSpan.ParentSpanID(), "bereq must reference the rxreq as its parent span")
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
//   - emit one trace per rxreq keyed on the rxreq's traceparent
//   - keep each descendant in its rxreq ancestor's trace
//   - NOT merge two rxreqs on the same TCP session into one trace
func TestBuildTraces_SessionRootedGroup_Varnish9(t *testing.T) {
	const (
		sessVXID   uint64 = 100
		rxreqAVXID uint64 = 101
		rxreqBVXID uint64 = 103
		bereqAVXID uint64 = 102
		bereqBVXID uint64 = 104

		traceIDA     = "a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4a4"
		traceIDB     = "b4b4b4b4b4b4b4b4b4b4b4b4b4b4b4b4"
		spanIDA      = "1414141414141414"
		spanIDB      = "2424242424242424"
		bereqSpanIDA = "3434343434343434"
		bereqSpanIDB = "4444444444444444"
	)
	tpA := fmt.Sprintf("00-%s-%s-01", traceIDA, spanIDA)
	tpB := fmt.Sprintf("00-%s-%s-01", traceIDB, spanIDB)
	tpAbereq := fmt.Sprintf("00-%s-%s-01", traceIDA, bereqSpanIDA)
	tpBbereq := fmt.Sprintf("00-%s-%s-01", traceIDB, bereqSpanIDB)

	rcv := newTestReceiver(t)

	txGrp := []varnishlog.Transaction{
		{
			Type: varnishlog.TypeUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 1, VXID: int64(sessVXID), ParentVXID: 0,
			Records: []varnishlog.Record{beginRecord(t, sessVXID, "sess", "0", "HTTP/1")},
		},
		{
			Type: varnishlog.TypeUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 2, VXID: int64(rxreqAVXID), ParentVXID: int64(sessVXID),
			Records: []varnishlog.Record{
				beginRecord(t, rxreqAVXID, "req", fmt.Sprintf("%d", sessVXID), "rxreq"),
				reqHeaderRecord(t, rxreqAVXID, "traceparent", tpA),
			},
		},
		{
			Type: varnishlog.TypeUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 2, VXID: int64(rxreqBVXID), ParentVXID: int64(sessVXID),
			Records: []varnishlog.Record{
				beginRecord(t, rxreqBVXID, "req", fmt.Sprintf("%d", sessVXID), "rxreq"),
				reqHeaderRecord(t, rxreqBVXID, "traceparent", tpB),
			},
		},
		{
			Type: varnishlog.TypeUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 3, VXID: int64(bereqAVXID), ParentVXID: int64(rxreqAVXID),
			Records: []varnishlog.Record{
				beginRecord(t, bereqAVXID, "bereq", fmt.Sprintf("%d", rxreqAVXID), "fetch"),
				bereqHeaderRecord(t, bereqAVXID, "traceparent", tpAbereq),
			},
		},
		{
			Type: varnishlog.TypeUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 3, VXID: int64(bereqBVXID), ParentVXID: int64(rxreqBVXID),
			Records: []varnishlog.Record{
				beginRecord(t, bereqBVXID, "bereq", fmt.Sprintf("%d", rxreqBVXID), "fetch"),
				bereqHeaderRecord(t, bereqBVXID, "traceparent", tpBbereq),
			},
		},
	}

	traces := rcv.buildTraces(txGrp)

	spans := traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	require.Equal(t, 4, spans.Len(), "session must be dropped; 2 rxreqs + 2 bereqs = 4 spans")

	find := func(vxid uint64) (ptrace.Span, bool) {
		for i := 0; i < spans.Len(); i++ {
			s := spans.At(i)
			v, has := s.Attributes().Get("varnish.vxid")
			if has && uint64(v.Int()) == vxid {
				return s, true
			}
		}
		return ptrace.Span{}, false
	}

	_, sessEmitted := find(sessVXID)
	assert.False(t, sessEmitted, "session VXID must not appear in emitted spans")

	rxreqA, ok := find(rxreqAVXID)
	require.True(t, ok, "rxreq A must be emitted")
	wantTraceA := hexTraceID(t, traceIDA)
	assert.Equal(t, wantTraceA, rxreqA.TraceID(), "rxreq A trace ID must derive from its traceparent")
	assert.Equal(t, pcommon.NewSpanIDEmpty(), rxreqA.ParentSpanID(), "rxreq A must be a trace root")
	_, rootAHasParent := rxreqA.Attributes().Get("varnish.vxid_parent")
	assert.False(t, rootAHasParent, "rxreq root must not carry varnish.vxid_parent")

	rxreqB, ok := find(rxreqBVXID)
	require.True(t, ok, "rxreq B must be emitted")
	wantTraceB := hexTraceID(t, traceIDB)
	assert.Equal(t, wantTraceB, rxreqB.TraceID())
	assert.NotEqual(t, wantTraceA, wantTraceB, "two rxreqs on the same session must NOT be merged into one trace")
	assert.Equal(t, pcommon.NewSpanIDEmpty(), rxreqB.ParentSpanID())

	bereqA, ok := find(bereqAVXID)
	require.True(t, ok)
	assert.Equal(t, wantTraceA, bereqA.TraceID(), "bereq A must inherit rxreq A's trace ID")
	assert.Equal(t, hexSpanID(t, spanIDA), bereqA.ParentSpanID())

	bereqB, ok := find(bereqBVXID)
	require.True(t, ok)
	assert.Equal(t, wantTraceB, bereqB.TraceID(), "bereq B must inherit rxreq B's trace ID")
	assert.Equal(t, hexSpanID(t, spanIDB), bereqB.ParentSpanID())
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
		sessVXID  uint64 = 100
		rxreqVXID uint64 = 101
		bereqVXID uint64 = 102
		esiVXID   uint64 = 103
		esiBqVXID uint64 = 104

		traceIDHex     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		rxreqSpanIDHex = "1111111111111111"
		esiSpanIDHex   = "2222222222222222"
		bereqSpanIDHex = "3333333333333333"
		esiBqSpanIDHex = "4444444444444444"
	)
	rxreqTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, rxreqSpanIDHex)
	esiTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, esiSpanIDHex)
	// vcl_backend_fetch mints a fresh span-id and rewrites bereq.http.traceparent
	// so the origin backend sees the bereq's own span as its parent.
	bereqTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, bereqSpanIDHex)
	esiBqTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, esiBqSpanIDHex)

	rcv := newTestReceiver(t)

	txGrp := []varnishlog.Transaction{
		{
			Type: varnishlog.TypeUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 1, VXID: int64(sessVXID), ParentVXID: 0,
			Records: []varnishlog.Record{beginRecord(t, sessVXID, "sess", "0", "HTTP/1")},
		},
		{
			Type: varnishlog.TypeUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 2, VXID: int64(rxreqVXID), ParentVXID: int64(sessVXID),
			Records: []varnishlog.Record{
				beginRecord(t, rxreqVXID, "req", fmt.Sprintf("%d", sessVXID), "rxreq"),
				reqHeaderRecord(t, rxreqVXID, "traceparent", rxreqTP),
			},
		},
		{
			Type: varnishlog.TypeUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 3, VXID: int64(bereqVXID), ParentVXID: int64(rxreqVXID),
			Records: []varnishlog.Record{
				beginRecord(t, bereqVXID, "bereq", fmt.Sprintf("%d", rxreqVXID), "fetch"),
				bereqHeaderRecord(t, bereqVXID, "traceparent", bereqTP),
			},
		},
		{
			Type: varnishlog.TypeUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 3, VXID: int64(esiVXID), ParentVXID: int64(rxreqVXID),
			Records: []varnishlog.Record{
				beginRecord(t, esiVXID, "req", fmt.Sprintf("%d", rxreqVXID), "esi"),
				reqHeaderRecord(t, esiVXID, "traceparent", esiTP),
			},
		},
		{
			Type: varnishlog.TypeUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 4, VXID: int64(esiBqVXID), ParentVXID: int64(esiVXID),
			Records: []varnishlog.Record{
				beginRecord(t, esiBqVXID, "bereq", fmt.Sprintf("%d", esiVXID), "fetch"),
				bereqHeaderRecord(t, esiBqVXID, "traceparent", esiBqTP),
			},
		},
	}

	traces := rcv.buildTraces(txGrp)

	spans := traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	require.Equal(t, 4, spans.Len(), "session dropped; rxreq + bereq + esi + esi's bereq = 4 spans")

	find := func(vxid uint64) (ptrace.Span, bool) {
		for i := 0; i < spans.Len(); i++ {
			s := spans.At(i)
			v, has := s.Attributes().Get("varnish.vxid")
			if has && uint64(v.Int()) == vxid {
				return s, true
			}
		}
		return ptrace.Span{}, false
	}

	wantTraceID := hexTraceID(t, traceIDHex)
	wantRxreqSpanID := hexSpanID(t, rxreqSpanIDHex)
	wantEsiSpanID := hexSpanID(t, esiSpanIDHex)
	wantBereqSpanID := hexSpanID(t, bereqSpanIDHex)
	wantEsiBqSpanID := hexSpanID(t, esiBqSpanIDHex)

	rxreq, ok := find(rxreqVXID)
	require.True(t, ok)
	assert.Equal(t, wantTraceID, rxreq.TraceID(), "trace ID must come from rxreq's traceparent, not from VXID")
	assert.Equal(t, wantRxreqSpanID, rxreq.SpanID(), "rxreq span ID must come from its traceparent parent_id (VCL-generated Varnish span)")
	assert.Equal(t, pcommon.NewSpanIDEmpty(), rxreq.ParentSpanID())

	bereq, ok := find(bereqVXID)
	require.True(t, ok)
	assert.Equal(t, wantTraceID, bereq.TraceID(), "bereq must share the rxreq trace ID")
	assert.Equal(t, wantBereqSpanID, bereq.SpanID(), "bereq span ID must come from its own traceparent parent_id (vcl_backend_fetch mints a fresh span)")
	assert.Equal(t, wantRxreqSpanID, bereq.ParentSpanID(), "bereq parent must be the parent req's Varnish span, looked up via the VXID chain")

	esi, ok := find(esiVXID)
	require.True(t, ok)
	assert.Equal(t, wantTraceID, esi.TraceID(), "ESI sub must share the rxreq trace ID (VCL propagated traceparent from req_top)")
	assert.Equal(t, wantEsiSpanID, esi.SpanID(), "ESI span ID from its own traceparent parent_id")
	assert.Equal(t, wantRxreqSpanID, esi.ParentSpanID(), "ESI parent must be the parent req's Varnish span, looked up in the txGrp")

	esiBq, ok := find(esiBqVXID)
	require.True(t, ok)
	assert.Equal(t, wantTraceID, esiBq.TraceID())
	assert.Equal(t, wantEsiBqSpanID, esiBq.SpanID(), "ESI's bereq span ID must come from its own traceparent parent_id (vcl_backend_fetch mints a fresh span)")
	assert.Equal(t, wantEsiSpanID, esiBq.ParentSpanID(), "ESI's bereq parent = ESI's Varnish span, looked up via the VXID chain")
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
		rxreqVXID uint64 = 300
		esiVXID   uint64 = 301
		bereqVXID uint64 = 302

		traceIDHex     = "a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5"
		rxreqSpanIDHex = "1515151515151515"
		esiSpanIDHex   = "2525252525252525"
		bereqSpanIDHex = "3535353535353535"
	)
	rxreqTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, rxreqSpanIDHex)
	esiTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, esiSpanIDHex)
	bereqTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, bereqSpanIDHex)

	rcv := newTestReceiver(t)

	linkRec := varnishlog.Record{
		IsClient: true,
		Tag:      tagByName(t, "Link"),
		VXID:     rxreqVXID,
		Data:     fmt.Sprintf("req %d esi", esiVXID),
	}

	txGrp := []varnishlog.Transaction{
		{
			Type: varnishlog.TypeUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 1, VXID: int64(rxreqVXID), ParentVXID: 0,
			Records: []varnishlog.Record{
				beginRecord(t, rxreqVXID, "req", "0", "rxreq"),
				reqHeaderRecord(t, rxreqVXID, "traceparent", rxreqTP),
				linkRec,
			},
		},
		{
			Type: varnishlog.TypeUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 2, VXID: int64(esiVXID), ParentVXID: int64(rxreqVXID),
			Records: []varnishlog.Record{
				beginRecord(t, esiVXID, "req", fmt.Sprintf("%d", rxreqVXID), "esi"),
				reqHeaderRecord(t, esiVXID, "traceparent", esiTP),
			},
		},
		{
			Type: varnishlog.TypeUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 3, VXID: int64(bereqVXID), ParentVXID: int64(esiVXID),
			Records: []varnishlog.Record{
				beginRecord(t, bereqVXID, "bereq", fmt.Sprintf("%d", esiVXID), "fetch"),
				bereqHeaderRecord(t, bereqVXID, "traceparent", bereqTP),
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

	txGrp := []varnishlog.Transaction{
		{
			Type: varnishlog.TypeUnknown, Reason: varnishlog.ReasonUnknown,
			Level: 1, VXID: 400, ParentVXID: 0,
			Records: []varnishlog.Record{beginRecord(t, 400, "sess", "0", "HTTP/1")},
		},
	}

	traces := rcv.buildTraces(txGrp)
	assert.Equal(t, 0, traces.SpanCount(), "session-only txGrp must emit zero spans")
	assert.Equal(t, 0, traces.ResourceSpans().Len(), "must not emit an empty ResourceSpans block")
}

// TestBuildTraces_ConfiguredHeadersEmitAtConfiguredAttrKeys verifies
// that each header→attribute mapping in CaptureRequestHeaders results in
// the header's value being emitted verbatim under the configured
// attribute name. The receiver applies no derivation: user-agent goes
// wherever the user says (here, user_agent.original), Host is a plain
// request-header attribute (never host.name), and any custom header can
// map to any attribute name. Headers not in the config map must not
// emit at all, and configured headers absent from VSL must not emit an
// empty attribute.
func TestBuildTraces_ConfiguredHeadersEmitAtConfiguredAttrKeys(t *testing.T) {
	const (
		rxreqVXID uint64 = 800

		traceIDHex     = "a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6a6"
		rxreqSpanIDHex = "1616161616161616"
	)
	rxreqTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, rxreqSpanIDHex)

	rcv := newTestReceiverWithHeaders(t, map[string]string{
		"user-agent":   "user_agent.original",
		"host":         "http.request.header.host",
		"x-request-id": "http.request.header.x_request_id",
		"x-tenant":     "tenant.id",
	})

	txGrp := []varnishlog.Transaction{{
		Type:   varnishlog.TypeRequest,
		Reason: varnishlog.ReasonRxReq,
		VXID:   int64(rxreqVXID),
		Records: []varnishlog.Record{
			beginRecord(t, rxreqVXID, "req", "0", "rxreq"),
			reqHeaderRecord(t, rxreqVXID, "traceparent", rxreqTP),
			reqHeaderRecord(t, rxreqVXID, "user-agent", "curl/8.0"),
			reqHeaderRecord(t, rxreqVXID, "host", "example.com"),
			reqHeaderRecord(t, rxreqVXID, "x-request-id", "req-abc-123"),
			reqHeaderRecord(t, rxreqVXID, "X-Forwarded-For", "10.0.0.1"),
		},
	}}

	attrs := spanAttrsForVXID(t, rcv, txGrp, rxreqVXID)

	assert.Equal(t, "curl/8.0", attrs["user_agent.original"])
	assert.Equal(t, "example.com", attrs["http.request.header.host"], "host emits at its configured attribute name, verbatim")
	assert.Equal(t, "req-abc-123", attrs["http.request.header.x_request_id"])
	_, hasXFF := attrs["http.request.header.x_forwarded_for"]
	assert.False(t, hasXFF, "headers absent from capture map must not emit")
	_, hasTenant := attrs["tenant.id"]
	assert.False(t, hasTenant, "configured header not seen in VSL must not emit an empty attribute")
}

// TestBuildTraces_RespectUpstreamSampling_SampledEmits: when the root
// rxreq's traceparent has flags=01 the whole trace (rxreq + bereq) is
// emitted. Baseline for the sampling gate — no dropping when upstream
// signals sampled.
func TestBuildTraces_RespectUpstreamSampling_SampledEmits(t *testing.T) {
	const (
		rxreqVXID uint64 = 500
		bereqVXID uint64 = 501

		traceIDHex     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		rxreqSpanIDHex = "1111111111111111"
		bereqSpanIDHex = "2222222222222222"
	)
	rxreqTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, rxreqSpanIDHex)
	bereqTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, bereqSpanIDHex)

	rcv := newTestReceiver(t)
	txGrp := []varnishlog.Transaction{
		{
			Type: varnishlog.TypeRequest, Reason: varnishlog.ReasonRxReq,
			Level: 1, VXID: int64(rxreqVXID), ParentVXID: 0,
			Records: []varnishlog.Record{
				beginRecord(t, rxreqVXID, "req", "0", "rxreq"),
				reqHeaderRecord(t, rxreqVXID, "traceparent", rxreqTP),
			},
		},
		{
			Type: varnishlog.TypeBackend, Reason: varnishlog.ReasonFetch,
			Level: 2, VXID: int64(bereqVXID), ParentVXID: int64(rxreqVXID),
			Records: []varnishlog.Record{
				beginRecord(t, bereqVXID, "bereq", fmt.Sprintf("%d", rxreqVXID), "fetch"),
				bereqHeaderRecord(t, bereqVXID, "traceparent", bereqTP),
			},
		},
	}
	traces := rcv.buildTraces(txGrp)
	spans := traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	require.Equal(t, 2, spans.Len(), "sampled root must emit root + descendant spans")
}

// TestBuildTraces_RespectUpstreamSampling_MissingTraceparentDrops
// exercises the fail-closed contract: when the flag is on and the
// root carries no traceparent header at all, the trace is dropped.
func TestBuildTraces_RespectUpstreamSampling_MissingTraceparentDrops(t *testing.T) {
	const rxreqVXID uint64 = 520

	rcv := newTestReceiver(t)
	txGrp := []varnishlog.Transaction{
		{
			Type: varnishlog.TypeRequest, Reason: varnishlog.ReasonRxReq,
			Level: 1, VXID: int64(rxreqVXID), ParentVXID: 0,
			Records: []varnishlog.Record{
				beginRecord(t, rxreqVXID, "req", "0", "rxreq"),
			},
		},
	}
	traces := rcv.buildTraces(txGrp)
	assert.Equal(t, 0, traces.SpanCount(), "fail-closed: root without traceparent must drop when RespectUpstreamSampling=true")
}

// TestBuildTraces_RespectUpstreamSampling_DisabledEmitsUnsampled locks
// in the default (backward-compat) behavior: with the flag off, even
// an explicitly unsampled root (-00) is emitted. Prevents accidental
// regression that would break deployments relying on downstream
// tailsampling.
func TestBuildTraces_RespectUpstreamSampling_DisabledEmitsUnsampled(t *testing.T) {
	const (
		rxreqVXID uint64 = 700

		traceIDHex     = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
		rxreqSpanIDHex = "9999999999999999"
	)
	rxreqTP := fmt.Sprintf("00-%s-%s-00", traceIDHex, rxreqSpanIDHex)

	rcv := newTestReceiver(t)
	txGrp := []varnishlog.Transaction{
		{Type: varnishlog.TypeRequest, Reason: varnishlog.ReasonRxReq,
			Level: 1, VXID: int64(rxreqVXID), ParentVXID: 0,
			Records: []varnishlog.Record{
				beginRecord(t, rxreqVXID, "req", "0", "rxreq"),
				reqHeaderRecord(t, rxreqVXID, "traceparent", rxreqTP),
			}},
	}
	traces := rcv.buildTraces(txGrp)
	spans := traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	require.Equal(t, 1, spans.Len(), "with RespectUpstreamSampling=false, unsampled traceparent must still emit")
}

func Test_buildHeaderMapping(t *testing.T) {
	type args struct {
		mapping map[string]string
	}
	tests := []struct {
		name string
		args args
		want []headerMapping
	}{
		{
			name: "build header mapping",
			args: args{
				mapping: map[string]string{
					"user-agent": "user_agent.original",
					"host":       "http.request.header.host",
				},
			},
			want: []headerMapping{
				{HdrName: "user-agent", OtelAttrKey: "user_agent.original"},
				{HdrName: "host", OtelAttrKey: "http.request.header.host"},
			},
		},
		{
			name: "drop invalid header mapping",
			args: args{
				mapping: map[string]string{
					"":     "user_agent.original",
					"host": "http.request.header.host",
				},
			},
			want: []headerMapping{
				{HdrName: "host", OtelAttrKey: "http.request.header.host"},
			},
		},
		{
			name: "drop invalid header mapping if otel attr is empty",
			args: args{
				mapping: map[string]string{
					"host": "",
				},
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, buildHeaderMapping(tt.args.mapping), "buildHeaderMapping(%v)", tt.args.mapping)
		})
	}
}

func TestBuildTraces_UpstreamTraceparent_SetsRxreqParent(t *testing.T) {
	const (
		rxreqVXID uint64 = 200

		traceIDHex          = "cccccccccccccccccccccccccccccccc"
		clientOrigSpanIDHex = "aaaaaaaaaaaaaaaa"
		vclRewriteSpanIDHex = "bbbbbbbbbbbbbbbb"
	)
	clientTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, clientOrigSpanIDHex)
	vclTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, vclRewriteSpanIDHex)

	rcv := newTestReceiver(t)

	txGrp := []varnishlog.Transaction{{
		Type: varnishlog.TypeRequest, Reason: varnishlog.ReasonRxReq,
		VXID: int64(rxreqVXID), ParentVXID: 0,
		Records: []varnishlog.Record{
			beginRecord(t, rxreqVXID, "req", "0", "rxreq"),
			reqHeaderRecord(t, rxreqVXID, "traceparent", clientTP),
			vclCallRecord(t, rxreqVXID, "RECV"),
			reqHeaderRecord(t, rxreqVXID, "traceparent", vclTP),
			vclCallRecord(t, rxreqVXID, "HASH"),
			vclCallRecord(t, rxreqVXID, "DELIVER"),
		},
	}}

	traces := rcv.buildTraces(txGrp)
	spans := traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	require.Equal(t, 1, spans.Len())
	rxreq := spans.At(0)

	assert.Equal(t, hexTraceID(t, traceIDHex), rxreq.TraceID(),
		"trace-id must come from the traceparent (both records agree)")
	assert.Equal(t, hexSpanID(t, vclRewriteSpanIDHex), rxreq.SpanID(),
		"rxreq's own span-id comes from the VCL-rewritten traceparent (used for bereq linkage)")
	assert.Equal(t, hexSpanID(t, clientOrigSpanIDHex), rxreq.ParentSpanID(),
		"rxreq's parent-span-id must be the CLIENT-original span-id — rxreq is a child of upstream, not a new root")
}

func TestBuildTraces_VCLGeneratedTraceparent_NoRxreqParent(t *testing.T) {
	const (
		rxreqVXID uint64 = 201

		traceIDHex   = "dddddddddddddddddddddddddddddddd"
		vclSpanIDHex = "eeeeeeeeeeeeeeee"
	)
	vclTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, vclSpanIDHex)

	rcv := newTestReceiver(t)

	txGrp := []varnishlog.Transaction{{
		Type: varnishlog.TypeRequest, Reason: varnishlog.ReasonRxReq,
		VXID: int64(rxreqVXID), ParentVXID: 0,
		Records: []varnishlog.Record{
			beginRecord(t, rxreqVXID, "req", "0", "rxreq"),
			vclCallRecord(t, rxreqVXID, "RECV"),
			reqHeaderRecord(t, rxreqVXID, "traceparent", vclTP),
			vclCallRecord(t, rxreqVXID, "HASH"),
			vclCallRecord(t, rxreqVXID, "DELIVER"),
		},
	}}

	traces := rcv.buildTraces(txGrp)
	spans := traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	require.Equal(t, 1, spans.Len())
	rxreq := spans.At(0)

	assert.Equal(t, hexTraceID(t, traceIDHex), rxreq.TraceID())
	assert.Equal(t, hexSpanID(t, vclSpanIDHex), rxreq.SpanID())
	assert.Equal(t, pcommon.NewSpanIDEmpty(), rxreq.ParentSpanID(),
		"no client-sent traceparent - rxreq must remain a top-level root")
}

func TestBuildTraces_MalformedUpstreamTraceparent_NoRxreqParent(t *testing.T) {
	const (
		rxreqVXID uint64 = 202

		traceIDHex   = "ffffffffffffffffffffffffffffffff"
		vclSpanIDHex = "1212121212121212"
	)
	vclTP := fmt.Sprintf("00-%s-%s-01", traceIDHex, vclSpanIDHex)

	rcv := newTestReceiver(t)

	txGrp := []varnishlog.Transaction{{
		Type: varnishlog.TypeRequest, Reason: varnishlog.ReasonRxReq,
		VXID: int64(rxreqVXID), ParentVXID: 0,
		Records: []varnishlog.Record{
			beginRecord(t, rxreqVXID, "req", "0", "rxreq"),
			reqHeaderRecord(t, rxreqVXID, "traceparent", "garbage-not-a-tp"),
			vclCallRecord(t, rxreqVXID, "RECV"),
			reqHeaderRecord(t, rxreqVXID, "traceparent", vclTP),
			vclCallRecord(t, rxreqVXID, "DELIVER"),
		},
	}}

	traces := rcv.buildTraces(txGrp)
	spans := traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	require.Equal(t, 1, spans.Len())
	rxreq := spans.At(0)

	assert.Equal(t, hexSpanID(t, vclSpanIDHex), rxreq.SpanID())
	assert.Equal(t, pcommon.NewSpanIDEmpty(), rxreq.ParentSpanID(),
		"malformed client-sent traceparent must not surface as parent-span-id")
}
