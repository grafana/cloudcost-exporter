# Build Go Binary
FROM golang:1.25.5@sha256:20b91eda7a9627c127c0225b0d4e8ec927b476fa4130c6760928b849d769c149 AS build

WORKDIR /app
COPY ["go.mod", "go.sum", "./"]
RUN go mod download

COPY . .

ENV GOCACHE=/go/pkg/mod/
RUN  --mount=type=cache,target="/go/pkg/mod/" make build-binary

# Build Image
FROM scratch
COPY --from=alpine:latest@sha256:4b7ce07002c69e8f3d704a9c5d6fd3053be500b7f1c69fc0d80990c2ad8dd412 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /etc/passwd /etc/passwd

WORKDIR /root

COPY --from=build /app/cloudcost-exporter ./
ENTRYPOINT ["./cloudcost-exporter"]
