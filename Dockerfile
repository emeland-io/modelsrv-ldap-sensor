FROM golang:1.26.11 AS builder
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o modelsrv-ldap-sensor ./cmd/modelsrv-ldap-sensor

# Static binary on distroless: includes CA certificates (for ldaps://) and runs
# as the numeric non-root user 65532.
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/modelsrv-ldap-sensor .
USER 65532:65532

ENTRYPOINT ["/modelsrv-ldap-sensor"]
