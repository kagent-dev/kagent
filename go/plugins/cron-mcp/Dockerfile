FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go/ ./go/
WORKDIR /app/go
RUN go build -o cron-mcp ./plugins/cron-mcp

FROM alpine:3.20
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /app/go/cron-mcp /usr/local/bin/cron-mcp
ENTRYPOINT ["cron-mcp"]
