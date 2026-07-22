package config

import (
	"log/slog"
	"time"
)

type Config struct {
	Provider     string
	ProjectID    string
	ListServices bool
	Providers    struct {
		AWS struct {
			Profile              string
			Region               string
			Services             StringSliceFlag
			ExperimentalServices StringSliceFlag
			RoleARN              string
			ExcludeRegions       StringSliceFlag
			BedrockFamilyFilter  string
			RDSRegionListTimeout time.Duration
			CapacityBlocks       bool
		}
		GCP struct {
			DefaultGCSDiscount       int
			Projects                 StringSliceFlag
			BucketProjectsDeprecated bool
			Region                   string
			Services                 StringSliceFlag
			ExperimentalServices     StringSliceFlag
			GKEZoneConcurrency       int
			VertexFamilyFilter       string
		}
		Azure struct {
			Services             StringSliceFlag
			ExperimentalServices StringSliceFlag
			SubscriptionID       string
			Region               string
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
