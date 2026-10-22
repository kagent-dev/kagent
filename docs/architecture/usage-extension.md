# Token Usage

Kagent reports the tokens a task consumed through a versioned A2A profile
extension. A2A defines no token usage schema, so the extension gives the value a
discoverable, versioned contract instead of an ad hoc metadata key.

The extension URI is:

```text
https://kagent.dev/extensions/usage/v1
```

The kagent ADK runtimes (Go and Python) and the Claude and Codex harnesses
advertise that URI on their Agent Card with `required: false`. BYO agents do not
advertise it.

## Activation

Version 1 is emitted whether or not the client activated it. A client that does
not know the extension ignores one extra metadata key, and consumers that read
the aggregate do not have to change how they call the agent.

A client that reads the payload should activate it with the standard
`A2A-Extensions` header. The Go runtime echoes the URI back when a client
requests it. No kagent client reads the payload yet, so none of them activate
it.

The Python A2A SDK has no server-side activation API, so the Python runtime
does not echo the header. Emission is unaffected.

## Payload

The payload is stored in the metadata of the status update that ends an
execution (`completed`, `input-required`, `failed`, or `canceled`) under the
extension URI key:

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
- **Cancellation:** the `canceled` status update carries the usage counted up
  to the cancel, including the calls the interrupted execution completed. A2A
  makes that update the task's final event and discards the interrupted
  execution's own, so the runtime reads the running execution's usage when it
  handles the cancel request. A task with no running execution, such as one
  waiting for input, repeats the total its `input-required` status carries. A
  call cut off before its final response was never reported by the provider and
  is not counted.

## Claude and Codex harnesses

The Claude and Codex harnesses report the same payload from the shared A2A
executor in `go/harness/runtime/a2a`. It counts every model call the runtime
reports to the executor, which are the calls the runtime's chat spans and token
metrics record. Paused, resumed and canceled executions follow the rules above.

- **Reasoning:** Claude Code and Codex count reasoning tokens inside their output
  tokens. The executor sums the counters as reported and splits the reasoning
  tokens out of the output once, on the stamped payload, so `outputTokens`
  excludes them as it does for the ADK runtimes. A session remainder that names
  reasoning tokens after the output holding them was counted moves them from
  `outputTokens` to `reasoningTokens` instead of counting them twice.
- **Cancel while waiting for input:** the `canceled` status carries the tally the
  execution left when it parked, which is what its `input-required` status
  carries, plus the calls the cancel reaps. A cancel that arrives before that
  status is persisted loses nothing. After a restart, the restored task starts
  from the persisted total.
- **Claude session totals:** each turn runs a Claude Code process that resumes the
  session, and its result reports the session's totals, not the turn's. The
  Claude harness reads the totals the session saved before the turn and reports
  what they grew by beyond the calls it already reported, such as the final
  output of subagent calls. A turn that Claude Code ends at one of its own limits
  (an `error_max_*` result) fails, and its status carries those tokens too. When
  the saved totals cannot be read, the turn counts only the calls it reported.
- **Models:** that remainder names no response model but the model it was
  requested as, the undated name the session totals use, such as
  `claude-sonnet-4-5`. It counts in the one dated entry that name prefixes, such
  as `claude-sonnet-4-5-20250929`, or in an entry of its own under the requested
  name when no dated entry or several of them match. The reasoning tokens it
  identifies are therefore split out of its model's `outputTokens` as well.
- **Cost:** v1 has no cost field. Claude Code's `total_cost_usd` is not reported.

## Out of scope for v1

The per-event `kagent.dev/a2a/usage` metadata key described in
[A2A metadata](a2a-metadata.md) is unchanged and remains outside this extension.
It reports the usage of a single model response and is emitted unconditionally.
Moving it into the extension and gating emission on activation are candidates
for a later version of the URI.

For the base extension mechanism, see the
[A2A extension specification](https://a2a-protocol.org/latest/topics/extensions/).
