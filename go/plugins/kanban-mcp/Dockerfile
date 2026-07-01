# Multi-stage build for the kanban-mcp server.
#
# The build context is expected to be the repository root so the entire `go/`
# workspace (a single module, github.com/kagent-dev/kagent/go) is available:
#
#   docker build -f go/plugins/kanban-mcp/Dockerfile -t kanban-mcp .
FROM golang:1.26-alpine AS builder

# GOPROXY is passed in by the build so module downloads go through the configured
# (e.g. internal/proxied) module mirror instead of the public proxy.golang.org,
# which may be unreachable in restricted build environments.
ARG GOPROXY
ENV GOPROXY=${GOPROXY}

WORKDIR /app

# Copy the Go module/workspace and build the kanban-mcp binary. The plugin lives
# under the single go/ module, so copying go/ is sufficient.
COPY go/ ./go/

WORKDIR /app/go

# CGO is not required: pgx is pure Go and the UI is embedded via //go:embed.
RUN CGO_ENABLED=0 go build -o /out/kanban-mcp ./plugins/kanban-mcp

FROM alpine:3.20

# ca-certificates lets the server dial TLS Postgres endpoints (e.g. managed PG).
# Copy the bundle from the builder (golang:alpine ships ca-certificates) instead
# of `apk add`, which can't reach the public Alpine CDN in restricted/proxied
# build environments.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

COPY --from=builder /out/kanban-mcp /usr/local/bin/kanban-mcp

# Default HTTP transport listens on :8080 (see internal/config defaults).
EXPOSE 8080

ENTRYPOINT ["kanban-mcp"]
