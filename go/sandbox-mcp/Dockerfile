FROM golang:1.24 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /sandbox-mcp ./cmd/

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /sandbox-mcp /sandbox-mcp
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/sandbox-mcp"]
