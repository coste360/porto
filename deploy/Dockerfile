# syntax=docker/dockerfile:1
#
# Porto production image (spec 31.1, 31.4).
#
#   docker build -f deploy/Dockerfile --build-arg VERSION=v0.1.0 -t ghcr.io/coste360/porto:dev .
#   docker buildx build -f deploy/Dockerfile --platform linux/amd64,linux/arm64 -t ghcr.io/coste360/porto:dev .
#
# Stages:
#   web      builds the admin and booking SPAs (Node, on the build platform)
#   build    compiles a static Go binary (CGO_ENABLED=0) with the SPAs embedded, cross-compiling to the target
#   runtime  distroless static, non-root, only /porto and the /data volume
#
# The first two stages always run on $BUILDPLATFORM: an arm64 image is produced by Go cross-compilation,
# not by emulating Node and Go under QEMU.

ARG NODE_IMAGE=node:22-alpine
ARG GO_IMAGE=golang:1.27-alpine
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot

# ---------------------------------------------------------------------------------------------------------------
# web: admin + booking SPAs
# ---------------------------------------------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS web
WORKDIR /src
ENV CI=true \
    NPM_CONFIG_AUDIT=false \
    NPM_CONFIG_FUND=false \
    NPM_CONFIG_UPDATE_NOTIFIER=false
# Workspace manifests first: the dependency layer is rebuilt only when a manifest or the lockfile changes.
COPY web/package*.json ./web/
COPY web/shared/package.json ./web/shared/
COPY web/admin/package.json ./web/admin/
COPY web/booking/package.json ./web/booking/
RUN --mount=type=cache,target=/root/.npm \
    if [ -f web/package-lock.json ]; then npm --prefix web ci; else npm --prefix web install; fi
COPY web ./web
COPY api ./api
RUN npm --prefix web run build

# ---------------------------------------------------------------------------------------------------------------
# build: static Go binary
# ---------------------------------------------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build
WORKDIR /src
ENV CGO_ENABLED=0 \
    GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
# .dockerignore leaves only the .gitkeep files in the static directories; drop in the fresh SPA builds.
COPY --from=web /src/web/admin/dist/ ./internal/http/static/admin/
COPY --from=web /src/web/booking/dist/ ./internal/http/static/booking/
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS="${TARGETOS:-linux}" GOARCH="${TARGETARCH:-amd64}" \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/porto ./cmd/porto
# /data must be writable by the distroless "nonroot" user (uid/gid 65532) without running as root.
RUN mkdir -p /out/data && chown 65532:65532 /out/data && chmod 0700 /out/data

# ---------------------------------------------------------------------------------------------------------------
# runtime: no shell, no package manager, non-root
# ---------------------------------------------------------------------------------------------------------------
FROM ${RUNTIME_IMAGE}
ARG VERSION=dev
LABEL org.opencontainers.image.title="Porto" \
      org.opencontainers.image.description="Gestionale per porti turistici e cantieri nautici" \
      org.opencontainers.image.vendor="Coste360 SRL" \
      org.opencontainers.image.licenses="BUSL-1.1" \
      org.opencontainers.image.source="https://github.com/coste360/porto" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=build /out/porto /porto
COPY --from=build --chown=65532:65532 /out/data /data
VOLUME ["/data"]
ENV PORTO_DATA_DIR=/data \
    PORTO_ADDR=:8080
EXPOSE 8080
# The image has no curl/wget: probe GET /healthz and /readyz from the orchestrator instead.
HEALTHCHECK NONE
USER nonroot:nonroot
# "docker run IMAGE" starts the server; "docker compose exec porto /porto <command>" runs the other commands.
ENTRYPOINT ["/porto"]
CMD ["serve"]
