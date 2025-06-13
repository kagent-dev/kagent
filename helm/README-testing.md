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

# Run specific test files
helm unittest -f "tests/*deployment*" helm/kagent
```

## Test Structure

The tests are organized as follows:

```
helm/
├── kagent/
│   └── tests/
│       ├── deployment_test.yaml      # Tests for deployment template
│       ├── service_test.yaml         # Tests for service template
│       ├── rbac_test.yaml           # Tests for RBAC resources
│       ├── secret_test.yaml         # Tests for secret template
│       ├── modelconfig_test.yaml    # Tests for modelconfig template
│       └── integration_test.yaml    # Integration tests
└── README-testing.md                # This file
```

## Test Coverage

Our test suite covers:

### Main kagent Chart
- **Deployment Tests**: Container configuration, resource limits, environment variables, image tags
- **Service Tests**: Port configuration, service type, selector labels
- **RBAC Tests**: ServiceAccount, ClusterRole, ClusterRoleBinding configuration
- **Secret Tests**: Provider-specific API key secrets (OpenAI, Anthropic, Azure OpenAI)
- **ModelConfig Tests**: AI model configuration for different providers
- **Integration Tests**: End-to-end chart rendering with various configurations

## Example Test Scenarios

### Testing with Different Values

```bash
# Test with custom values file
helm unittest -f values-production.yaml helm/kagent

# Test with inline value overrides
helm unittest --set replicaCount=3 --set global.tag=v2.0.0 helm/kagent
```

## Writing New Tests

### Test File Structure

```yaml
suite: test description
templates:
  - template1.yaml
  - template2.yaml
tests:
  - it: should do something
    set:
      key: value
    asserts:
      - isKind:
          of: Deployment
      - equal:
          path: metadata.name
          value: expected-name
```

### Common Assertions

- `isKind`: Check resource type
- `equal`: Check exact value
- `notEqual`: Check value is not equal
- `contains`: Check array/object contains item
- `notContains`: Check array/object does not contain item
- `isNotEmpty`: Check value is not empty
- `isEmpty`: Check value is empty
- `hasDocuments`: Check number of rendered documents
- `matchRegex`: Check value matches regex pattern
- `matchSnapshot`: Compare against saved snapshot

### JSONPath Support

Use JSONPath to access nested values:

```yaml
- equal:
    path: spec.template.spec.containers[0].resources.limits.memory
    value: 512Mi
- equal:
    path: metadata.annotations["prometheus.io/scrape"]
    value: "true"
```

### Testing Dynamic Values

When testing values that change (like image tags), use regex patterns:

```yaml
- matchRegex:
    path: spec.template.spec.containers[0].image
    pattern: "^cr\\.kagent\\.dev/kagent-dev/kagent/controller:.+"
```

## Continuous Integration

Add helm-unittest to your CI/CD pipeline:

```yaml
# GitHub Actions example
- name: Install helm-unittest
  run: helm plugin install https://github.com/helm-unittest/helm-unittest

- name: Run helm tests
  run: helm unittest helm/kagent
```

## Best Practices

1. **Test Default Values**: Always test that charts render correctly with default values
2. **Test Edge Cases**: Test with various configurations and edge cases
3. **Test Resource Limits**: Validate CPU/memory requests and limits
4. **Test Security**: Validate RBAC, security contexts, and secrets handling
5. **Test Labels**: Ensure consistent labeling across resources
6. **Test Conditional Logic**: Test when features are enabled/disabled
7. **Use Regex for Dynamic Values**: Use `matchRegex` for values that change between environments (like image tags)
8. **Keep Tests Simple**: Each test should validate one specific behavior
9. **Use Descriptive Names**: Test names should clearly describe what's being tested

## Troubleshooting

### Common Issues

1. **Template not found**: Ensure template paths are correct relative to chart root
2. **Assertion failures**: Check JSONPath syntax and expected values
3. **Values not applied**: Verify `set` syntax and value inheritance
4. **Version mismatches**: Use regex patterns for dynamic values like image tags

### Debug Tips

```bash
# Check what templates would be rendered
helm template test-release helm/kagent

# Check specific template with custom values
helm template test-release helm/kagent --show-only templates/deployment.yaml

# Validate chart syntax
helm lint helm/kagent
```

## Known Limitations

- **CRD Charts**: Some charts with complex CRD structures may not work with helm-unittest
- **Subcharts**: Agent subcharts require parent chart context and are best tested as part of the main chart
- **Template Dependencies**: Charts that heavily depend on helper templates from parent charts may need special handling

For more information, see the [helm-unittest documentation](https://github.com/helm-unittest/helm-unittest). 