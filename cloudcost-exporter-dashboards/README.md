# Grafana Dashboards

> [!WARNING]
> This is still highly experimental as engineers at Grafana Labs are learning how to generate dashboards as code.
> The main goal is for us to be able to generate and use the same internal dashboards as we recommend OSS users to use.

This container a set of Grafana Dashboards that are generated using the [Grafana Foundation SDK](https://github.com/grafana/grafana-foundation-sdk).

## Getting Started

> [!INFO]
> If you want to develop these dashboards and view them against a live Grafana instance,
> install and configure [grizzly](https://grafana.github.io/grizzly/installation/)

To generate the dashboards:

```shell
make build-dashboards
```

To iteratively develop dashboards with live reload:

```shell
make grizzly-serve
```
