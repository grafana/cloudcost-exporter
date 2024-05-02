# Google

This package contains the Google Cloud Platform (GCP) exporter and reporter.
`gcp.go` is responsible for setting up the GCP session and starting the collection process.
The module is built upon the [google-cloud-go](https://github.com/googleapis/google-cloud-go) library and uses the GCP Billing API to collect cost data.
Pricing data is fetched from the [GCP Pricing API](Pricing data is fetched from the [GCP Pricing API](https://cloud.google.com/billing/docs/how-to/understanding-costs#pricing).
