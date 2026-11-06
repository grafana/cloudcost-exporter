# Build Go Binary
FROM golang:1.27.3@sha256:e519d3b0a5b285e5aa53ab9eef02bc6bbb9cb04608389b743a136a9ce75f0d1e AS build

WORKDIR /app
COPY ["go.mod", "go.sum", "./"]
RUN go mod download

COPY . .

ENV GOCACHE=/go/pkg/mod/
RUN  --mount=type=cache,target="/go/pkg/mod/" make build-binary

# Refresh the trust store. The scratch image copies this file.
FROM alpine:3.24@sha256:3f4d6b32e26703c6082839234f43769d5046b3ac7c910033911d1e71d8cd630a AS certs
RUN apk add --no-cache 'ca-certificates>=20261020-r0' \
    && update-ca-certificates

# Build Image
FROM scratch
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /etc/passwd /etc/passwd

WORKDIR /root

COPY --from=build /app/cloudcost-exporter ./
ENTRYPOINT ["./cloudcost-exporter"]
