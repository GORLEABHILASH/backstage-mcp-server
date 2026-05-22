# syntax=docker/dockerfile:1.7

# ----------------------------------------------------------------------------
# Build stage — produces a static linux/amd64 (or linux/arm64) binary.
# Uses Go build cache mounts to speed up rebuilds.
# ----------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

WORKDIR /src

# Copy module files first for better layer caching.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.serverVersion=${VERSION}" \
        -o /out/backstage-mcp-server ./cmd/server && \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" \
        -o /out/backstage-mcp-indexer ./cmd/indexer

# ----------------------------------------------------------------------------
# Runtime stage — distroless, non-root, no shell, ~25MB final image.
# OpenShift's restricted-v2 SCC requires running as a non-root, non-arbitrary
# UID with a numeric USER directive; 65532 ("nonroot") is the distroless
# default and satisfies that constraint.
# ----------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

LABEL org.opencontainers.image.title="backstage-mcp-server" \
      org.opencontainers.image.description="MCP server exposing Backstage Software Catalog and TechDocs RAG" \
      org.opencontainers.image.source="https://github.com/GORLEABHILASH/backstage-mcp-server" \
      org.opencontainers.image.licenses="Apache-2.0"

COPY --from=build /out/backstage-mcp-server /usr/local/bin/backstage-mcp-server
COPY --from=build /out/backstage-mcp-indexer /usr/local/bin/backstage-mcp-indexer

USER 65532:65532

EXPOSE 8080

ENV MCP_TRANSPORT=http \
    HTTP_ADDR=:8080 \
    HTTP_PATH=/mcp

ENTRYPOINT ["/usr/local/bin/backstage-mcp-server"]
