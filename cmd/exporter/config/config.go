package config

import (
	"time"
)

type Config struct {
	Provider  string
	ProjectID string
	Providers struct {
		AWS struct {
			Profiles StringSliceFlag
			Region   string
			Services StringSliceFlag
		}
		GCP struct {
			DefaultGCSDiscount int
			Projects           StringSliceFlag
			Region             string
			Services           StringSliceFlag
		}
	}
	Collector struct {
		ScrapeInterval time.Duration
	}

	Server struct {
		Address string
		Path    string
	}
}
