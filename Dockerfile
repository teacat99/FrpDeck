# syntax=docker/dockerfile:1.7

# ---- Stage 1: frontend build ----
FROM node:20-alpine AS frontend
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# ---- Stage 2: backend build ----
FROM golang:1.25-alpine AS backend
WORKDIR /src
RUN apk add --no-cache git
# Allow overriding the Go module proxy at build time for environments where
# proxy.golang.org is slow or unreachable (e.g. CN networks).
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
# Embed version metadata into the binary; release.yml passes the git tag here.
ARG APP_VERSION=dev
ARG FRP_VERSION=v0.68.1
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Frontend dist from stage 1 overrides the placeholder so go:embed picks it up.
# (vite outDir is `../web/dist` relative to /app, resolving to /web/dist)
COPY --from=frontend /web/dist ./web/dist
RUN CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags="-s -w \
        -X 'github.com/teacat99/FrpDeck/internal/frpcd.BundledFrpVersion=${FRP_VERSION}' \
        -X 'main.appVersion=${APP_VERSION}'" \
      -o /out/frpdeck \
      ./cmd/server/

# ---- Stage 3: runtime ----
FROM alpine:3.23

# OCI-standard labels: rendered into image metadata for registries / scanners.
ARG APP_VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.title="FrpDeck" \
      org.opencontainers.image.description="Multi-frps tunnel manager with temporary tunnel lifecycle (frp client controller)." \
      org.opencontainers.image.url="https://github.com/teacat99/FrpDeck" \
      org.opencontainers.image.source="https://github.com/teacat99/FrpDeck" \
      org.opencontainers.image.documentation="https://github.com/teacat99/FrpDeck/tree/main/deploy" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.vendor="teacat99" \
      org.opencontainers.image.version="${APP_VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}"

RUN apk add --no-cache \
      ca-certificates \
      tzdata \
 && adduser -D -u 10001 frpdeck \
 && mkdir -p /data \
 && chown frpdeck:frpdeck /data

COPY --from=backend /out/frpdeck /usr/local/bin/frpdeck

ENV FRPDECK_LISTEN=":8080" \
    FRPDECK_DATA_DIR="/data" \
    FRPDECK_FRPCD_DRIVER="embedded" \
    FRPDECK_AUTH_MODE="password" \
    FRPDECK_HEALTH_URL="http://127.0.0.1:8080/api/version" \
    TZ="UTC"

VOLUME ["/data"]
EXPOSE 8080

# `/api/version` is unauthenticated and returns 200 + JSON when the daemon is
# fully booted, so we use it for liveness probing without needing a JWT.
# Override FRPDECK_HEALTH_URL when remapping the listen port inside the
# container (e.g. behind a non-default unix socket or alternate bind addr).
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider "${FRPDECK_HEALTH_URL}" || exit 1

USER frpdeck
ENTRYPOINT ["/usr/local/bin/frpdeck"]
