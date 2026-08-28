# --platform=$BUILDPLATFORM keeps the compiler on the build host under
# buildx and cross-compiles via GOOS/GOARCH, so a multi-arch build does
# not run Go under emulation. TARGETOS/TARGETARCH are empty on a plain
# single-arch build, and empty GOOS/GOARCH means the toolchain default.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# VERSION can be passed explicitly; otherwise it is derived from git so
# `gocov-server version` reports the checked-out tag instead of "dev".
ARG VERSION=
ARG TARGETOS
ARG TARGETARCH
RUN ver="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}" && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${ver}" \
    -o /out/gocov-server ./cmd/gocov-server

# Distroless: no shell, no package manager, just the static binary plus CA
# certificates and /etc/passwd. The :nonroot tag runs as uid 65532.
FROM gcr.io/distroless/static-debian13:nonroot
# GHCR links the pushed package to this repo because of this label.
LABEL org.opencontainers.image.source="https://github.com/gocov/gocov" \
      org.opencontainers.image.description="gocov server — self-hostable coverage tracking" \
      org.opencontainers.image.licenses="AGPL-3.0"
COPY --from=build /out/gocov-server /usr/local/bin/gocov-server
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/gocov-server"]
CMD ["serve"]
