# Public A2A transports

The core gateway serves A2A over gRPC and HTTP/JSON-RPC on the controller's API
listener (port 8083 by default). Both transports use the same AgentInstance
authorization, durable tasks, history, and runtime lifecycle.

Each Agent has a discoverable HTTP endpoint:

| Operation | Path |
| --- | --- |
| Read the Agent Card | `GET /agents/{namespace}/{name}/.well-known/agent-card.json` |
| Call JSON-RPC, including streaming | `POST /agents/{namespace}/{name}` |

The URL selects the Agent. gRPC requests use the standard A2A `tenant` field
(`namespace/name`), also advertised on the card's interfaces. A nonempty HTTP
request tenant must match its URL. The message `contextId` is the AgentInstance ID;
omitting both context and task IDs creates a new conversation. Retrying that first
message with the same message ID reuses the conversation. Task-only requests resolve
the conversation from the globally unique task ID.
There is no deployment-wide Agent Card because the gateway serves multiple agents.

Cards and JSON-RPC calls require the same authentication as the core API. A
validated `X-Share-Token` supplements the authenticated user's access to its
instance: read-only shares permit card/task reads and subscriptions; read-write
shares also permit messages and cancellation. Cards are not publicly cached.

The card comes from the Agent's latest successful revision (or the shared
instance's pinned revision) and advertises
JSON-RPC first, followed by gRPC. It retains runtime extensions while reporting
the gateway's streaming capabilities. JSON-RPC uses the pinned upstream A2A v1
SDK and supports `SendMessage`, `SendStreamingMessage`, `GetTask`, `ListTasks`,
`CancelTask`, `SubscribeToTask`, and `GetExtendedAgentCard`. These are the v1
method names for the operations older clients called `message/send`,
`message/stream`, `tasks/get`, `tasks/list`, `tasks/cancel`, and
`tasks/resubscribe`.

Set `controller.a2aGatewayUrl` in Helm (the controller's `KAGENT_GATEWAY_URL`) to
the externally reachable gateway base URL when exposing agents outside the
cluster. Its default is the controller's cluster Service URL. HTTP interfaces
append `/agents/{namespace}/{name}` to this base, preserving any deployment prefix.
An ingress using a prefix must strip that prefix before forwarding to the core
listener. Forward streaming responses without buffering.

For example, with the default unsecure authentication mode:

```sh
curl -H 'X-User-Id: alice' \
  'https://kagent.example/agents/kagent/assistant/.well-known/agent-card.json'
```

Use the returned JSON-RPC interface URL with an A2A v1 client, supplying the
credentials required by the deployment on discovery and subsequent requests.

For example, the initial JSON-RPC send body is:

```json
{
  "jsonrpc": "2.0",
  "id": "request-1",
  "method": "SendMessage",
  "params": {
    "message": {
      "messageId": "message-1",
      "role": "ROLE_USER",
      "parts": [{"text": "Hello"}]
    }
  }
}
```

Send it to `/agents/kagent/assistant`. Save the returned task's `contextId` for
subsequent messages. The Go client exposes `A2A().ForAgent(ctx, agentRef)`;
`ForAgentInstance(ctx, id)` resolves the Agent and supplies that instance's context.
