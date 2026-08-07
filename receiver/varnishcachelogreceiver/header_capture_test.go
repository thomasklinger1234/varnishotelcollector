package varnishcachelogreceiver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildCapturedHeaders_TraceparentAlwaysAtSlotZero(t *testing.T) {
	got := buildCapturedHeaders(nil)
	assert.Equal(t, []capturedHeader{{Name: "traceparent"}}, got)

	got = buildCapturedHeaders(map[string]string{})
	assert.Equal(t, []capturedHeader{{Name: "traceparent"}}, got)

	got = buildCapturedHeaders(map[string]string{"x-request-id": "http.request.header.x_request_id"})
	assert.Equal(t, []capturedHeader{
		{Name: "traceparent"},
		{Name: "x-request-id", AttrKey: "http.request.header.x_request_id"},
	}, got)
}

func TestBuildCapturedHeaders_NormalizesKeysAndSortsSlots(t *testing.T) {
	got := buildCapturedHeaders(map[string]string{
		"User-Agent":   "user_agent.original",
		"  HOST  ":     "server.address",
		"x-request-id": "http.request.header.x_request_id",
	})

	assert.Equal(t, []capturedHeader{
		{Name: "traceparent"},
		{Name: "host", AttrKey: "server.address"},
		{Name: "user-agent", AttrKey: "user_agent.original"},
		{Name: "x-request-id", AttrKey: "http.request.header.x_request_id"},
	}, got, "user entries sorted alphabetically by lowercase header name")
}

func TestBuildCapturedHeaders_UserSuppliedTraceparentIgnored(t *testing.T) {
	got := buildCapturedHeaders(map[string]string{
		"traceparent": "some.custom.attr",
		"host":        "server.address",
	})

	assert.Equal(t, []capturedHeader{
		{Name: "traceparent"},
		{Name: "host", AttrKey: "server.address"},
	}, got, "user-supplied traceparent entry silently dropped; internal capture keeps AttrKey empty")
}

func TestBuildCapturedHeaders_EmptyValuesDropped(t *testing.T) {
	got := buildCapturedHeaders(map[string]string{
		"host":         "server.address",
		"x-request-id": "",
		"":             "some.attr",
	})

	assert.Equal(t, []capturedHeader{
		{Name: "traceparent"},
		{Name: "host", AttrKey: "server.address"},
	}, got, "empty attr name or empty header name → entry dropped")
}
