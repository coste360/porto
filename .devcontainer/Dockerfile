# Porto development container: Go toolchain from go.mod, golangci-lint and air.
# Node, Docker (for scripts/with-postgres.sh and compose) and gh come from devcontainer features.
FROM mcr.microsoft.com/devcontainers/go:1-bookworm

ARG GO_VERSION=1.27.4
ARG GOLANGCI_LINT_VERSION=v2.15.0
ARG AIR_VERSION=v1.67.4

# Replace the base image toolchain with the exact version declared in go.mod.
RUN set -eux; \
    arch="$(dpkg --print-architecture)"; \
    rm -rf /usr/local/go; \
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${arch}.tar.gz" | tar -C /usr/local -xz; \
    /usr/local/go/bin/go version

ENV GOTOOLCHAIN=local

RUN set -eux; \
    GOBIN=/usr/local/bin /usr/local/go/bin/go install \
      "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}"; \
    GOBIN=/usr/local/bin /usr/local/go/bin/go install "github.com/air-verse/air@${AIR_VERSION}"; \
    rm -rf /root/go /root/.cache/go-build; \
    golangci-lint version; \
    air -v
