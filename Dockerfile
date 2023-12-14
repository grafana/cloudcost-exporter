# Build Go Binary
FROM golang:1.21.5-alpine AS build
ARG GO_LDFLAGS

WORKDIR /app
COPY ["go.mod", "go.sum", "./"]
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags "${GO_LDFLAGS} -extldflags '-static'" -o cloudcost-exporter ./cmd/exporter

# Build Image
FROM scratch
COPY --from=alpine:latest /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /etc/passwd /etc/passwd

WORKDIR /root

COPY --from=build /app/cloudcost-exporter ./
ENTRYPOINT ["./cloudcost-exporter"]
