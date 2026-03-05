FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go/ ./go/
WORKDIR /app/go
RUN go build -o kanban-mcp ./plugins/kanban-mcp

FROM alpine:3.20
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /app/go/kanban-mcp /usr/local/bin/kanban-mcp
ENTRYPOINT ["kanban-mcp"]
