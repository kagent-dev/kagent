# CLI Testing Guide

This document describes how to write and run tests for the kagent CLI.

## Table of Contents

- [Overview](#overview)
- [Running Tests](#running-tests)
- [Writing Unit Tests](#writing-unit-tests)
- [Writing Integration Tests](#writing-integration-tests)
- [Writing TUI Tests](#writing-tui-tests)
- [Test Utilities](#test-utilities)
- [Best Practices](#best-practices)

## Overview

The CLI test suite is organized into two layers:

1. **Unit Tests** - Fast tests with no external dependencies (inline with code)
2. **Integration Tests** - Slower tests requiring external tools like Docker/kind (marked with `//go:build integration`)

## Running Tests

### Quick Start

```bash
# From repository root - run all tests including CLI
cd go && make test

# Run just CLI tests
cd go/core && go test ./cli/...

# Run with coverage
cd go/core && go test -coverprofile=coverage.out ./cli/...
cd go/core && go tool cover -html=coverage.out

# Run integration tests (requires Docker, kind, npm, etc.)
cd go/core && go test -tags=integration ./cli/...
```

### Test Flags

- `-v` - Verbose output
- `-run <pattern>` - Run specific tests matching pattern
- `-tags=integration` - Include integration tests
- `-coverprofile=file` - Generate coverage report

## Writing Unit Tests

### Location

Unit tests should be placed alongside the code they test:

```
cli/internal/cli/agent/
├── invoke.go
├── invoke_test.go       ← Unit tests here
├── get.go
└── get_test.go          ← Unit tests here
```

### Example: Testing a Pure Function

```go
package cli

import (
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestValidateAgentName(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantErr bool
    }{
        {name: "valid simple name", input: "dice", wantErr: false},
        {name: "invalid dash", input: "hello-agent", wantErr: true},
        {name: "empty", input: "", wantErr: true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := validateAgentName(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("validateAgentName(%q) error = %v, wantErr %v",
                    tt.input, err, tt.wantErr)
            }
        })
    }
}
```

### Example: Testing with Mocks

```go
func TestGetAgentCmd(t *testing.T) {
    // Create mock HTTP server
    mockServer := testutil.NewMockHTTPServer(t,
        testutil.MockAgentResponse([]api.AgentResponse{
            {Agent: testutil.CreateTestAgent("default", "test-agent")},
        }),
    )

    // Create config pointing to mock server
    cfg := &config.Config{KAgentURL: mockServer.URL}

    // Test the command
    GetAgentCmd(cfg, "test-agent")

    // Assert expected behavior
}
```

### Example: Testing with In-Memory Filesystem

```go
func TestInitCmd(t *testing.T) {
    // Create in-memory filesystem
    fs := testutil.NewMemFS()

    // Create test directory
    testutil.CreateTestDir(t, fs, "/tmp/test-project")

    // Run command with mocked filesystem
    cfg := &InitCfg{
        ProjectDir: "/tmp/test-project",
        AgentName:  "dice",
        // ...
    }

    err := InitCmd(cfg)
    assert.NoError(t, err)

    // Verify files were created
    assert.True(t, testutil.FileExists(t, fs, "/tmp/test-project/kagent.yaml"))
}
```

## Writing Integration Tests

### Location

Integration tests are placed alongside unit tests but use the `//go:build integration` build tag:

```
cli/internal/cli/agent/
├── invoke.go
├── invoke_test.go           ← Unit tests
└── integration_test.go      ← Integration tests (with build tag)
```

### Example: Integration Test

```go
//go:build integration

package agent

import (
    "testing"
    "os/exec"
)

func TestAgentDeployWorkflow(t *testing.T) {
    // Check for required tools
    if _, err := exec.LookPath("docker"); err != nil {
        t.Skip("docker not available, skipping integration test")
    }

    // Test init → build → deploy workflow
    // ...
}
```

## Writing TUI Tests

TUI tests use the [teatest](https://github.com/charmbracelet/x/tree/main/exp/teatest) library.

### Example: Testing TUI Model

```go
package tui

import (
    "testing"
    tea "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/x/exp/teatest"
    "github.com/charmbracelet/lipgloss"
    "github.com/muesli/termenv"
)

func init() {
    // Ensure consistent output in CI
    lipgloss.SetColorProfile(termenv.Ascii)
}

func TestWorkspaceInit(t *testing.T) {
    m := newWorkspaceModel(testCfg, mockClient, false)

    tm := teatest.NewTestModel(t, m,
        teatest.WithInitialTermSize(120, 40),
    )

    // Verify initial state
    finalModel := tm.FinalModel(t).(workspaceModel)
    assert.Equal(t, focusSessions, finalModel.focus)
}

func TestWorkspaceNavigation(t *testing.T) {
    m := newWorkspaceModel(testCfg, mockClient, false)

    tm := teatest.NewTestModel(t, m,
        teatest.WithInitialTermSize(120, 40),
    )

    // Simulate Tab key press
    tm.Send(tea.KeyMsg{Type: tea.KeyTab})

    // Wait for update
    tm.WaitFinished(t)

    // Verify focus changed
    finalModel := tm.FinalModel(t).(workspaceModel)
    assert.Equal(t, focusChat, finalModel.focus)
}
```

### Golden File Testing

For snapshot testing of TUI output:

```go
func TestWorkspaceView(t *testing.T) {
    m := newWorkspaceModel(testCfg, mockClient, false)

    tm := teatest.NewTestModel(t, m,
        teatest.WithInitialTermSize(120, 40),
    )

    // Update golden file with: UPDATE_GOLDEN=1 go test
    teatest.RequireEqualOutput(t, tm.Output())
}
```

## Test Utilities

The `test/testutil/` package provides helpers for common testing scenarios:

### Filesystem Utilities

```go
import "github.com/kagent-dev/kagent/go/core/cli/test/testutil"

// Create in-memory filesystem
fs := testutil.NewMemFS()

// Create test file
testutil.CreateTestFile(t, fs, "/path/to/file", "content")

// Create test directory
testutil.CreateTestDir(t, fs, "/path/to/dir")

// Read test file
content := testutil.ReadTestFile(t, fs, "/path/to/file")

// Check file exists
exists := testutil.FileExists(t, fs, "/path/to/file")
```

### Kubernetes Utilities

```go
// Create fake controller-runtime client
client := testutil.NewFakeControllerClient(t)

// Create fake clientset (for kubectl-style operations)
clientset := testutil.NewFakeK8sClientset()

// Create test resources
agent := testutil.CreateTestAgent("default", "test-agent")
secret := testutil.CreateTestSecret("default", "test-secret", map[string]string{
    "key": "value",
})
ns := testutil.CreateTestNamespace("test-ns")
```

### HTTP Utilities

```go
// Create mock HTTP server
mockServer := testutil.NewMockHTTPServer(t,
    testutil.MockAgentResponse([]api.AgentResponse{...}),
)

// Use mock server URL
cfg := &config.Config{KAgentURL: mockServer.URL}

// Other mock handlers
testutil.MockSessionResponse(sessions)
testutil.MockVersionResponse("v1.0.0")
testutil.MockErrorResponse(404, "not found")
```

### Docker/Kind Utilities

```go
// Skip test if Docker not available
testutil.RequireDocker(t)

// Skip test if kind not available
testutil.RequireKind(t)

// Check availability programmatically
if testutil.IsDockerAvailable() {
    // ...
}
```

## Best Practices

### 1. Use Table-Driven Tests

```go
func TestSomething(t *testing.T) {
    tests := []struct {
        name string
        input string
        want string
        wantErr bool
    }{
        {name: "case 1", input: "a", want: "b", wantErr: false},
        {name: "case 2", input: "c", want: "", wantErr: true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Something(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
            }
            if got != tt.want {
                t.Errorf("got %v, want %v", got, tt.want)
            }
        })
    }
}
```

### 2. Use t.Helper() for Test Utilities

```go
func createTestAgent(t *testing.T, name string) *v1alpha2.Agent {
    t.Helper() // Marks this function as a helper
    // ...
}
```

### 3. Clean Up Resources

```go
func TestWithCleanup(t *testing.T) {
    server := setupServer()
    t.Cleanup(func() {
        server.Stop()
    })
    // Test code...
}
```

### 4. Skip Slow Tests in Short Mode

```go
func TestSlow(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping slow test in short mode")
    }
    // ...
}
```

### 5. Use Subtests for Organization

```go
func TestFeature(t *testing.T) {
    t.Run("success case", func(t *testing.T) {
        // ...
    })

    t.Run("error case", func(t *testing.T) {
        // ...
    })
}
```

### 6. Mock External Dependencies

Never make real API calls or modify real resources in unit tests. Use:
- In-memory filesystems (afero)
- Mock HTTP servers (httptest)
- Fake Kubernetes clients (client-go/fake)
- Test databases (if needed)

### 7. Test Error Paths

Don't just test the happy path:

```go
func TestWithErrors(t *testing.T) {
    t.Run("success", func(t *testing.T) {
        // ...
    })

    t.Run("invalid input", func(t *testing.T) {
        // ...
    })

    t.Run("network error", func(t *testing.T) {
        // ...
    })
}
```

### 8. Use testify for Assertions

```go
import (
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// Use assert for non-critical checks
assert.Equal(t, expected, actual)
assert.NoError(t, err)

// Use require for critical checks (stops test on failure)
require.NoError(t, err) // Test stops here if err != nil
```

## CI/CD Integration

CLI tests run as part of the main test suite in `.github/workflows/ci.yaml`. The standard `make test` command runs all unit tests including CLI tests.

Integration tests can be run locally with `-tags=integration` but are not currently part of the CI pipeline.

## Coverage

To generate and view coverage:

```bash
cd go/core
go test -coverprofile=coverage.out ./cli/...
go tool cover -html=coverage.out -o coverage.html
# Open coverage.html in browser
```

Current coverage: ~21%

## Troubleshooting

### Integration tests fail with "Docker daemon not available"

Integration tests require external tools (Docker, kind, npm, uv, etc.). Either:
- Install the required tools
- Skip integration tests: `go test ./cli/...` (without `-tags=integration`)

### TUI tests have different output locally vs CI

Ensure you're setting consistent color profiles:

```go
func init() {
    lipgloss.SetColorProfile(termenv.Ascii)
}
```

### Golden file tests failing

Update golden files:

```bash
UPDATE_GOLDEN=1 go test ./cli/...
```

## Resources

- [Go Testing Package](https://pkg.go.dev/testing)
- [Testify Documentation](https://pkg.go.dev/github.com/stretchr/testify)
- [Teatest Guide](https://carlosbecker.com/posts/teatest/)
- [Table Driven Tests](https://go.dev/wiki/TableDrivenTests)
