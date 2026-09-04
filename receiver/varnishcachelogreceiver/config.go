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
	VSLBinaryFile    string        `mapstructure:"vsl_binary_file"`

	// CaptureRequestHeaders maps request header names (case-insensitive)
	// to the OTel span attribute name the value should be emitted under.
	//
	// Default: {"user-agent": "user_agent.original",
	//           "host":       "http.request.header.host"}
	CaptureRequestHeaders map[string]string `mapstructure:"capture_request_headers"`

	// CaptureResponseHeaders maps response header names (case-insensitive)
	// to the OTel span attribute name the value should be emitted under.
	//
	// Default: {}
	CaptureResponseHeaders map[string]string `mapstructure:"capture_response_headers"`

	// RespectUpstreamSampling turns the receiver into a head-sampler
	// that honors the W3C traceparent `sampled` flag (bit 0 of the
	// flags byte) on each trace's root client request.
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
