# HTTP Server API — Session & Agent Invocation

## Key Endpoints

### Session Creation
- **POST `/api/sessions`**
- Body: `{ "agent_ref": "namespace/agent-name", "name": "optional-name" }`
- Returns: `Session { id, name, user_id, agent_id, created_at }`
- Status: 201 Created

### Agent Invocation (A2A Protocol)
- **POST `/api/a2a/{namespace}/{name}`**
- JSON-RPC 2.0 with SSE streaming
- Body:
```json
{
  "jsonrpc": "2.0",
  "method": "message/stream",
  "params": {
    "message": {
      "kind": "message",
      "role": "user",
      "parts": [{"kind": "text", "text": "prompt here"}],
      "contextID": "session-id"
    }
  },
  "id": "unique-request-id"
}
```

### Auth
- `user_id` query param or `X-User-Id` header
- Default: `admin@kagent.dev`

## Existing CRUD Patterns
- Agents: GET/POST `/api/agents`, GET/PUT/DELETE `/api/agents/{ns}/{name}`
- Sessions: GET/POST `/api/sessions`, GET/PUT/DELETE `/api/sessions/{id}`
- Tasks: POST `/api/tasks`, GET/DELETE `/api/tasks/{id}`

## For AgentCronJob Controller
1. Create session: POST `/api/sessions` with agent_ref
2. Send prompt: POST `/api/a2a/{ns}/{name}` with contextID = session ID
3. Track session ID in CRD status
