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

	// prevent unkeyed literal initialization
	_ struct{}
}

func (c *Config) Validate() error {
	var err []error

	return errors.Join(err...)
}
