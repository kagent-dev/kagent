## Code commenting best practices

### General Philosophy

- **Self-Documenting Code First**: Write code that explains itself through clear structure and naming
- **Minimal, Helpful Comments**: Add concise comments to explain complex logic sections
- **Evergreen Content**: Comments should be relevant far into the future, not tied to recent changes
- **Don't Comment the Obvious**: Avoid comments that simply restate what the code does

---

### Go Commenting

#### Package Documentation

```go
// Package httpserver provides HTTP handlers for the Kagent REST API.
// It implements endpoints for agent management, sessions, and model configs.
package httpserver
```

#### Function Documentation

```go
// HandleListAgents returns all agents for the authenticated user.
// Agents can be filtered by namespace using the query parameter.
func (h *AgentHandler) HandleListAgents(w http.ResponseWriter, r *http.Request) {
    // Implementation
}
```

#### Complex Logic Comments

```go
// Ensure the session exists before processing the request
session := await self._prepare_session(context, run_args, runner)

// Priority of task state (highest to lowest):
// 1. failed
// 2. auth_required
// 3. input_required
// 4. working
if task.Status == "failed" {
    return errors.New("task failed")
}
```

#### TODO Comments

```go
// TODO: add user and agent headers for authentication
func buildHeaders() map[string]string {
    return map[string]string{}
}
```

---

### Python Commenting

#### Module Docstrings

```python
"""LangGraph Agent Executor for A2A Protocol.

This module implements an agent executor that runs LangGraph workflows
within the A2A (Agent-to-Agent) protocol, converting graph events to A2A events.
"""
```

#### Class Docstrings

```python
class KAgentSessionService(BaseSessionService):
    """A session service implementation that uses the Kagent API.

    This service integrates with the Kagent server to manage session state
    and persistence through HTTP API calls.
    """
```

#### Function Docstrings (Google Style)

```python
async def get(self, task_id: str) -> Task | None:
    """Retrieve a task from KAgent.

    Args:
        task_id: The ID of the task to retrieve

    Returns:
        The task if found, None otherwise

    Raises:
        httpx.HTTPStatusError: If the API request fails (except 404)
    """
```

#### Inline Comments (Strategic)

```python
# ensure the session exists
session = await self._prepare_session(context, run_args, runner)

# Priority of task state:
# - failed
# - auth_required
# - input_required
# - working
```

#### TODO/FIXME Comments

```python
# TODO: implement pagination for large result sets
def list_sessions(self):
    pass

# TODO: add support for after_timestamp filtering
if config.after_timestamp:
    # Not yet implemented
    pass
```

---

### TypeScript/React Commenting

#### Component Documentation

```tsx
/**
 * ChatInterface provides an interactive chat UI for communicating with agents.
 * Supports real-time streaming, session management, and message history.
 */
export function ChatInterface({ agent, session }: Props) {
  // Implementation
}
```

#### Complex Logic Comments

```tsx
useEffect(() => {
  // Skip completely if this is a first message session creation flow
  if (isFirstMessage || isCreatingSessionRef.current) {
    return
  }

  // Initialize chat by loading existing session data
  async function initializeChat() {
    // ...
  }
}, [sessionId, isFirstMessage])
```

#### Props/Types Documentation

```tsx
interface Props {
  /** The agent to chat with */
  agent: Agent
  /** Optional session to continue, or undefined for new session */
  session?: Session
  /** Callback when user closes the chat */
  onClose?: () => void
}
```

---

## What NOT to Comment

### Don't Comment Obvious Code

```go
// Bad - obvious
i++ // increment i

// Bad - just restates code
// Get the user ID from the request header
userID := r.Header.Get("X-User-ID")
```

### Don't Comment Temporary Changes

```go
// Bad - tied to specific change
// Fixed bug #123 where agents weren't loading

// Bad - temporary note
// Changed this on 2025-01-15 to fix performance issue
```

### Don't Comment Out Code

```go
// Bad - delete instead of commenting
// func oldImplementation() {
//     // ...
// }

// Good - just delete it (it's in git history)
```

### Don't Explain Variable Names

```python
# Bad - obvious from name
user_id = request.headers.get("X-User-ID")  # Get the user ID

# Good - no comment needed
user_id = request.headers.get("X-User-ID")
```

---

## When TO Comment

### Complex Algorithms

```python
# Use exponential backoff with jitter to avoid thundering herd
delay = (2 ** attempt) + random.uniform(0, 1)
```

### Business Logic

```go
// RFC 1123 DNS subdomain: lowercase alphanumeric, '-', '.'
// Must start and end with alphanumeric character
pattern := `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
```

### Non-Obvious Decisions

```python
# Return None for 404 instead of raising - allows callers
# to distinguish between "not found" and "error"
if response.status_code == 404:
    return None
```

### API/Interface Contracts

```go
// HandleCreateAgent creates a new agent in the specified namespace.
// Returns 400 if the agent name is invalid.
// Returns 409 if an agent with the same name already exists.
func (h *AgentHandler) HandleCreateAgent(w http.ResponseWriter, r *http.Request)
```

### Warnings and Gotchas

```python
# WARNING: This mutates the input list. Use copy() if you need the original.
def sort_in_place(items: list):
    items.sort()
```

---

## Best Practices

1. **Write Self-Documenting Code First**: Good names > Comments
2. **Comment Why, Not What**: Explain rationale, not mechanics
3. **Keep Comments Updated**: Outdated comments are worse than no comments
4. **Use Standard Formats**: Package docs, function docs (Google/JSDoc style)
5. **Be Concise**: Short, clear comments are better than long paragraphs
6. **Avoid Redundancy**: Don't repeat what the code clearly shows
7. **TODOs Are OK**: Mark incomplete work, but include context
8. **Delete Dead Code**: Don't comment it out, delete it (use git history)
