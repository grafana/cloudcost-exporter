package config

import (
	"log/slog"
	"time"
)

type Config struct {
	Provider  string
	ProjectID string
	Providers struct {
		AWS struct {
			Profile  string
			Region   string
			Services StringSliceFlag
			RoleARN  string
		}
		GCP struct {
			DefaultGCSDiscount int
			Projects           StringSliceFlag
			Region             string
			Services           StringSliceFlag
		}
		Azure struct {
			Services       StringSliceFlag
			SubscriptionId string
		}
	}
	Collector struct {
		ScrapeInterval time.Duration
		Timeout        time.Duration
	}

	Server struct {
		Address string
		Path    string
		Timeout time.Duration
	}
	LoggerOpts struct {
		Level  string // Maps to slog levels: debug, info, warn, error
		Output string // io.Writer interface to write out to: stdout, stderr, file
		Type   string // How to write out the logs: json, text
	}

	Logger *slog.Logger
}
