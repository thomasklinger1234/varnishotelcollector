package varnishcachelogreceiver

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

func TestFactoryCreate(t *testing.T) {
	factory := NewFactory()
	require.Equal(t, metadata.Type, factory.Type())
}

func TestCreateMetricsReceiver(t *testing.T) {
	tests := []struct {
		desc string
		run  func(t *testing.T)
	}{
		{
			desc: "Defaults with valid config",
			run: func(t *testing.T) {
				t.Parallel()

				cfg := createDefaultConfig().(*Config)

				_, err := createTracesReceiver(
					t.Context(),
					receivertest.NewNopSettings(metadata.Type),
					cfg,
					consumertest.NewNop(),
				)

				require.NoError(t, err, "failed to create metrics receiver with valid inputs")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.desc, test.run)
	}
}
