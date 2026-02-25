FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go/ ./go/
WORKDIR /app/go
RUN go build -o kanban-mcp ./cmd/kanban-mcp

FROM alpine:3.20
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/go/kanban-mcp /usr/local/bin/kanban-mcp
ENTRYPOINT ["kanban-mcp"]
