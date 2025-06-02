# Helm Chart Testing with helm-unittest

This document explains how to run unit tests for the kagent helm charts using [helm-unittest](https://github.com/helm-unittest/helm-unittest).

## What is helm-unittest?

helm-unittest is a BDD-styled unit test framework for Kubernetes Helm charts. It allows you to write tests that validate your chart templates render correctly with various input values, ensuring your charts work as expected before deployment.

## Installation

### Install helm-unittest as a Helm plugin

```bash
# Install the helm-unittest plugin
helm plugin install https://github.com/helm-unittest/helm-unittest

# Verify installation
helm unittest --help
```

### Alternative: Using Docker

If you prefer not to install the plugin, you can use Docker:

```bash
# Create an alias for easy use
alias helm-unittest='docker run --rm -v $(pwd):/apps helmunittest/helm-unittest:latest'
```

## Running Tests

### Test the main kagent chart

```bash
# Run all tests for the kagent chart
helm unittest helm/kagent

# Run tests with verbose output
helm unittest -v helm/kagent

# Run tests and update snapshots if needed
helm unittest -u helm/kagent

# Run specific test files
helm unittest -f "tests/*deployment*" helm/kagent
```

## Test Coverage

Our test suite covers:

### Main kagent Chart
- **Deployment Tests**: Container configuration, resource limits, environment variables, image tags
- **Service Tests**: Port configuration, service type, selector labels
- **RBAC Tests**: ServiceAccount, ClusterRole, ClusterRoleBinding configuration
- **Secret Tests**: Provider-specific API key secrets (OpenAI, Anthropic, Azure OpenAI)
- **ModelConfig Tests**: AI model configuration for different providers
- **Agent Tests**: Agent enablement/disablement and configuration
- **Integration Tests**: End-to-end chart rendering with various configurations


## Example Test Scenarios

### Testing with Different Values

```bash
# Test with custom values file
helm unittest -f values-production.yaml helm/kagent

# Test with inline value overrides
helm unittest --set replicaCount=3 --set global.tag=v2.0.0 helm/kagent
```

For more information, see the [helm-unittest documentation](https://github.com/helm-unittest/helm-unittest). 