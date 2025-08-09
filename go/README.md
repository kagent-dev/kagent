# Kagent Go Components

This directory contains the Go components of the Kagent project, including the controller and CLI tools.

## Directory Structure


- **cli/**: Command-line interface for Kagent
  - `cmd/`: Entry points for CLI commands
  - `internal/`: Internal CLI implementation

- **controller/**: Kubernetes controller for Kagent
  - `api/`: API definitions for custom resources
  - `cmd/`: Controller entry point
  - `internal/`: Internal controller implementation
  - `hack/`: Helper scripts and tools
  - `translator/`: Translator implementation.

- **internal/**: Internal code for the controller and CLI.
  - `httpserver/`: HTTP server for the controller.
  - `a2a/`: A2A implementation.
  - `adk/`: Go types for the declarative ADK framework.
  - `database/`: Database implementation.
  - `version/`: Version information for the controller.
  - `utils/`: Utility functions for the controller.

- **pkg/**: Shared code for the controller and CLI.


- **bin/**: Output directory for compiled binaries

- **test/**: Test files and e2e tests. Unit tests are co-located with the code they test.


## Building the Code

### Prerequisites

- Go 1.23 or later
- Docker (for container builds)
- Make

### Building the CLI

To build the CLI for multiple platforms:

```bash
# Build for all supported platforms
make build

# The binaries will be available in the bin/ directory:
# - bin/kagent-linux-amd64
# - bin/kagent-linux-arm64
# - bin/kagent-darwin-amd64
# - bin/kagent-darwin-arm64
# - bin/kagent-windows-amd64.exe
```

### Running the CLI locally

The CLI is a REPL (Read-Eval-Print Loop), so you can run it directly without building:

```bash
go run cli/cmd/kagent/main.go
```

### Building the Controller

To build the controller as a Docker image:

```bash
# Build the Docker image
make docker-build

# Push the Docker image to a registry
make docker-push
```

You can customize the image name and tag by setting the following variables:

```bash
# Example with custom values
make docker-build docker-push \
  DOCKER_REGISTRY=my-registry.io \
  DOCKER_REPO=my-org/kagent \
  IMAGE_NAME=controller \
  VERSION=v1.0.0
```

## Running the Code

### Running the CLI

After building, you can run the CLI directly from the bin directory:

```bash
# Linux/macOS
./bin/kagent-linux-amd64 [command]
# or
./bin/kagent-darwin-arm64 [command]

# Windows
./bin/kagent-windows-amd64.exe [command]
```

### Running the Controller

To run the controller locally:

```bash
make run
```

### Deploying to Kubernetes

To deploy the controller to a Kubernetes cluster:

```bash
# Install CRDs
make install

# Deploy the controller
make deploy
```

To undeploy:

```bash
# Undeploy the controller
make undeploy

# Uninstall CRDs
make uninstall
```

## Development

### Code Generation

```bash
# Generate manifests (CRDs, RBAC, etc.)
make manifests

# Generate DeepCopy methods
make generate
```

### Testing

```bash
# Run unit tests
make test

# Run end-to-end tests (requires a running Kind cluster)
make test-e2e
```

### Linting

```bash
# Run linters
make lint

# Fix linting issues automatically where possible
make lint-fix
```

## Building the Installer

To generate a consolidated YAML file for installation:

```bash
make build-installer
# The installer will be available at dist/install.yaml
```
