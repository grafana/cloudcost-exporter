# Logging

`cloudcost-exporter` codebase has grown to the point where we need to have some form of structured logging. 
The initial commit was introduced `db82ae9ccdfddc010492f4739724c8e67ef40851` with a fairly detailed messaged on the requirements and the approach.
The very short is we needed:

1. Structured logging with consistent labels across providers and collectors
2. Ability to define a log level and runtime that limits the logs emitted

With `slog` being part of Go's stdlib since 1.21, we decided to use it as the logging library with a wrapper so that we can get log levels as well.

## Guidelines

1. Every provider _must_ accept a `*slog.Logger` in the constructor
1. Every collector _must_ accept a `*slog.Logger` in the constructor
1. Each provider and collector _must_ add a `provider` or `collector` group when initializing using the [slog.Logger.With](https://pkg.go.dev/golang.org/x/exp/slog#Logger.With) method, specifying the collector or provider used
1. Always prefer to use the `logger.WithAttr(...)` method to add structured data to the log message for both performance and consistency(see [slog blog post](https://go.dev/blog/slog) and search Performance section for more information)
   - NB: If you do not have any additional fields to log out, then you can use the `logger.Info("message")` methods

## Expanding the logging

If you need more flexibility or need to expand the logger, please file an [issue](https://github.com/grafana/cloudcost-exporter/issues/new) with the requirements, and we can discuss the best way to implement it.

