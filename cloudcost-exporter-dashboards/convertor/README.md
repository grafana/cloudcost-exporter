# Convertor

This is a small script that's intended to be used to convert a json blob dashboard to Go code using Grafana's Foundation SDK to manage dashboards.

## Usage

First get a dashboard represented as a [json blob](https://grafana.com/docs/grafana/latest/dashboards/share-dashboards-panels/#export-a-dashboard-as-json) and output the contents into `dashboard.json`

Then execute the script with the following command:
```bash
go run convert_dashboard.go
```
Copy the output and paste it into a new file in the `config/dashboards` directory. 

