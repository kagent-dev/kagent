## Coding style best practices

### Language-Specific Styles

Kagent is a multi-language project. Follow the conventions for each language.

---

## Go Coding Style

### Naming Conventions

**Constants:**
```go
const APIPathAgents = "/api/agents"
const APIPathSessions = "/api/sessions"
```

**Interfaces:** PascalCase
```go
type Client interface{}
type Handler interface{}
```

**Structs:** PascalCase
```go
type ServerConfig struct{}
type AgentHandler struct{}
```

**Functions and Methods:**
```go
func HandleListAgents(w http.ResponseWriter, r *http.Request) {}
func (h *AgentHandler) SetConfig(config ServerConfig) {}
```

**Variables:**
- Short names for local scope: `w`, `r`, `h`, `c`, `err`
- Descriptive names for package scope: `ServerConfig`, `Database`

**JSON Tags:** snake_case
```go
type Agent struct {
    Name      string `json:"name"`
    Namespace string `json:"namespace"`
    UserID    string `json:"user_id"`
}
```

### Code Organization

```go
// 1. Package declaration
package httpserver

// 2. Imports (grouped: stdlib, external, internal)
import (
    "context"
    "net/http"

    "github.com/gorilla/mux"

    "github.com/kagent-dev/kagent/go/internal/database"
)

// 3. Constants
const APIPathAgents = "/api/agents"

// 4. Types
type Handler struct {
    db *database.Service
}

// 5. Constructor
func NewHandler(db *database.Service) *Handler {
    return &Handler{db: db}
}

// 6. Methods
func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {}
```

### Error Handling

```go
// Check and return early
if err != nil {
    return fmt.Errorf("failed to get agent: %w", err)
}

// Wrap errors with context
err = db.Create(agent)
if err != nil {
    return fmt.Errorf("database create failed: %w", err)
}
```

### Function Size

- Keep functions small and focused (< 50 lines ideal)
- Extract complex logic into helper functions
- Use meaningful function names

---

## Python Coding Style

### Formatting

**Tool:** Ruff (configured in `pyproject.toml`)

```bash
ruff format .    # Format code
ruff check .     # Lint code
```

### Naming Conventions

**Modules/Files:** snake_case with leading underscore for private
```
_a2a.py
_session_service.py
event_converter.py
```

**Classes:** PascalCase
```python
class KAgentApp:
class A2aAgentExecutor:
class AgentConfig(BaseModel):
```

**Functions:** snake_case
```python
def convert_event_to_a2a_events():
def _serialize_metadata_value():  # Private with underscore
async def aput():  # Async with 'a' prefix
```

**Constants:** ALL_CAPS
```python
ARTIFACT_ID_SEPARATOR = "-"
A2A_DATA_PART_METADATA_TYPE_KEY = "type"
```

**Variables:** snake_case
```python
http_client = httpx.AsyncClient()
task_store = KAgentTaskStore()
event_queue = EventQueue()
```

### Type Hints

**Modern Python 3.11+ syntax:**

```python
# Use | for unions (not Union)
def get_task(id: str) -> Task | None:
    pass

# Use dict, list directly (not Dict, List from typing)
def process(data: dict[str, Any]) -> list[str]:
    pass

# Optional parameters
def create_session(name: str, metadata: dict[str, Any] | None = None):
    pass
```

### Async Methods

Prefix with `a` for async versions:

```python
class Checkpointer:
    def put(self, checkpoint: Checkpoint):
        raise NotImplementedError("Use aput")

    async def aput(self, checkpoint: Checkpoint):
        # Async implementation
        await self.client.post("/api/checkpoints", json=checkpoint)
```

### Docstrings

Google-style with Args/Returns/Raises:

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
    response = await self.client.get(f"/api/tasks/{task_id}")
    if response.status_code == 404:
        return None
    response.raise_for_status()
    return Task.model_validate(response.json())
```

### Pydantic Models

```python
class AgentConfig(BaseModel):
    name: str
    model: str
    system_prompt: str | None = None
    headers: dict[str, str] | None = None

    @override
    def model_dump(self) -> dict[str, Any]:
        return super().model_dump(exclude_none=True, by_alias=True)
```

---

## TypeScript/React Coding Style

### Naming Conventions

**Components:** PascalCase
```tsx
export function ChatInterface() {}
export function AgentCard() {}
```

**Files:** PascalCase for components
```
ChatInterface.tsx
AgentCard.tsx
SessionsSidebar.tsx
```

**Functions:** camelCase
```tsx
const handleSubmit = () => {}
const fetchAgents = async () => {}
```

**Event Handlers:** `handle{Action}`
```tsx
const handleClick = () => {}
const handleInputChange = (e: ChangeEvent) => {}
const handleSubmit = async (data: FormData) => {}
```

**Hooks:** `use{Name}`
```tsx
const useAgents = () => {}
const useIsMobile = () => {}
```

**Props Interfaces:**
```tsx
interface Props {
  agent: Agent
  onClose: () => void
}

// Or inline
export function Component({ title, count }: { title: string; count: number }) {}
```

### File Structure

```tsx
// 1. Imports
import React, { useState, useEffect } from 'react'
import { Button } from '@/components/ui/button'

// 2. Types/Interfaces
interface Props {
  title: string
}

// 3. Component
export function MyComponent({ title }: Props) {
  // 4. Hooks
  const [state, setState] = useState()
  const { data } = useQuery()

  // 5. Effects
  useEffect(() => {
    // ...
  }, [])

  // 6. Event handlers
  const handleClick = () => {}

  // 7. Render
  return <div onClick={handleClick}>{title}</div>
}
```

### TypeScript

**Use explicit types for props:**
```tsx
interface ButtonProps {
  variant?: 'primary' | 'secondary'
  size?: 'sm' | 'md' | 'lg'
  onClick?: () => void
  children: React.ReactNode
}
```

**Type server responses:**
```tsx
interface AgentResponse {
  id: string
  name: string
  namespace: string
}

const agents: AgentResponse[] = await fetchAgents()
```

---

## General Principles

### DRY (Don't Repeat Yourself)

Extract common patterns:

```go
// Bad - repeated code
func GetAgent() { /* db logic */ }
func GetSession() { /* same db logic */ }

// Good - generic function
func get[T any](db *gorm.DB, id uint) (*T, error) { /* shared logic */ }
```

### Small, Focused Functions

- One responsibility per function
- Max 50-100 lines per function
- Extract complex logic

### Meaningful Names

```go
// Bad
func f(x int) int { return x * 2 }

// Good
func doubleValue(input int) int { return input * 2 }
```

### Consistent Indentation

- **Go**: Tabs (gofmt standard)
- **Python**: 4 spaces (PEP 8)
- **TypeScript**: 2 spaces (Prettier default)

### Remove Dead Code

- Delete unused imports
- Delete commented-out code
- Delete unused functions/variables

### No Backward Compatibility Unless Required

Unless explicitly instructed, assume you don't need to maintain backward compatibility when refactoring.
