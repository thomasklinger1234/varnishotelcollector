package varnishcachelogreceiver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	varnishlog "github.com/varnish/varnish-go/log"
	"go.opentelemetry.io/collector/pdata/ptrace"
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

func TestTransformReqHeader_ValueEdgeCases(t *testing.T) {
	cases := []struct {
		name    string
		slot    int
		payload string
		want    string
	}{
		{"empty value trimmed of NUL", 1, "x-foo:", ""},
		{"no space after colon", 2, "x-foo:value", "value"},
		{"normal name-colon-space-value", 3, "x-foo: value", "value"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			vtx := emptyTransaction()
			rec := varnishlog.Record{
				Tag:       varnishlog.TagReqHeader,
				VXID:      1,
				IsClient:  true,
				IsBackend: false,
				Data:      c.payload,
			}
			err := transformReqHeader(vtx, rec)
			require.NoError(t, err, "empty and no-space values must not error")
			assert.Equal(t, c.want, vtx.Req.Headers["x-foo"])
		})
	}
}

func TestTransformReqHeader_MalformedRejected(t *testing.T) {
	vtx := emptyTransaction()
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

func emptyTransaction() *varnishTransaction {
	return &varnishTransaction{
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
}

func transactionWithHeaders(reqHdrs, respHdrs map[string]string) *varnishTransaction {
	result := emptyTransaction()
	result.Req.Headers = reqHdrs
	result.Resp.Headers = respHdrs
	return result
}

func Test_setHeaderSpanAttrs(t *testing.T) {
	type args struct {
		span ptrace.Span
		tx   *varnishTransaction
		opts spanOpts
	}
	type result struct {
		value string
		ok    bool
	}
	type wants struct {
		otelAttrs map[string]result
	}
	tests := []struct {
		name  string
		args  args
		wants wants
	}{
		{
			name: "set otel attribute with header values",
			args: args{
				span: ptrace.NewSpan(),
				tx:   transactionWithHeaders(map[string]string{"x-foo": "bar"}, map[string]string{"x-baz": "foobar"}),
				opts: spanOpts{
					requestHdrMapping:  []headerMapping{{HdrName: "x-foo", OtelAttrKey: "http.request.header.x_foo"}},
					responseHdrMapping: []headerMapping{{HdrName: "x-baz", OtelAttrKey: "http.response.header.x_baz"}},
				},
			},
			wants: wants{
				otelAttrs: map[string]result{"http.request.header.x_foo": {value: "bar", ok: true}, "http.response.header.x_baz": {value: "foobar", ok: true}},
			},
		},
		{
			name: "set otel attribute with header values but fail on one",
			args: args{
				span: ptrace.NewSpan(),
				tx:   transactionWithHeaders(map[string]string{"x-foo": "bar"}, map[string]string{"x-baz": "foobar"}),
				opts: spanOpts{
					requestHdrMapping:  []headerMapping{{HdrName: "x-foo", OtelAttrKey: "http.request.header.x_foo"}},
					responseHdrMapping: []headerMapping{{HdrName: "x-not-existing", OtelAttrKey: "http.response.header.x_baz"}},
				},
			},
			wants: wants{
				otelAttrs: map[string]result{"http.request.header.x_foo": {value: "bar", ok: true}, "http.response.header.x_baz": {value: "", ok: false}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setHeaderSpanAttrs(tt.args.span, tt.args.tx, tt.args.opts)
			for key, want := range tt.wants.otelAttrs {
				value, ok := tt.args.span.Attributes().Get(key)
				assert.Equal(t, ok, want.ok)
				assert.Equal(t, value.Str(), want.value)
			}
		})
	}
}

func Test_setCustomSpanAttrs(t *testing.T) {
	type args struct {
		span ptrace.Span
		tx   *varnishTransaction
	}
	type wants struct {
		attrs map[string]string
	}
	tests := []struct {
		name string
		args args
		want wants
	}{
		{
			name: "sets custom attributes from OTEL_Attribute logs",
			args: args{
				span: ptrace.NewSpan(),
				tx: &varnishTransaction{Logs: []string{
					"OTEL_Attribute: app.feature=checkout",
					"OTEL_Attribute: app.tier=premium",
				}},
			},
			want: wants{attrs: map[string]string{
				"app.feature": "checkout",
				"app.tier":    "premium",
			}},
		},
		{
			name: "trims spaces and keeps text after first equals in value",
			args: args{
				span: ptrace.NewSpan(),
				tx: &varnishTransaction{Logs: []string{
					"OTEL_Attribute:   custom.key   =   some=value   ",
				}},
			},
			want: wants{attrs: map[string]string{
				"custom.key": "some=value",
			}},
		},
		{
			name: "ignores malformed and non-prefixed logs",
			args: args{
				span: ptrace.NewSpan(),
				tx: &varnishTransaction{Logs: []string{
					"OTEL_Attribute: missingequals",
					"OTEL_Attribute: =novalidkey",
					"OTEL_Attribute: novalidvalue=",
					"some other log message",
				}},
			},
			want: wants{attrs: map[string]string{}},
		},
		{
			name: "last value wins for same key",
			args: args{
				span: ptrace.NewSpan(),
				tx: &varnishTransaction{Logs: []string{
					"OTEL_Attribute: app.feature=search",
					"OTEL_Attribute: app.feature=recommendations",
				}},
			},
			want: wants{attrs: map[string]string{
				"app.feature": "recommendations",
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCustomSpanAttrs(tt.args.span, tt.args.tx)

			gotAttrs := tt.args.span.Attributes().AsRaw()
			assert.Equal(t, len(tt.want.attrs), len(gotAttrs))
			for key, wantVal := range tt.want.attrs {
				gotVal, ok := tt.args.span.Attributes().Get(key)
				assert.True(t, ok, "expected attribute %q to be set", key)
				assert.Equal(t, wantVal, gotVal.Str())
			}
		})
	}
}
