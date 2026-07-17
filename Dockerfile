# syntax=docker/dockerfile:1

# Multi-stage, production-like image for DatoriumDB.
#
# Stage 1 (build) compiles both binaries (datoriumdb, datoriumctl) as
# static executables. Stage 2 (runtime) is a minimal, non-root image with
# a health check and a persistent /db volume, suitable for the Compose
# topologies under deploy/ and test/compose.

FROM golang:1.25-alpine AS build
WORKDIR /src

# The whole source tree (including the third_party/ojson local module
# used via go.mod's replace directive) needs to be present before `go mod
# download`/`go build` can resolve it, so we copy everything up front
# rather than trying to cache go.mod/go.sum separately.
COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/datoriumdb ./cmd/datoriumdb && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/datoriumctl ./cmd/datoriumctl

FROM alpine:3.20 AS runtime

RUN apk add --no-cache ca-certificates wget tzdata && \
    addgroup -S datorium && adduser -S -G datorium -h /db datorium && \
    mkdir -p /db && chown -R datorium:datorium /db

COPY --from=build /out/datoriumdb /out/datoriumctl /usr/local/bin/
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh /usr/local/bin/datoriumdb /usr/local/bin/datoriumctl

USER datorium:datorium
WORKDIR /db
VOLUME ["/db"]
EXPOSE 8080

# Health check hits the unauthenticated liveness endpoint
# (GET /datoriumdb/v1/health, tech-docs/LOCAL-ARCHITECTURE.md) and greps
# for ok:true; readiness (config loaded) is a separate concern checked by
# compose-level smoke tests via GET /datoriumdb/v1/ready.
HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=5 \
    CMD wget -q -O - http://127.0.0.1:8080/datoriumdb/v1/health | grep -q '"ok":true' || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
# CMD is intentionally empty: every deployment must supply at least
# <serverName> <establishmentBaseURL>, per
# tech-docs/ESTABLISHMENT-CONFIG.md's "Server Startup".
CMD []
