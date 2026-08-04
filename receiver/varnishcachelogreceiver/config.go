package varnishcachelogreceiver

import (
	"errors"
	"time"
)

const (
	defaultTimeout = 5 * time.Second
)

type Config struct {
	Timeout          time.Duration `mapstructure:"timeout"`
	WorkingDirectory string        `mapstructure:"working_directory"`
	VSLQuery         string        `mapstructure:"vsl_query"`

	// CaptureRequestHeaders maps request header names (case-insensitive)
	// to the OTel span attribute name the value should be emitted under.
	// `traceparent` is always captured internally to support W3C trace
	// context propagation and cannot be listed here — an entry for it is
	// silently ignored.
	//
	// The attribute name is taken verbatim from the config value; no
	// sanitization or transformation is applied. Any OTel attribute name
	// is valid (e.g. user_agent.original, server.address,
	// http.request.header.x_request_id).
	//
	// Default: {"user-agent": "user_agent.original",
	//           "host":       "http.request.header.host"}
	CaptureRequestHeaders map[string]string `mapstructure:"capture_request_headers"`

	// RespectUpstreamSampling turns the receiver into a head-sampler
	// that honors the W3C traceparent `sampled` flag (bit 0 of the
	// flags byte) on each trace's root client request.
	//
	// When true, a whole trace (root rxreq + all its bereqs and ESI
	// sub-requests) is dropped unless the root rxreq carries a valid
	// traceparent with `sampled=1`. Missing or malformed traceparent
	// on the root counts as unsampled (fail-closed) — only enable
	// this when the upstream (VCL, front proxy, or client SDK) is
	// trusted to inject traceparent on every traceable request. See
	// dev/varnish/otel.vcl for the reference propagation setup.
	//
	// When false, every trace-root is emitted; downstream
	// tailsamplingprocessor / probabilisticsamplerprocessor stays in
	// charge of any sampling decision.
	//
	// Default: true
	RespectUpstreamSampling bool `mapstructure:"respect_upstream_sampling"`

	// prevent unkeyed literal initialization
	_ struct{}
}

func (c *Config) Validate() error {
	var err []error

	return errors.Join(err...)
}
