# syntax=docker/dockerfile:1

# ── Build stage ────────────────────────────────────────────────────────────
# The admin SPA is prebuilt into proxy/webdist and //go:embed-ed into the binary,
# so no Node stage is needed — this is a pure Go build.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/kiro-pool-proxy .

# ── Runtime stage ──────────────────────────────────────────────────────────
FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 kiro

WORKDIR /app
COPY --from=builder /out/kiro-pool-proxy /app/kiro-pool-proxy
RUN mkdir -p /app/data && chown -R kiro:kiro /app

USER kiro
EXPOSE 5000
VOLUME /app/data

# config.json lives in the mounted /app/data volume so accounts persist.
ENTRYPOINT ["/app/kiro-pool-proxy"]
CMD ["--config", "/app/data/config.json"]
