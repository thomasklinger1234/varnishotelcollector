package varnishcachestatreceiver

import (
	"errors"
	"time"

	"go.opentelemetry.io/collector/scraper/scraperhelper"
)

const (
	defaultTimeout            = 5 * time.Second
	defaultCollectionInterval = 10 * time.Second
)

type Config struct {
	scraperhelper.ControllerConfig `mapstructure:",squash"`

	WorkingDirectory string   `mapstructure:"working_directory"`
	IncludeTags      []string `mapstructure:"include_tags"`
	ExcludeTags      []string `mapstructure:"exclude_tags"`

	// prevent unkeyed literal initialization
	_ struct{}
}

func (c *Config) Validate() error {
	var err []error

	return errors.Join(err...)
}
