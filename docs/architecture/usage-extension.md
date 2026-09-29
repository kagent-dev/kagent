# Token Usage

Kagent reports the tokens a task consumed through a versioned A2A profile
extension. A2A defines no token usage schema, so the extension gives the value a
discoverable, versioned contract instead of an ad hoc metadata key.

The extension URI is:

```text
https://kagent.dev/extensions/usage/v1
```

The kagent ADK runtimes (Go and Python) advertise that URI on their Agent Card
with `required: false`. Other harnesses do not emit usage and do not advertise
it.

## Activation

Version 1 is emitted whether or not the client activated it. A client that does
not know the extension ignores one extra metadata key, and consumers that read
the aggregate do not have to change how they call the agent.

Clients should still activate it with the standard `A2A-Extensions` header. The
Go runtime echoes the URI back when a client requests it. The kagent UI, the MCP
invocation tool, and the Go and Python remote A2A tools always send it. A later
version that only emits usage for clients that activated it is then a no-op for
kagent's own clients.

The Python A2A SDK has no server-side activation API, so the Python runtime
does not echo the header. Emission is unaffected.

## Payload

The payload is stored in the metadata of the status update that ends an
execution (`completed`, `input-required`, or `failed`) under the extension URI
key:

```json
{
  "inputTokens": 1300,
  "outputTokens": 210,
  "reasoningTokens": 40,
  "cachedInputTokens": 800,
  "totalTokens": 1550,
  "models": [
    {
      "model": "gpt-4o-2024-11-20",
      "inputTokens": 1000,
      "outputTokens": 150,
      "reasoningTokens": 40,
      "cachedInputTokens": 800,
      "totalTokens": 1190
    },
    {
      "model": "claude-sonnet-4",
      "inputTokens": 300,
      "outputTokens": 60,
      "reasoningTokens": 0,
      "cachedInputTokens": 0,
      "totalTokens": 360
    }
  ]
}
```

| Field | Meaning |
| --- | --- |
| `inputTokens` | Prompt tokens sent to the model |
| `outputTokens` | Generated tokens, excluding reasoning tokens |
| `reasoningTokens` | Reasoning (thinking) tokens, when the provider reports them |
| `cachedInputTokens` | Input tokens served from the provider's cache; a subset of `inputTokens` |
| `totalTokens` | The provider-reported total, or `inputTokens + outputTokens + reasoningTokens` for a call that reports none |
| `models` | The same counts per model, ordered by model name |

Each LLM call is counted once, on its final non-partial response. Because
`totalTokens` is the provider's own figure when there is one, it can exceed the
sum of the other counts. A call whose response names no model is counted in the
top-level totals only, so the per-model entries can add up to less than the
totals. The key is omitted when no call reported usage.

## Semantics

The value covers the whole task lifetime, not the last execution. A task that
pauses for input and resumes, or receives a follow-up message, continues from
the total persisted on the stored task. Consumers replace any earlier value with
the latest one; they never sum values across status updates.

- **Resume:** the runtime reads the persisted payload back from the stored task,
  so the aggregate is computed whether or not the client activated the
  extension.
- **Failure:** a failed status update carries the usage consumed before the
  failure.
- **Cancellation:** the `canceled` status update produced by a cancel request
  carries no usage. The stored task keeps the last value it received, so tokens
  spent by the canceled execution are not reported.

## Out of scope for v1

The per-event `kagent.dev/a2a/usage` metadata key described in
[A2A metadata](a2a-metadata.md) is unchanged and remains outside this extension.
It reports the usage of a single model response and is emitted unconditionally.
Moving it into the extension and gating emission on activation are candidates
for a later version of the URI.

For the base extension mechanism, see the
[A2A extension specification](https://a2a-protocol.org/latest/topics/extensions/).
