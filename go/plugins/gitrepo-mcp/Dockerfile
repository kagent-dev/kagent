FROM golang:1.26-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go/ ./go/
WORKDIR /app/go
RUN CGO_ENABLED=1 go build -o gitrepo-mcp ./plugins/gitrepo-mcp

FROM alpine:3.20
RUN apk add --no-cache git ca-certificates
COPY --from=builder /app/go/gitrepo-mcp /usr/local/bin/gitrepo-mcp
ENTRYPOINT ["gitrepo-mcp"]
