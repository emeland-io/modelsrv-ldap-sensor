FROM golang:1.26.11 AS builder
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o modelsrv-ldap-sensor ./cmd/modelsrv-ldap-sensor

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /
COPY --from=builder /workspace/modelsrv-ldap-sensor .
USER nobody

ENTRYPOINT ["/modelsrv-ldap-sensor"]
