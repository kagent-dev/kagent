# Stage 1: Build
FROM rust:1.85-slim AS builder

# Install build dependencies
RUN apt-get update && apt-get install -y pkg-config libssl-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Copy workspace
COPY Cargo.toml ./
COPY crates/ crates/

# Build release binary
RUN cargo build --release --bin kagent-adk

# Stage 2: Runtime
FROM gcr.io/distroless/cc-debian12:nonroot

COPY --from=builder /build/target/release/kagent-adk /app

USER 65532:65532

ENTRYPOINT ["/app"]
