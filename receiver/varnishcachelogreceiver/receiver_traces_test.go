package varnishcachelogreceiver

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thomasklinger1234/varnishotelcollector/receiver/varnishcachelogreceiver/internal/metadata"
	varnishlog "github.com/varnish/varnish-go/log"
	"go.opentelemetry.io/collector/pdata/pcommon"
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
