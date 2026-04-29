# Build Go Binary
FROM golang:1.26.2@sha256:1e598ea5752ae26c093b746fd73c5095af97d6f2d679c43e83e0eac484a33dc3 AS build

WORKDIR /app
COPY ["go.mod", "go.sum", "./"]
RUN go mod download

COPY . .

ENV GOCACHE=/go/pkg/mod/
RUN  --mount=type=cache,target="/go/pkg/mod/" make build-binary

# Build Image
FROM scratch
COPY --from=alpine:latest@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /etc/passwd /etc/passwd

WORKDIR /root

COPY --from=build /app/cloudcost-exporter ./
ENTRYPOINT ["./cloudcost-exporter"]
