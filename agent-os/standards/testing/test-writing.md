## Test coverage best practices

### Testing Philosophy

- **Write Minimal Tests During Development**: Do NOT write tests for every change or intermediate step. Focus on completing the feature implementation first, then add strategic tests only at logical completion points
- **Test Only Core User Flows**: Write tests exclusively for critical paths and primary user workflows. Skip writing tests for non-critical utilities and secondary workflows until if/when you're instructed to do so.
- **Defer Edge Case Testing**: Do NOT test edge cases, error states, or validation logic unless they are business-critical. These can be addressed in dedicated testing phases, not during feature development.
- **Test Behavior, Not Implementation**: Focus tests on what the code does, not how it does it, to reduce brittleness
- **Clear Test Names**: Use descriptive names that explain what's being tested and the expected outcome
- **Mock External Dependencies**: Isolate units by mocking databases, APIs, file systems, and other external services
- **Fast Execution**: Keep unit tests fast (milliseconds) so developers run them frequently during development

---

## Go Testing

### Test Framework

From `go/go.mod`:
- **Framework**: Standard Go `testing` package
- **Assertions**: `github.com/stretchr/testify/assert`
- **Linting**: `golangci-lint`

### Test Structure

```go
package database

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestGetAgent(t *testing.T) {
    // Arrange
    db := setupTestDB(t)
    defer cleanupTestDB(t, db)

    agent := &Agent{
        Name:      "test-agent",
        Namespace: "default",
        UserID:    "test-user",
    }
    db.Create(agent)

    // Act
    result, err := db.GetAgent(context.Background(), "default", "test-agent", "test-user")

    // Assert
    assert.NoError(t, err)
    assert.NotNil(t, result)
    assert.Equal(t, "test-agent", result.Name)
}
```

### Table-Driven Tests

```go
func TestIsResourceNameValid(t *testing.T) {
    tests := []struct {
        name  string
        input string
        want  bool
    }{
        {"valid name", "my-agent", true},
        {"with numbers", "agent-123", true},
        {"uppercase", "My-Agent", false},
        {"starts with hyphen", "-agent", false},
        {"ends with hyphen", "agent-", false},
        {"empty", "", false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := isResourceNameValid(tt.input)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

### Mocking

```go
// Mock interface
type MockClient struct {
    GetAgentFunc func(ctx context.Context, id string) (*Agent, error)
}

func (m *MockClient) GetAgent(ctx context.Context, id string) (*Agent, error) {
    if m.GetAgentFunc != nil {
        return m.GetAgentFunc(ctx, id)
    }
    return nil, nil
}

// Use in test
func TestHandleGetAgent(t *testing.T) {
    mock := &MockClient{
        GetAgentFunc: func(ctx context.Context, id string) (*Agent, error) {
            return &Agent{Name: "test"}, nil
        },
    }

    handler := NewHandler(mock)
    // Test handler...
}
```

---

## Python Testing

### Test Framework

From `python/packages/kagent-adk/pyproject.toml`:
- **Framework**: `pytest` 8.3.5+
- **Async**: `pytest-asyncio` 0.25.3+
- **Mocking**: `unittest.mock.Mock`
- **Linting**: `ruff`

### Test Structure

```python
import pytest
from unittest.mock import Mock

async def test_get_task():
    # Arrange
    mock_client = Mock()
    mock_client.get = Mock(return_value=Mock(
        status_code=200,
        json=lambda: {"id": "task-1", "status": "completed"}
    ))

    task_store = KAgentTaskStore(mock_client)

    # Act
    result = await task_store.get("task-1")

    # Assert
    assert result is not None
    assert result.id == "task-1"
    assert result.status == "completed"
```

### Parametrized Tests

From `python/packages/kagent-adk/tests/unittests/models/test_openai.py`:

```python
test_cases = [
    ("valid_model", "gpt-4", "gpt-4"),
    ("valid_claude", "claude-3", "claude-3"),
    ("empty_model", "", ValueError),
]

@pytest.mark.parametrize("name, input_model, expected", test_cases, ids=[c[0] for c in test_cases])
def test_model_validation(name, input_model, expected):
    if expected == ValueError:
        with pytest.raises(ValueError):
            validate_model(input_model)
    else:
        result = validate_model(input_model)
        assert result == expected
```

### Async Tests

```python
@pytest.mark.asyncio
async def test_aput_checkpoint():
    # Arrange
    mock_client = Mock()
    mock_client.post = Mock(return_value=Mock(status_code=201))

    checkpointer = KAgentCheckpointer(mock_client)

    # Act
    await checkpointer.aput({"thread_id": "test"}, checkpoint_data)

    # Assert
    mock_client.post.assert_called_once()
```

### Fixtures

```python
@pytest.fixture
def mock_httpx_client():
    """Provide a mock HTTP client for testing."""
    client = Mock()
    client.get = Mock(return_value=Mock(status_code=200))
    client.post = Mock(return_value=Mock(status_code=201))
    return client

def test_with_fixture(mock_httpx_client):
    service = MyService(mock_httpx_client)
    # Test...
```

---

## TypeScript/React Testing

### Test Framework

From `ui/package.json`:
- **Unit Tests**: Jest 29.7
- **Component Testing**: `@testing-library/react` 14.3.1
- **User Events**: `@testing-library/user-event` 14.6.1
- **DOM Assertions**: `@testing-library/jest-dom` 6.6.3
- **E2E**: Cypress 14.5.0
- **API Mocking**: MSW 2.10.2

### Component Testing

```tsx
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Button } from '@/components/ui/button'

describe('Button', () => {
  it('renders with text', () => {
    render(<Button>Click me</Button>)
    expect(screen.getByRole('button', { name: /click me/i })).toBeInTheDocument()
  })

  it('calls onClick when clicked', async () => {
    const handleClick = jest.fn()
    render(<Button onClick={handleClick}>Click me</Button>)

    await userEvent.click(screen.getByRole('button'))
    expect(handleClick).toHaveBeenCalledTimes(1)
  })

  it('is disabled when disabled prop is true', () => {
    render(<Button disabled>Click me</Button>)
    expect(screen.getByRole('button')).toBeDisabled()
  })
})
```

### Testing Async Components

```tsx
import { render, screen, waitFor } from '@testing-library/react'

describe('AgentList', () => {
  it('loads and displays agents', async () => {
    render(<AgentList />)

    // Check loading state
    expect(screen.getByText(/loading/i)).toBeInTheDocument()

    // Wait for data to load
    await waitFor(() => {
      expect(screen.getByText('my-agent')).toBeInTheDocument()
    })
  })
})
```

### Mocking API Calls (MSW)

```tsx
import { rest } from 'msw'
import { setupServer } from 'msw/node'

const server = setupServer(
  rest.get('/api/agents', (req, res, ctx) => {
    return res(ctx.json([
      { id: '1', name: 'agent-1' },
      { id: '2', name: 'agent-2' },
    ]))
  })
)

beforeAll(() => server.listen())
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

test('fetches agents', async () => {
  render(<AgentList />)
  await waitFor(() => {
    expect(screen.getByText('agent-1')).toBeInTheDocument()
  })
})
```

### E2E Testing (Cypress)

From `ui/cypress/e2e/smoke.cy.ts`:

```typescript
describe('Agent Creation', () => {
  it('creates a new agent', () => {
    cy.visit('/agents/new')

    cy.get('[data-testid="agent-name-input"]').type('my-new-agent')
    cy.get('[data-testid="namespace-select"]').select('default')
    cy.get('[data-testid="model-select"]').select('gpt-4')

    cy.get('[data-testid="create-button"]').click()

    cy.url().should('include', '/agents/default/my-new-agent')
    cy.contains('Agent created successfully')
  })
})
```

---

## Best Practices Summary

### What to Test

**High Priority:**
- Critical user workflows
- Business logic
- Error handling (for critical paths)
- API endpoints (core functionality)
- Data transformations

**Lower Priority:**
- Utility functions (unless complex)
- Getters/setters
- Simple UI components
- Edge cases (unless critical)

### What NOT to Test

- Third-party library code
- Auto-generated code
- Simple data structures
- Configuration files
- Obvious functionality

### Test Naming

**Go:**
```go
func TestFunctionName_Scenario_ExpectedBehavior(t *testing.T)
// Example: TestGetAgent_WithValidID_ReturnsAgent
```

**Python:**
```python
def test_function_name_scenario_expected_behavior():
    # Example: test_get_task_with_valid_id_returns_task
```

**TypeScript:**
```typescript
describe('ComponentName', () => {
  it('does something when condition', () => {
    // Example: 'renders error when API fails'
  })
})
```

### Test Independence

- Each test should be independent
- No shared state between tests
- Use setup/teardown to clean state
- Tests should pass in any order

### Mocking Guidelines

- Mock external dependencies (APIs, databases)
- Don't mock the system under test
- Use real objects when possible (prefer fakes over mocks)
- Keep mocks simple and focused

### Performance

- Unit tests should be fast (< 100ms each)
- Integration tests can be slower (< 5s each)
- E2E tests are slowest (< 30s each)
- Run unit tests frequently, E2E less often
