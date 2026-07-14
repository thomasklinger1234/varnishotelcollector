package varnishcachelogreceiver

import (
	"context"

	"github.com/thomasklinger1234/varnishotelcollector/receiver/varnishcachelogreceiver/internal/metadata"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
)

func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		metadata.Type,
		createDefaultConfig,
		receiver.WithTraces(createTracesReceiver, metadata.TracesStability),
	)
}

func createDefaultConfig() component.Config {
	cfg := &Config{
		Timeout: defaultTimeout,
	}

	return cfg
}

func createTracesReceiver(
	_ context.Context,
	settings receiver.Settings,
	rConf component.Config,
	nextConsumer consumer.Traces,
) (receiver.Traces, error) {
	cfg := rConf.(*Config)
	rcv := newVarnishcacheLogReceiver(settings, cfg, nextConsumer)

	return rcv, nil
}
