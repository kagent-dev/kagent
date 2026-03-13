FROM golang:1.24 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /sandbox-mcp ./cmd/

FROM python:3.13-slim
COPY --from=builder /sandbox-mcp /sandbox-mcp
EXPOSE 8080
ENTRYPOINT ["/sandbox-mcp"]
