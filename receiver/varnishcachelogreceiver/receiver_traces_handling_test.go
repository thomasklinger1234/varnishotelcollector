package varnishcachelogreceiver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	varnishlog "github.com/varnish/varnish-go/log"
)

func vclCallRecord(t *testing.T, vxid uint64, phase string) varnishlog.Record {
	t.Helper()
	return varnishlog.Record{
		Tag:  varnishlog.TagVCLCall,
		VXID: vxid,
		Data: phase,
	}
}

func hitRecord(t *testing.T, vxid uint64, payload string) varnishlog.Record {
	t.Helper()
	return varnishlog.Record{
		Tag:       varnishlog.TagHit,
		VXID:      vxid,
		IsClient:  true,
		IsBackend: false,
		Data:      payload,
	}
}

func spanAttrsForVXID(t *testing.T, rcv *varnishcachelogTraceReceiver, txGrp []varnishlog.Transaction, vxid uint64) map[string]any {
	t.Helper()
	traces := rcv.buildTraces(txGrp)
	require.Equal(t, 1, traces.ResourceSpans().Len())
	spans := traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans()
	for i := 0; i < spans.Len(); i++ {
		s := spans.At(i)
		v, ok := s.Attributes().Get("varnish.vxid")
		if ok && uint64(v.Int()) == vxid {
			return s.Attributes().AsRaw()
		}
	}
	t.Fatalf("no span emitted for vxid %d", vxid)
	return nil
}

// TestTransformVCLCall_DeliverDoesNotStompHit is the regression test for
// the "cache hits show handling=deliver in Tempo" bug: VSL emits
// `VCL_call HIT` followed by `VCL_call DELIVER`, and the old default
// branch (`tx.Handling = strings.ToLower(h)`) turned every cache hit into
// `handling=deliver`.
func TestTransformVCLCall_DeliverDoesNotStompHit(t *testing.T) {
	const rxreqVXID uint64 = 9

	rcv := newTestReceiver(t)
	txGrp := []varnishlog.Transaction{{
		VXID:       int64(rxreqVXID),
		ParentVXID: 0,
		Type:       varnishlog.TypeRequest,
		Reason:     varnishlog.ReasonRxReq,
		Records: []varnishlog.Record{
			beginRecord(t, rxreqVXID, "req", "0", "rxreq"),
			reqHeaderRecord(t, rxreqVXID, "traceparent", "00-09090909090909090909090909090909-0909090909090909-01"),
			vclCallRecord(t, (rxreqVXID), "RECV"),
			vclCallRecord(t, rxreqVXID, "HASH"),
			hitRecord(t, rxreqVXID, "32773 9.196981 1.000000 0.000000"),
			vclCallRecord(t, rxreqVXID, "HIT"),
			vclCallRecord(t, rxreqVXID, "DELIVER"),
		},
	}}

	attrs := spanAttrsForVXID(t, rcv, txGrp, rxreqVXID)

	assert.Equal(t, "hit", attrs["varnish.handling"])
	assert.Equal(t, true, attrs["varnish.cache.hit"])
	assert.Equal(t, false, attrs["varnish.cache.grace_hit"], "fresh hit: ttl>0 => grace_hit=false")
	assert.Equal(t, int64(9196), attrs["varnish.cache.ttl_ms"])
	assert.Equal(t, int64(1000), attrs["varnish.cache.grace_ms"])
	assert.Equal(t, int64(0), attrs["varnish.cache.keep_ms"])
}

func TestTransformHit_GraceHitFlaggedWhenTTLNonPositive(t *testing.T) {
	const rxreqVXID uint64 = 10

	rcv := newTestReceiver(t)
	txGrp := []varnishlog.Transaction{{
		VXID:       int64(rxreqVXID),
		ParentVXID: 0,
		Type:       varnishlog.TypeRequest,
		Reason:     varnishlog.ReasonRxReq,
		Records: []varnishlog.Record{
			beginRecord(t, rxreqVXID, "req", "0", "rxreq"),
			reqHeaderRecord(t, rxreqVXID, "traceparent", "00-0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a-0a0a0a0a0a0a0a0a-01"),
			hitRecord(t, rxreqVXID, "32773 -0.500000 1.000000 0.000000"),
			vclCallRecord(t, rxreqVXID, "HIT"),
			vclCallRecord(t, rxreqVXID, "DELIVER"),
		},
	}}

	attrs := spanAttrsForVXID(t, rcv, txGrp, rxreqVXID)

	assert.Equal(t, "hit", attrs["varnish.handling"])
	assert.Equal(t, true, attrs["varnish.cache.hit"])
	assert.Equal(t, true, attrs["varnish.cache.grace_hit"], "stale-but-graced: ttl<=0 && grace>0 => grace_hit=true")
	assert.Equal(t, int64(-500), attrs["varnish.cache.ttl_ms"])
	assert.Equal(t, int64(1000), attrs["varnish.cache.grace_ms"])
}

func TestTransformHit_StreamingHitSetsHandlingAndHitFlag(t *testing.T) {
	const rxreqVXID uint64 = 11

	rcv := newTestReceiver(t)
	txGrp := []varnishlog.Transaction{{
		VXID:       int64(rxreqVXID),
		ParentVXID: 0,
		Type:       varnishlog.TypeRequest,
		Reason:     varnishlog.ReasonRxReq,
		Records: []varnishlog.Record{
			beginRecord(t, rxreqVXID, "req", "0", "rxreq"),
			reqHeaderRecord(t, rxreqVXID, "traceparent", "00-0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b-0b0b0b0b0b0b0b0b-01"),
			hitRecord(t, rxreqVXID, "32773 5.000000 1.000000 0.000000 12345 67890"),
			vclCallRecord(t, rxreqVXID, "HIT"),
			vclCallRecord(t, rxreqVXID, "DELIVER"),
		},
	}}

	attrs := spanAttrsForVXID(t, rcv, txGrp, rxreqVXID)

	assert.Equal(t, "streaming-hit", attrs["varnish.handling"])
	assert.Equal(t, true, attrs["varnish.cache.hit"])
	assert.Equal(t, false, attrs["varnish.cache.grace_hit"])
}

func TestTransformVCLCall_MissDoesNotSetCacheHit(t *testing.T) {
	const rxreqVXID uint64 = 12

	rcv := newTestReceiver(t)
	txGrp := []varnishlog.Transaction{{
		VXID:       int64(rxreqVXID),
		ParentVXID: 0,
		Type:       varnishlog.TypeRequest,
		Reason:     varnishlog.ReasonRxReq,
		Records: []varnishlog.Record{
			beginRecord(t, rxreqVXID, "req", "0", "rxreq"),
			reqHeaderRecord(t, rxreqVXID, "traceparent", "00-0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c-0c0c0c0c0c0c0c0c-01"),
			vclCallRecord(t, rxreqVXID, "RECV"),
			vclCallRecord(t, rxreqVXID, "HASH"),
			vclCallRecord(t, rxreqVXID, "MISS"),
			vclCallRecord(t, rxreqVXID, "DELIVER"),
		},
	}}

	attrs := spanAttrsForVXID(t, rcv, txGrp, rxreqVXID)

	assert.Equal(t, "miss", attrs["varnish.handling"])
	_, hasCacheHit := attrs["varnish.cache.hit"]
	assert.False(t, hasCacheHit, "miss must not emit varnish.cache.hit")
	_, hasGrace := attrs["varnish.cache.grace_hit"]
	assert.False(t, hasGrace, "miss must not emit varnish.cache.grace_hit")
}

// TestTransformVCLCall_LifecyclePhasesNeverSetHandling guards the removal
// of the `default: tx.Handling = strings.ToLower(h)` branch: RECV, HASH,
// DELIVER and BACKEND_FETCH are lifecycle phases, not cache decisions, and
// must leave Handling empty when they are the only VCL_call records.
func TestTransformVCLCall_LifecyclePhasesNeverSetHandling(t *testing.T) {
	const rxreqVXID uint64 = 13

	rcv := newTestReceiver(t)
	txGrp := []varnishlog.Transaction{{
		VXID:       int64(rxreqVXID),
		ParentVXID: 0,
		Type:       varnishlog.TypeRequest,
		Reason:     varnishlog.ReasonRxReq,
		Records: []varnishlog.Record{
			beginRecord(t, rxreqVXID, "req", "0", "rxreq"),
			reqHeaderRecord(t, rxreqVXID, "traceparent", "00-0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d-0d0d0d0d0d0d0d0d-01"),
			vclCallRecord(t, rxreqVXID, "RECV"),
			vclCallRecord(t, rxreqVXID, "HASH"),
			vclCallRecord(t, rxreqVXID, "DELIVER"),
		},
	}}

	attrs := spanAttrsForVXID(t, rcv, txGrp, rxreqVXID)

	_, hasHandling := attrs["varnish.handling"]
	assert.False(t, hasHandling, "lifecycle-only VCL_call phases must not set varnish.handling")
}

func TestExtractTraceContext_CachesSuccessfulParse(t *testing.T) {
	const tp = "00-11111111111111111111111111111111-2222222222222222-01"
	vtx := &varnishTransaction{
		capturedHeaders:      []capturedHeader{{Name: requiredCapturedHeader}},
		capturedHeaderValues: []string{tp},
	}

	tid1, sid1, flags1, ok1 := extractTraceContext(vtx)
	require.True(t, ok1)

	vtx.capturedHeaderValues[0] = "not-a-valid-traceparent-at-all"

	tid2, sid2, flags2, ok2 := extractTraceContext(vtx)
	assert.True(t, ok2, "cache must survive after-the-fact source mutation")
	assert.Equal(t, tid1, tid2, "TraceID must come from cache, not re-parse")
	assert.Equal(t, sid1, sid2, "SpanID must come from cache, not re-parse")
	assert.Equal(t, flags1, flags2, "flags must come from cache, not re-parse")
}

func TestExtractTraceContext_CachesFailedParse(t *testing.T) {
	vtx := &varnishTransaction{
		capturedHeaders:      []capturedHeader{{Name: requiredCapturedHeader}},
		capturedHeaderValues: []string{"malformed"},
	}

	_, _, _, ok1 := extractTraceContext(vtx)
	require.False(t, ok1)

	vtx.capturedHeaderValues[0] = "00-11111111111111111111111111111111-2222222222222222-01"

	_, _, _, ok2 := extractTraceContext(vtx)
	assert.False(t, ok2, "failed-parse cache must not be revived by later valid header")
}

func TestExtractTraceContext_CachesMissingHeader(t *testing.T) {
	vtx := &varnishTransaction{
		capturedHeaders:      []capturedHeader{{Name: requiredCapturedHeader}},
		capturedHeaderValues: []string{""},
	}

	_, _, _, ok1 := extractTraceContext(vtx)
	require.False(t, ok1)

	vtx.capturedHeaderValues[0] = "00-11111111111111111111111111111111-2222222222222222-01"

	_, _, _, ok2 := extractTraceContext(vtx)
	assert.False(t, ok2, "missing-header cache must not be revived by later populated header")
}

// Regression: the fork's payload() trims trailing NUL, so a Varnish 9
// payload that used to arrive as `Name:\x00` (empty value + C terminator)
// now arrives as `Name:` (length 5, colon at end). Older guard rejected
// this as "invalid tag". Empty header values are legal (RFC 9110 §5.5)
// and were previously accepted only because the trailing NUL made the
// length-check pass. Also covers `Name:value` (no space after colon).
func TestTransformReqHeader_ValueEdgeCases(t *testing.T) {
	captured := []capturedHeader{
		{Name: requiredCapturedHeader},
		{Name: "x-empty", AttrKey: "http.request.header.x_empty"},
		{Name: "x-nospace", AttrKey: "http.request.header.x_nospace"},
		{Name: "x-normal", AttrKey: "http.request.header.x_normal"},
	}

	cases := []struct {
		name    string
		slot    int
		payload string
		want    string
	}{
		{"empty value trimmed of NUL", 1, "x-empty:", ""},
		{"no space after colon", 2, "x-nospace:value", "value"},
		{"normal name-colon-space-value", 3, "x-normal: value", "value"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			vtx := &varnishTransaction{
				capturedHeaders:      captured,
				capturedHeaderValues: make([]string, len(captured)),
			}
			rec := varnishlog.Record{
				Tag:       varnishlog.TagReqHeader,
				VXID:      1,
				IsClient:  true,
				IsBackend: false,
				Data:      c.payload,
			}
			err := transformReqHeader(vtx, rec)
			require.NoError(t, err, "empty and no-space values must not error")
			assert.Equal(t, c.want, vtx.capturedHeaderValues[c.slot])
		})
	}
}

func TestTransformReqHeader_MalformedRejected(t *testing.T) {
	vtx := &varnishTransaction{
		capturedHeaders:      []capturedHeader{{Name: requiredCapturedHeader}},
		capturedHeaderValues: make([]string, 1),
	}
	for _, payload := range []string{"", "no-colon-at-all", ":no-name"} {
		rec := varnishlog.Record{
			Tag:       varnishlog.TagReqHeader,
			VXID:      1,
			IsClient:  true,
			IsBackend: false,
			Data:      payload,
		}
		err := transformReqHeader(vtx, rec)
		assert.Error(t, err, "payload %q must error: no colon or empty name", payload)
	}
}

func BenchmarkExtractTraceparent(b *testing.B) {
	const tp = "00-11111111111111111111111111111111-2222222222222222-01"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := extractTraceparent(tp); err != nil {
			b.Fatal(err)
		}
	}
}
