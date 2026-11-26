# MCP request metadata

Kagent's Go ADK implements an optional, project-defined A2A bridge identified by
`https://kagent.dev/extensions/mcp-request-metadata/v1`. It forwards explicitly
supplied metadata into MCP tool calls. It defines no MCP extension, capability,
carrier object or application payload schema.

## Declaration and activation

The Agent Card declares the bridge with `required: false`. The caller activates
it for each request with the standard `A2A-Extensions` header. The runtime echoes
the activated URI. Without activation, the envelope is ignored.

The Go ADK implements the bridge. Other runtimes must implement it explicitly
before declaring support. Public discovery declares support only for the
release Go ADK image without workload command or argument overrides. Custom
runtimes declare their own support; the controller does not infer it.

## A2A envelope and MCP delivery

The caller places an object under the bridge URI in `params.metadata`:

```json
{
  "metadata": {
    "https://kagent.dev/extensions/mcp-request-metadata/v1": {
      "com.example/session": "a1b2c3",
      "org.example/options": {"format": "csv", "any application field": true}
    }
  }
}
```

The runtime adds those entries directly to each MCP `tools/call` request:

```json
{
  "_meta": {
    "com.example/session": "a1b2c3",
    "org.example/options": {"format": "csv", "any application field": true}
  }
}
```

There is no additional wrapper or identifier translation. MCP's SDK supplies
its own protocol fields as usual. Every MCP server called during that A2A
request can receive the full object, without a special server capability.
There are no accepted-key settings, per-server policies or application field
filters. Callers must not assume that an entry is delivered to only one server.

Only `tools/call` carries the entries. Initialization, `server/discover`, list
requests and other MCP methods never carry the envelope's values. This holds
on both MCP 2025-11-25 and 2026-07-28. Other A2A metadata and calls to other
agents do not contribute entries.

Missing, null and empty envelopes contribute no values. An activated envelope
of another type is refused with A2A invalid params before dispatch. Each
request supplies its own values, including follow-up and approval messages.
Nothing is retained in ADK session state; an approval uses that request's
values and a request without values inherits none.

## Validation and collisions

Top-level names follow MCP's `_meta` key grammar, including its optional prefix
and possibly empty name. Nested objects and their field names are opaque.
Malformed top-level names are refused, not silently removed.

The transport owns `progressToken`, `traceparent`, `tracestate`, `baggage` and
the MCP core fields `io.modelcontextprotocol/protocolVersion`,
`io.modelcontextprotocol/clientInfo`, `io.modelcontextprotocol/clientCapabilities`,
`io.modelcontextprotocol/logLevel`, `io.modelcontextprotocol/serverInfo` and
`io.modelcontextprotocol/subscriptionId`. Supplying any of these in the
envelope is refused before dispatch. For other collisions, existing outgoing
metadata takes precedence; nested objects are not merged.

## Extension support

An entry whose name is an extension identifier does not declare client support
for that extension. This bridge does not add capability declarations based on
metadata, negotiate arbitrary extensions or implement their behavior. An
extension requiring additional client behavior must be implemented and
negotiated separately. Servers must not infer capabilities from metadata.

## Trust and compatibility

Values are untrusted application data. They grant no authority and must not
contain access tokens, refresh tokens, API keys or other credentials. Servers
validate their meaning and independently authenticate and authorize requests.
HTTP credentials use the transport's existing Authorization header; stdio
credentials come from the environment. The bridge does not alter either.
Values are attached after tool selection, outside the model's arguments. A
server can still return them in a tool result, so they are not guaranteed to
remain invisible to the model.

This proposal's specification is maintained in this document. Publication at
the identifier's URI requires the website owner's action; the identifier does
not imply that page is live. Incompatible changes require a new A2A URI.

Sources: [A2A extensions](https://a2a-protocol.org/v1.0.1/topics/extensions/),
[MCP metadata](https://modelcontextprotocol.io/specification/2026-07-28/basic#_meta),
[MCP extension negotiation](https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#extension-negotiation)
and [MCP authorization](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization).
