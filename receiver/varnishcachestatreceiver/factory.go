package varnishcachestatreceiver

import (
	"context"

	"github.com/thomasklinger1234/varnishotelcollector/receiver/varnishcachestatreceiver/internal/metadata"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/scraper"
	"go.opentelemetry.io/collector/scraper/scraperhelper"
)

func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		metadata.Type,
		createDefaultConfig,
		receiver.WithMetrics(createMetricsReceiver, metadata.MetricsStability),
	)
}

func createDefaultConfig() component.Config {
	ccfg := scraperhelper.NewDefaultControllerConfig()
	ccfg.Timeout = defaultTimeout
	ccfg.CollectionInterval = defaultCollectionInterval

	cfg := &Config{
		ControllerConfig: ccfg,
	}

	return cfg
}

func createMetricsReceiver(
	_ context.Context,
	settings receiver.Settings,
	rConf component.Config,
	nextConsumer consumer.Metrics,
) (receiver.Metrics, error) {
	cfg := rConf.(*Config)
	scrRcv, err := newVarnishcachestatScraper(settings, cfg)
	if err != nil {
		return nil, err
	}
	scr, err := scraper.NewMetrics(scrRcv.scrape)
	if err != nil {
		return nil, err
	}

	return scraperhelper.NewMetricsController(
		&cfg.ControllerConfig,
		settings,
		nextConsumer,
		scraperhelper.AddMetricsScraper(metadata.Type, scr),
	)
}
