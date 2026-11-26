# Request-scoped MCP metadata

The Go ADK forwards application entries from A2A SendMessage and
SendStreamingMessage request-level `metadata` directly into MCP
`tools/call` request `_meta`. This is implementation behavior using existing
protocol fields. It requires no custom A2A or MCP extension, URI, activation
header, capability declaration or wrapper object.

For example, request-level metadata:

```json
{"metadata":{"com.example/session":"session-123","format":"csv"}}
```

becomes tool-call metadata:

```json
{"_meta":{"com.example/session":"session-123","format":"csv"}}
```

Every called server can receive the complete application metadata, without
application-key policies or destination declaration checks. Applications and
servers own the meaning and validation of values. Forwarding metadata does not
implement application semantics or negotiate arbitrary MCP extensions.

Only `tools/call` receives the application entries. Initialize, discovery and
list requests receive none. The values are held in request context, not model
prompts, tool arguments or conversation state. Each follow-up or approval
request supplies its own values; an absent or empty map inherits nothing.
Servers can still expose values to the model by echoing them in tool results.

Top-level names follow MCP's `_meta` key syntax; nested values remain opaque.
Caller-supplied `progressToken`, `traceparent`, `tracestate`, `baggage`, and
`io.modelcontextprotocol/{protocolVersion,clientInfo,clientCapabilities,logLevel,serverInfo,subscriptionId}`
are refused before execution because the transport owns them. Existing outgoing
metadata wins other collisions, without merging nested objects.

Message and part metadata keep their existing meaning and are not forwarded as
request metadata. Authentication and authorization remain on their existing
transports. Metadata is untrusted application data, never credentials.

The existing protocol fields are defined by [A2A metadata](https://a2a-protocol.org/v1.0.1/specification/#325-metadata)
and [MCP `_meta`](https://modelcontextprotocol.io/specification/2026-07-28/basic#_meta).
Their specifications do not prescribe this cross-protocol mapping.
