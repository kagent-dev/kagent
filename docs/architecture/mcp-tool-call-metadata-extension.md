# MCP Tool-Call Metadata

Kagent defines an optional A2A extension and a corresponding MCP extension for
request-scoped tool-call metadata. These are project-defined contracts built on
the protocols' extension mechanisms. They do not implement arbitrary MCP
extensions or authenticate the values they carry.

| Protocol | Identifier | Purpose |
| --- | --- | --- |
| A2A | `https://kagent.dev/extensions/mcp-tool-call-metadata/v1` | Caller supplies metadata for one request |
| MCP | `dev.kagent/tool-call-metadata` | Client and server opt into an opaque tool-call metadata payload |

The Go ADK implements this contract. Python, Codex, Claude and BYO runtimes do
not implement it unless their runtime explicitly provides the same behavior.

Managed public cards advertise it for the release-default kagent workload.
The configured default kagent image must preserve the release's Go ADK
contract. An explicit image, command or argument override has no declared
capability provenance, so its public card conservatively omits this feature.
A standalone Go ADK runtime advertises its actual support on its own card.
General discovery for replacement workloads requires an explicit runtime
capability contract rather than guessing from an image or executable name.

## A2A activation and payload

The Agent Card declares the A2A extension with `required: false`. A caller
activates it through the standard `A2A-Extensions` header. The runtime echoes
the activated URI; without activation, it ignores the payload.

Values belong in `params.metadata[<A2A extension URI>]`:

```json
{
  "metadata": {
    "https://kagent.dev/extensions/mcp-tool-call-metadata/v1": {
      "com.example/session": "a1b2c3",
      "com.example/export": { "format": "csv" }
    }
  }
}
```

The payload is an opaque JSON object. Its field names and values belong to the
application; the runtime does not validate individual names or filter fields.
Namespaced field names can help applications avoid collisions. These fields
are not declarations of support for extensions with matching identifiers.

A missing payload, JSON null or an empty object contributes no values. An
activated payload of another type causes an invalid
params error before runtime dispatch. Unrecognized surrounding A2A metadata
does not become tool-call metadata.

Each request supplies its own values, including follow-up and approval
messages. Nothing is retained as ADK session state. A request without values
sends none; an approved tool call uses the approval request's values. Calls
to other agents over A2A do not forward these values.

## MCP capability negotiation

The client advertises this fixed capability, including when no values are
attached to the current request:

```json
{
  "extensions": {
    "dev.kagent/tool-call-metadata": {}
  }
}
```

Each participating server advertises the same capability with an empty settings
object:

```json
{
  "extensions": {
    "dev.kagent/tool-call-metadata": {}
  }
}
```

The extension defines no settings. An absent declaration or settings that are
not an object disable delivery to that server. Unknown settings fields are
ignored. A server advertising another extension with the same identifier as
one of the payload fields does not opt into this contract.

On MCP 2025-11-25 the client and server advertise these capabilities during
`initialize`. On MCP 2026-07-28 the client advertises its capability through
`_meta["io.modelcontextprotocol/clientCapabilities"]` on requests, and the
runtime reads the server's declaration from `server/discover`. The fixed
client declaration does not depend on the current request's payload.

A proxy can relay the extension declaration and payload without interpreting
the payload's fields. Servers reached through such a proxy receive the complete
payload when they declare support. No per-field capability aggregation or
filtering is required.

## MCP delivery and trust

Only `tools/call` requests carry the values. Initialization, `server/discover`,
list requests and every other MCP request never do, so a cached list result
never holds one request's values.

For every `tools/call` to a server declaring support, the runtime sends the
current request's complete payload under the MCP extension identifier:

```json
{
  "_meta": {
    "dev.kagent/tool-call-metadata": {
      "com.example/session": "a1b2c3",
      "com.example/export": { "format": "csv" }
    }
  }
}
```

No extension metadata is added when the payload is empty. Existing
`_meta["dev.kagent/tool-call-metadata"]` takes precedence and is not merged or
overwritten. Other call metadata, including SDK-managed protocol fields, is
preserved. Metadata is attached after the model selects the tool, outside
model arguments. A server can still return a value in a tool result, so this
is not a guarantee that a value can never reach the model.

Capability declarations express protocol support, not authorization to receive
confidential data. Every participating server called during a request can
receive its complete payload. Servers must treat values as untrusted application
data and independently authenticate and authorize their requests.

Values are not credentials. A caller must not put access tokens, refresh
tokens, API keys or other secrets in them, and a server must not treat these
values as credentials. For HTTP transports using MCP authorization, access
tokens belong in the `Authorization` header, and the server must validate
that they were issued for its resources. Stdio servers should obtain
credentials from the environment, as the
[MCP authorization specification](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization)
recommends. A server must not accept or forward tokens issued for another
resource; see the
[MCP token passthrough guidance](https://modelcontextprotocol.io/docs/2026-07-28/tutorials/security/security_best_practices#token-passthrough).

Breaking changes to the A2A contract require a new URI; breaking changes to
the MCP extension require a new extension identifier. Unknown settings fields
are ignored. The specification is maintained in this document; publication
at the A2A extension URI belongs to the kagent website.

Sources: [A2A extensions](https://a2a-protocol.org/latest/topics/extensions/),
[MCP extension negotiation](https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#extension-negotiation),
and [MCP metadata](https://modelcontextprotocol.io/specification/2026-07-28/basic#_meta).
