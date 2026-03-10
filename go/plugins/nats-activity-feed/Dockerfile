FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go/ ./go/
WORKDIR /app/go
RUN go build -o nats-activity-feed ./plugins/nats-activity-feed

FROM alpine:3.20
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /app/go/nats-activity-feed /usr/local/bin/nats-activity-feed
ENTRYPOINT ["nats-activity-feed"]
