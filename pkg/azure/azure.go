package azure

import (
	"context"
	"log/slog"

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
	Logger *slog.Logger
}

type Config struct {
	Logger *slog.Logger
}

// New is a TODO
func New(config *Config) (*Azure, error) {
	providerGroup := config.Logger.With("provider", "azure")

	return &Azure{
		Logger: providerGroup,
	}, nil
}

// RegisterCollectors is a TODO
func (a *Azure) RegisterCollectors(registry provider.Registry) error {
	a.Logger.LogAttrs(context.TODO(), slog.LevelInfo, "Register")
	return nil
}

// Describe is a TODO
func (a *Azure) Describe(ch chan<- *prometheus.Desc) {
	a.Logger.LogAttrs(context.TODO(), slog.LevelInfo, "Describe")
}

// Collect is a TODO
func (a *Azure) Collect(ch chan<- prometheus.Metric) {
	a.Logger.LogAttrs(context.TODO(), slog.LevelInfo, "Collect")
}
