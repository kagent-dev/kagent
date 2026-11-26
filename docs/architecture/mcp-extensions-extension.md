# MCP Extension Values

A client can give the MCP servers an agent calls a value for one request, such
as its own session id, without putting it in the prompt. The value travels in a
versioned A2A extension and reaches only the MCP servers that declared the
matching MCP extension.

The extension URI is:

```text
https://kagent.dev/extensions/mcp-extensions/v1
```

The Go ADK runtime advertises that URI on its Agent Card with
`required: false`. Other runtimes do not implement it yet.

## Activation

The values are read only when the client activates the extension with the
standard `A2A-Extensions` header. The runtime echoes the URI when it activated
it. Without activation the entry is ignored and no MCP server receives a value.

The client sends the values on every request that needs them, including
follow-up and approval messages. Nothing is stored: a request without values
carries none to its tool calls, whatever an earlier request sent.

## Payload

The payload is stored in the request's `params.metadata` under the extension
URI: a JSON object keyed by MCP extension id. Values are opaque JSON.

```json
{
  "metadata": {
    "https://kagent.dev/extensions/mcp-extensions/v1": {
      "com.example/session": "a1b2c3",
      "com.example/export": { "format": "csv" }
    }
  }
}
```

A key must be an MCP extension id (`{vendor-prefix}/{extension-name}`, MCP
SEP-2133): a `_meta` key whose prefix is mandatory and not reserved by MCP. A
prefix whose second label is `modelcontextprotocol` or `mcp` is reserved. A
request whose entry is not an object, or has a key that is not an MCP extension
id, is refused with invalid params before anything runs.

## Delivery to MCP servers

On each `tools/call`, the runtime sets `params._meta[<id>]` to the value of
`<id>` only when the MCP server declared `<id>` in `capabilities.extensions`,
in its `initialize` result or, on MCP 2026-07-28, its `server/discover` result.
A server that did not declare an extension never receives its value. A `_meta`
key the call already carries is not overwritten.

The value is sent as the client gave it. The MCP server validates it: the
platform neither authenticates it nor authorizes anything with it.

A tool call that waits for approval runs with the values of the request that
carries the approval.

## Out of scope for v1

- Calls to other agents over A2A carry no values.
- The runtime does not declare the ids in its own client
  `capabilities.extensions`; the ids are known per request, after the MCP
  session started.

For the base mechanisms, see the
[A2A extension specification](https://a2a-protocol.org/latest/topics/extensions/)
and [MCP SEP-2133](https://modelcontextprotocol.io/seps/2133-extensions).
