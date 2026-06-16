# Build Go Binary
FROM golang:1.26.3@sha256:2d6c80227255c3112a4d08e67ba98e58efd3846daf15d9d7d4c389565d881b1a AS build

WORKDIR /app
COPY ["go.mod", "go.sum", "./"]
RUN go mod download

COPY . .

ENV GOCACHE=/go/pkg/mod/
RUN  --mount=type=cache,target="/go/pkg/mod/" make build-binary

# Build Image
FROM scratch
COPY --from=alpine:latest@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /etc/passwd /etc/passwd

WORKDIR /root

COPY --from=build /app/cloudcost-exporter ./
ENTRYPOINT ["./cloudcost-exporter"]
