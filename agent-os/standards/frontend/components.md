## UI component best practices

### Component Organization

**Directory Structure:**
```
ui/src/components/
├── ui/                      # Radix UI wrapper components (button, input, form, etc.)
├── chat/                    # Chat feature components (ChatInterface, ChatMessage, etc.)
├── sidebars/               # Navigation sidebars
├── create/                 # Agent creation flow
├── models/                 # Model configuration
├── onboarding/             # Onboarding wizard
├── tools/                  # Tool browsing
├── icons/                  # Provider icons
└── [shared].tsx            # Shared components (Header, Footer, etc.)
```

### Component Patterns

**1. Wrapper Components with Context**

Use React Context for component composition (from `ui/src/components/ui/form.tsx`):

```tsx
const FormField = <TFieldValues extends FieldValues>({
  ...props
}: ControllerProps<TFieldValues>) => {
  return (
    <FormFieldContext.Provider value={{ name: props.name }}>
      <Controller {...props} />
    </FormFieldContext.Provider>
  )
}

// Access context in child components
const useFormField = () => {
  const fieldContext = useContext(FormFieldContext)
  const { error, formItemId } = fieldContext
  return { error, formItemId }
}
```

**2. Container/Presentational Split**

From `ui/src/components/chat/ChatInterface.tsx` (lines 31-427):
- **Container**: Manages state, effects, data fetching
- **Presentational**: Pure components receiving props (`ChatMessage`, `StreamingMessage`)

```tsx
// Container component
export function ChatInterface({ agent, session }: Props) {
  const [messages, setMessages] = useState<Message[]>([])
  const [isLoading, setIsLoading] = useState(false)

  // Complex state management and effects...

  return (
    <div>
      {messages.map(msg => (
        <ChatMessage message={msg} key={msg.id} />
      ))}
    </div>
  )
}

// Presentational component
export function ChatMessage({ message }: { message: Message }) {
  return <div>{message.content}</div>
}
```

**3. Composition Pattern**

Build complex components from smaller pieces:

```tsx
<FormField
  control={form.control}
  name="email"
  render={({ field }) => (
    <FormItem>
      <FormLabel>Email</FormLabel>
      <FormControl>
        <Input {...field} />
      </FormControl>
      <FormDescription>Your email address</FormDescription>
      <FormMessage />
    </FormItem>
  )}
/>
```

### Component Conventions

**Naming:**
- PascalCase for components: `ChatInterface`, `AgentCard`, `SessionsSidebar`
- Props interfaces: `{ComponentName}Props` or inline
- Event handlers: `handle{Action}` (e.g., `handleSubmit`, `handleDelete`)

**File Structure:**
```tsx
// Imports
import React from 'react'
import { Button } from '@/components/ui/button'

// Types
interface Props {
  title: string
  onClose: () => void
}

// Component
export function MyComponent({ title, onClose }: Props) {
  // Hooks first
  const [state, setState] = useState()

  // Event handlers
  const handleClick = () => { }

  // Render
  return <div>...</div>
}
```

### State Management

**Local State (useState)**

Use for component-specific UI state:

```tsx
const [isOpen, setIsOpen] = useState(false)
const [currentInput, setCurrentInput] = useState('')
const [errors, setErrors] = useState<Record<string, string>>({})
```

**Context API (Global State)**

From `ui/src/components/AgentsProvider.tsx`:

```tsx
const AgentsContext = createContext<AgentsContextType | undefined>(undefined)

export function useAgents() {
  const context = useContext(AgentsContext)
  if (context === undefined) {
    throw new Error("useAgents must be used within an AgentsProvider")
  }
  return context
}

export function AgentsProvider({ children }: { children: React.ReactNode }) {
  const [agents, setAgents] = useState<Agent[]>([])

  return (
    <AgentsContext.Provider value={{ agents, setAgents }}>
      {children}
    </AgentsContext.Provider>
  )
}
```

**Refs for Non-UI State**

From `ui/src/components/chat/ChatInterface.tsx`:

```tsx
// Use refs for values that don't trigger re-renders
const abortControllerRef = useRef<AbortController | null>(null)
const isFirstChunkRef = useRef(true)
```

### Props Patterns

**Optional Props with Defaults:**
```tsx
interface Props {
  title: string
  variant?: 'primary' | 'secondary'  // Optional
  onClose?: () => void               // Optional callback
}

function Component({ title, variant = 'primary', onClose }: Props) {
  // variant has default value
}
```

**Children Props:**
```tsx
interface Props {
  children: React.ReactNode
}
```

**Event Handler Props:**
```tsx
interface Props {
  onClick: () => void                    // No parameters
  onChange: (value: string) => void      // With parameters
  onSubmit: (data: FormData) => Promise<void>  // Async
}
```

### Effects and Data Fetching

From `ui/src/components/chat/ChatInterface.tsx` (lines 67-120):

```tsx
useEffect(() => {
  async function initializeChat() {
    // Skip if conditions not met
    if (isFirstMessage || isCreatingSessionRef.current) {
      return
    }

    setIsLoading(true)

    try {
      const response = await checkSessionExists(sessionId)
      if (response.error) {
        // Handle error
      }
      const messages = await getSessionTasks(sessionId)
      setMessages(messages)
    } catch (error) {
      toast.error("Error loading messages")
    } finally {
      setIsLoading(false)
    }
  }

  initializeChat()
}, [sessionId, isFirstMessage])  // Dependencies
```

### Radix UI Integration

From `ui/package.json` (lines 18-33):

**Installed Primitives:**
- `@radix-ui/react-dialog` - Modals
- `@radix-ui/react-dropdown-menu` - Dropdowns
- `@radix-ui/react-select` - Select inputs
- `@radix-ui/react-tooltip` - Tooltips
- `@radix-ui/react-tabs` - Tabs
- And many more...

**Wrapper Pattern** (`ui/src/components/ui/button.tsx`):

```tsx
import * as React from "react"
import { Slot } from "@radix-ui/react-slot"

interface ButtonProps {
  asChild?: boolean  // Radix composition pattern
  variant?: "default" | "destructive" | "outline"
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ asChild = false, variant = "default", ...props }, ref) => {
    const Comp = asChild ? Slot : "button"
    return <Comp ref={ref} className={...} {...props} />
  }
)
```

### Component Reusability

**UI Component Library** (`ui/src/components/ui/`):
- Wrap Radix primitives with project styling
- Add Tailwind classes via CVA (Class Variance Authority)
- Export from `ui/` directory for consistent imports

**Feature Components** (`ui/src/components/chat/`, etc.):
- Domain-specific components
- Compose UI components
- Handle business logic

### Error Boundaries

Handle component errors gracefully:

```tsx
// In next.js, use error.tsx files
export default function Error({
  error,
  reset,
}: {
  error: Error
  reset: () => void
}) {
  return (
    <div>
      <h2>Something went wrong!</h2>
      <button onClick={reset}>Try again</button>
    </div>
  )
}
```

### TypeScript Integration

**Component Props:**
```tsx
// Explicit interface
interface Props {
  title: string
  count: number
}

// Or inline type
export function Component({ title, count }: { title: string; count: number }) {
  // ...
}

// Extending HTML elements
interface Props extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: string
}
```

**Generic Components:**
```tsx
function List<T>({ items, renderItem }: {
  items: T[]
  renderItem: (item: T) => React.ReactNode
}) {
  return <div>{items.map(renderItem)}</div>
}
```
