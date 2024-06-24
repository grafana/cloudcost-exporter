package azure

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem = "azure"
)

var (
// TODO - add prometheus metrics here
)

type Azure struct {
	Context context.Context
	Logger  *slog.Logger

	CollectorTimeout time.Duration
}

type Config struct {
	Logger *slog.Logger

	CollectorTimeout time.Duration
}

// New is a TODO
func New(ctx context.Context, config *Config) (*Azure, error) {
	providerGroup := config.Logger.WithGroup(subsystem)

	return &Azure{
		Context: ctx,
		Logger:  providerGroup,

		CollectorTimeout: config.CollectorTimeout,
	}, nil
}

// RegisterCollectors is a TODO
func (a *Azure) RegisterCollectors(registry provider.Registry) error {
	return nil
}

// Describe is a TODO
func (a *Azure) Describe(ch chan<- *prometheus.Desc) {
}

// Collect is a TODO
func (a *Azure) Collect(ch chan<- prometheus.Metric) {
	// TODO - implement collector context
	_, cancel := context.WithTimeout(a.Context, a.CollectorTimeout)
	defer cancel()
}
