## Error handling best practices

### Go Error Handling

#### Custom Error Types

From `go/internal/httpserver/errors/errors.go`:

```go
type APIError struct {
    StatusCode int
    Message    string
    Err        error
}

func (e *APIError) Error() string {
    if e.Err != nil {
        return fmt.Sprintf("%s: %v", e.Message, e.Err)
    }
    return e.Message
}
```

**Error Factory Functions:**

```go
func NewBadRequest(message string) *APIError {
    return &APIError{
        StatusCode: http.StatusBadRequest,
        Message:    message,
    }
}

func NewNotFound(message string) *APIError {
    return &APIError{
        StatusCode: http.StatusNotFound,
        Message:    message,
    }
}

func NewInternalError(err error) *APIError {
    return &APIError{
        StatusCode: http.StatusInternalServerError,
        Message:    "Internal server error",
        Err:        err,
    }
}
```

#### Wrap Errors with Context

```go
// Add context when propagating errors
agent, err := h.Database.GetAgent(ctx, namespace, name, userID)
if err != nil {
    return fmt.Errorf("failed to get agent %s/%s: %w", namespace, name, err)
}
```

#### Check Errors Immediately

```go
result, err := doSomething()
if err != nil {
    return fmt.Errorf("operation failed: %w", err)
}
// Continue with result
```

#### Distinguish Not Found vs Error

```go
func (s *Service) GetAgent(ctx context.Context, namespace, name, userID string) (*Agent, error) {
    var agent Agent
    err := s.db.WithContext(ctx).
        Where("namespace = ? AND name = ? AND user_id = ?", namespace, name, userID).
        First(&agent).Error

    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, nil  // Not found is not an error
    }
    if err != nil {
        return nil, fmt.Errorf("database error: %w", err)
    }
    return &agent, nil
}
```

#### Centralized HTTP Error Handling

```go
func handleError(w http.ResponseWriter, err error) {
    var apiErr *APIError
    if errors.As(err, &apiErr) {
        respondJSON(w, apiErr.StatusCode, map[string]interface{}{
            "error": apiErr.Message,
        })
        return
    }

    // Default to 500 for unexpected errors
    respondJSON(w, http.StatusInternalServerError, map[string]interface{}{
        "error": "Internal server error",
    })
}
```

---

### Python Error Handling

#### HTTP Error Handling

From `python/packages/kagent-core/src/kagent/core/a2a/_task_store.py`:

```python
async def get(self, task_id: str) -> Task | None:
    """Retrieve a task from KAgent."""
    response = await self.client.get(f"/api/tasks/{task_id}")

    # Handle 404 specially - not found is None, not error
    if response.status_code == 404:
        return None

    # Raise for other HTTP errors
    response.raise_for_status()

    return Task.model_validate(response.json())
```

#### Try-Except-Finally Pattern

From `python/packages/kagent-adk/src/kagent/adk/_agent_executor.py`:

```python
try:
    await self._handle_request(context, event_queue, runner)
except Exception as e:
    logger.error("Error handling A2A request: %s", e, exc_info=True)

    # Publish failure event
    try:
        await event_queue.enqueue_event(
            Event(type="error", error=ErrorPart(message=str(e)))
        )
    except Exception as enqueue_error:
        logger.error("Failed to publish failure event: %s", enqueue_error, exc_info=True)
finally:
    # Cleanup resources
    if runner:
        await runner.cleanup()
```

#### Validation with Pydantic

```python
from pydantic import BaseModel, validator

class AgentConfig(BaseModel):
    name: str
    model: str

    @validator('name')
    def name_must_not_be_empty(cls, v):
        if not v or not v.strip():
            raise ValueError('Agent name must be a non-empty string')
        return v
```

#### Explicit ValueError Raises

From `python/packages/kagent-adk/src/kagent/adk/types.py`:

```python
if name is None or not str(name).strip():
    raise ValueError("Agent name must be a non-empty string.")

if not context.message:
    raise ValueError("A2A request must have a message")
```

#### Graceful Degradation

From `python/packages/kagent-adk/src/kagent/adk/converters/event_converter.py`:

```python
def _serialize_metadata_value(value: Any) -> str:
    """Safely serializes metadata values to string format."""
    if hasattr(value, "model_dump"):
        try:
            return value.model_dump(exclude_none=True, by_alias=True)
        except Exception as e:
            logger.warning("Failed to serialize metadata value: %s", e)
            return str(value)  # Fall back to string representation
    return str(value)
```

#### Logging with Exception Info

```python
try:
    result = await process_request()
except Exception as e:
    logger.error("Request processing failed: %s", e, exc_info=True)  # Includes stack trace
    raise
```

---

### TypeScript/React Error Handling

#### API Error Handling

From `ui/src/app/actions/`:

```tsx
export async function getAgents(): Promise<{ data?: Agent[]; error?: string }> {
  try {
    const response = await fetch('/api/agents')

    if (!response.ok) {
      return { error: `Failed to fetch agents: ${response.statusText}` }
    }

    const data = await response.json()
    return { data }
  } catch (error) {
    return { error: 'Network error fetching agents' }
  }
}
```

#### Component Error Handling

From `ui/src/components/chat/ChatInterface.tsx`:

```tsx
useEffect(() => {
  async function initializeChat() {
    setIsLoading(true)

    try {
      const response = await checkSessionExists(sessionId)

      if (response.error) {
        toast.error("Session not found")
        setSessionNotFound(true)
        return
      }

      const messages = await getSessionTasks(sessionId)
      if (messages.error) {
        toast.error("Failed to load messages")
      } else {
        setMessages(messages.data)
      }
    } catch (error) {
      toast.error("Error loading chat")
      console.error(error)
    } finally {
      setIsLoading(false)
    }
  }

  initializeChat()
}, [sessionId])
```

#### Form Validation Errors

From `ui/src/components/AgentsProvider.tsx`:

```tsx
const validateAgentData = (data: Partial<AgentFormData>): ValidationErrors => {
  const errors: ValidationErrors = {}

  if (data.name !== undefined) {
    if (!data.name.trim()) {
      errors.name = "Agent name is required"
    }
    if (!isResourceNameValid(data.name)) {
      errors.name = "Agent name must be lowercase alphanumeric with hyphens"
    }
  }

  if (!data.modelName || data.modelName.trim() === "") {
    errors.model = "Please select a model"
  }

  return errors
}
```

#### Display Errors to Users

```tsx
{errors.name && (
  <p className="text-destructive text-sm mt-1" role="alert">
    {errors.name}
  </p>
)}
```

#### Error Boundaries (Next.js)

```tsx
// error.tsx in app directory
export default function Error({
  error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  useEffect(() => {
    console.error(error)
  }, [error])

  return (
    <div className="flex flex-col items-center justify-center p-8">
      <h2 className="text-2xl font-bold mb-4">Something went wrong!</h2>
      <p className="text-muted-foreground mb-4">{error.message}</p>
      <Button onClick={reset}>Try again</Button>
    </div>
  )
}
```

---

## Best Practices Summary

### General Principles

1. **Fail Fast**: Check conditions early and return/throw immediately
2. **Add Context**: Wrap errors with meaningful context about what failed
3. **User-Friendly Messages**: Show clear, actionable messages to users
4. **Log Technical Details**: Log full error details for debugging
5. **Distinguish Error Types**: Treat not-found differently from actual errors
6. **Clean Up Resources**: Use finally blocks or defer to ensure cleanup
7. **Don't Swallow Errors**: Always handle or propagate errors, never ignore
8. **Specific Error Types**: Use custom error types for different failure modes

### Logging

- **Go**: Use structured logging with context
- **Python**: Use `logger.error()` with `exc_info=True` for stack traces
- **TypeScript**: Use `console.error()` and error tracking services

### User Feedback

- **Go**: Return appropriate HTTP status codes
- **Python**: Raise exceptions with clear messages
- **TypeScript**: Use toast notifications and inline form errors
