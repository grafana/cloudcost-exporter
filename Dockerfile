# Build Go Binary
FROM golang:1.26.3@sha256:313faae491b410a35402c05d35e7518ae99103d957308e940e1ae2cfa0aac29b AS build

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
