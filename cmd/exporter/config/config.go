package config

import (
	"time"
)

type Config struct {
	// TODO: Refactor this to become provider-agnostic
	Provider  string
	ProjectID string
	Providers struct {
		AWS struct {
			Profiles StringSliceFlag
			Region   string
			Services StringSliceFlag
		}
		GCP struct {
			DefaultDiscount int
			Projects        StringSliceFlag
			Region          string
			Services        StringSliceFlag
		}
	}
	Collector struct {
		ScrapeInterval time.Duration
	}
}
