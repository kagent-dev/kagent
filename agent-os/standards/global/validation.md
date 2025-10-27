## Validation best practices

### Go Validation

#### RFC 1123 DNS Subdomain Validation

Agent and resource names follow Kubernetes naming rules:

```go
func isResourceNameValid(name string) bool {
    // RFC 1123 subdomain: lowercase alphanumeric, '-', '.'
    // Must start and end with alphanumeric
    pattern := `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
    matched, _ := regexp.MatchString(pattern, name)
    return matched && len(name) <= 253
}
```

#### Input Validation in Handlers

```go
func (h *Handler) HandleCreateAgent(w http.ResponseWriter, r *http.Request) {
    var req CreateAgentRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        handleError(w, errors.NewBadRequest("Invalid JSON"))
        return
    }

    // Validate required fields
    if req.Name == "" {
        handleError(w, errors.NewBadRequest("Name is required"))
        return
    }

    if !isResourceNameValid(req.Name) {
        handleError(w, errors.NewBadRequest("Invalid name format"))
        return
    }

    // Continue with validated data
}
```

#### GORM Model Constraints

```go
type Agent struct {
    gorm.Model
    Name      string `gorm:"not null"`  // Database-level validation
    Namespace string `gorm:"not null"`
    UserID    string `gorm:"not null;index"`
}
```

---

### Python Validation

#### Pydantic BaseModel

From `python/packages/kagent-adk/src/kagent/adk/types.py`:

```python
from pydantic import BaseModel, Field, validator

class AgentConfig(BaseModel):
    name: str
    model: str
    system_prompt: str | None = None

    @validator('name')
    def name_must_not_be_empty(cls, v):
        if not v or not str(v).strip():
            raise ValueError("Agent name must be a non-empty string")
        return v

    @validator('model')
    def model_must_not_be_empty(cls, v):
        if not v or not str(v).strip():
            raise ValueError("Model name must be a non-empty string")
        return v
```

#### Discriminated Unions

```python
from typing import Union, Literal
from pydantic import BaseModel, Field

class OpenAI(BaseModel):
    type: Literal["openai"] = "openai"
    model: str
    api_key: str | None = None

class Anthropic(BaseModel):
    type: Literal["anthropic"] = "anthropic"
    model: str
    api_key: str | None = None

class LLMConfig(BaseModel):
    model: Union[OpenAI, Anthropic] = Field(discriminator="type")
```

#### Environment Variable Validation

From `python/packages/kagent-core/src/kagent/core/_config.py`:

```python
def __init__(self):
    kagent_url = os.getenv("KAGENT_URL")
    if not kagent_url:
        raise ValueError("KAGENT_URL environment variable is not set")

    kagent_name = os.getenv("KAGENT_NAME")
    if not kagent_name:
        raise ValueError("KAGENT_NAME environment variable is not set")
```

#### Data Part Validation

```python
def validate_message(context: RequestContext):
    """Validate A2A request has required data."""
    if not context.message:
        raise ValueError("A2A request must have a message")

    if not context.message.parts:
        raise ValueError("Message must have at least one part")
```

#### Config Extraction with Defaults

From `python/packages/kagent-langgraph/src/kagent/langgraph/_checkpointer.py`:

```python
configurable = config.get("configurable", {})

thread_id = configurable.get("thread_id")
if not thread_id:
    raise ValueError("thread_id is required in config.configurable")

user_id = configurable.get("user_id", "admin@kagent.dev")  # Default
checkpoint_ns = configurable.get("checkpoint_ns", "")      # Default
```

---

### TypeScript/React Validation

#### Zod Schema Validation

From `ui/package.json` (line 53) - uses Zod for runtime validation:

```tsx
import { z } from 'zod'

const agentSchema = z.object({
  name: z.string()
    .min(1, "Name is required")
    .regex(/^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/, "Invalid name format"),
  namespace: z.string().min(1, "Namespace is required"),
  modelName: z.string().min(1, "Model is required"),
  systemPrompt: z.string().optional(),
})

type AgentFormData = z.infer<typeof agentSchema>
```

#### React Hook Form + Zod Integration

```tsx
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'

const form = useForm<AgentFormData>({
  resolver: zodResolver(agentSchema),
  defaultValues: {
    name: '',
    namespace: 'default',
    modelName: '',
  },
})

const onSubmit = form.handleSubmit(async (data) => {
  // data is type-safe and validated
  const result = await createAgent(data)
})
```

#### Manual Validation Function

From `ui/src/components/AgentsProvider.tsx`:

```tsx
const validateAgentData = (data: Partial<AgentFormData>): ValidationErrors => {
  const errors: ValidationErrors = {}

  // Required field validation
  if (data.name !== undefined && !data.name.trim()) {
    errors.name = "Agent name is required"
  }

  // Format validation
  if (data.name !== undefined && !isResourceNameValid(data.name)) {
    errors.name = "Agent name can only contain lowercase alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"
  }

  // Conditional validation based on type
  const type = data.type || "Declarative"
  if (type === "Declarative") {
    if (data.systemPrompt !== undefined && !data.systemPrompt.trim()) {
      errors.systemPrompt = "Agent instructions are required"
    }
    if (!data.modelName || data.modelName.trim() === "") {
      errors.model = "Please select a model"
    }
  }

  return errors
}
```

#### Field-Level Validation

From `ui/src/components/models/new/BasicInfoSection.tsx`:

```tsx
<Input
  value={name}
  onChange={(e) => {
    const newName = e.target.value
    onNameChange(newName)

    // Real-time validation
    if (!newName.trim()) {
      setErrors({ ...errors, name: "Name is required" })
    } else if (!isResourceNameValid(newName)) {
      setErrors({ ...errors, name: "Invalid name format" })
    } else {
      const { name, ...rest } = errors
      setErrors(rest)  // Clear error
    }
  }}
  className={errors.name ? "border-destructive" : ""}
  aria-invalid={!!errors.name}
/>
```

---

## Validation Principles

### Validate on Multiple Layers

1. **Client-Side (UX)**: Immediate feedback for users
2. **API Layer**: Server-side validation before processing
3. **Database Layer**: Constraints (NOT NULL, UNIQUE, CHECK)

### Server-Side is Required

- **Never trust client input**
- Always validate on the server even if validated on client
- Client validation is for UX only, not security

### Allowlists Over Blocklists

```go
// Good - allowlist
if !isResourceNameValid(name) {
    return errors.NewBadRequest("Invalid name")
}

// Bad - blocklist
if strings.Contains(name, "..") || strings.Contains(name, "/") {
    // Easy to bypass, incomplete
}
```

### Specific Error Messages

```python
# Good - specific and actionable
if not name:
    raise ValueError("Agent name is required")
if len(name) > 253:
    raise ValueError("Agent name must be 253 characters or less")

# Bad - vague
if invalid_name(name):
    raise ValueError("Invalid name")
```

### Type and Format Validation

- Check data types match expected types
- Validate formats (emails, URLs, RFC 1123 names)
- Validate ranges (min/max length, numeric bounds)
- Check required fields are present

### Sanitize Input

- Escape HTML/SQL in user input
- Use parameterized queries (GORM handles this)
- Validate file uploads
- Check MIME types

### Business Rule Validation

Validate business logic constraints:

```python
# Check sufficient balance
if user.balance < transaction.amount:
    raise ValueError("Insufficient balance")

# Check valid date ranges
if end_date < start_date:
    raise ValueError("End date must be after start date")
```

### Consistent Validation

Apply validation at all entry points:
- Web forms
- API endpoints
- Background jobs
- CLI commands
