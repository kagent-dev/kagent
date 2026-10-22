# Structured Output

An agent's template can declare a JSON Schema for its successful terminal
response. The template's `outputSchema` field holds the schema inline;
`outputSchemaFrom` selects JSON from a same-namespace ConfigMap. The two fields
are mutually exclusive. These fields work in both `Agent.spec.template.inline`
and a reusable `AgentTemplate.spec`.

```yaml
apiVersion: api.kagent.dev/v1alpha3
kind: Harness
metadata:
  name: kagent
  namespace: kagent
spec:
  kagent: {}
  substrate:
    workerPoolRef:
      name: kagent-default
    snapshotPolicy:
      location: s3://snapshots/kagent/
---
apiVersion: api.kagent.dev/v1alpha3
kind: Agent
metadata:
  name: data-extractor
  namespace: kagent
spec:
  template:
    inline:
      modelConfig:
        name: default-model-config
      outputSchema:
        type: object
        properties:
          status:
            type: string
          payload:
            type: object
        required: [status, payload]
        additionalProperties: false
  harnessRef:
    name: kagent
```

This Harness uses the release's default Go ADK image. To override it, set
`spec.workload.image` to a full image reference including its `@sha256:` digest.
Choose your snapshot location; the referenced ModelConfig and WorkerPool must
already exist.

The translator resolves ConfigMap references, validates the portable schema
profile, and records a canonical schema and digest in the immutable prepared
revision. Invalid schemas and unsupported Harnesses fail compilation. The
kagent Harness supports structured output in its Go ADK runtime;
Codex, Claude, and BYO Harnesses do not currently support it.

The contract belongs to the public root agent. It is not inherited by local
child agents or agent tools. If a child template is compiled and invoked as a
root agent, its own schema applies to that independent revision.

## A2A response contract

The runtime suppresses partial root-answer fragments, validates the complete
value, and publishes the successful result as one A2A `DataPart` with media
type `application/json`. The part also carries the canonical schema digest in
`kagent.dev/a2a/output-schema-sha256`. The generated Agent Card advertises
`application/json` as its default output mode.

Progress updates, tool events, approval requests, and input-required messages
keep their existing representations. Consequently, a streaming client that
only needs the answer reads the last content-bearing artifact before the Task
reaches `TASK_STATE_COMPLETED`. The gateway persists and relays these events
without interpreting the structured payload.

Structured output has no text fallback. Provider refusal, incomplete output,
invalid JSON, or schema validation failure fails the Task. Invalid model output
is not published or included in errors because it may contain sensitive data.

## Portable schema profile

Schemas must have an object root. The portable profile supports primitive JSON
types, nested objects and arrays, `properties`, `required`,
`additionalProperties`, `items`, `enum`, `const`, `anyOf`, titles and
descriptions, and non-recursive local references through `$defs`. `type` is
one type name or a list of them, such as `type: [string, "null"]`.

Schemas whose `type` is `string`, or a list that includes `string`, can also use
`pattern` and `format`. A `pattern` must be a regular expression that Go's RE2
syntax accepts, so lookaround and backreferences are rejected. Word boundaries
(`\b` and `\B`) are also rejected: Claude documents them as unsupported, and
Bedrock accepts them but produces degenerate output. The Claude API also rejects
a range quantifier on `.` or on a negated class, such as `^.{0,100}$` or
`^[^@]{0,64}$`, and a large range on a wide class, such as `^[a-z]{0,900}$`.
Compilation does not check these. `format` accepts the formats that both OpenAI
and Claude structured outputs document: `date-time`, `time`, `date`, `duration`,
`email`, `hostname`, `ipv4`, `ipv6`, and `uuid`.

```yaml
outputSchema:
  type: object
  properties:
    due:
      type: string
      format: date-time
    ticket:
      type: string
      pattern: "^OPS-[0-9]+$"
  required: [due, ticket]
  additionalProperties: false
```

OpenAI, Azure OpenAI, Claude, Bedrock, and Foundry requests carry both keywords
unchanged. Gemini requests carry `pattern` and only the `date` and `date-time`
formats. Mistral requests carry the same projection as Gemini requests, and
Ollama and SAP AI Core requests carry no schema. The final validator asserts
every `pattern` and `format`, so a provider that ignores either keyword still
cannot complete the Task with a nonconforming value. `date-time` follows RFC
3339 and requires a time zone offset. `time` accepts an optional offset. The
format checks do not limit `email` or `hostname` length, and accept a leap
second (`:60`) at any time of day.

An OpenAI ModelConfig with a custom base URL sends the schema and `strict`
unchanged to that URL. All the checks on this page still apply.

External or recursive references, conditional schemas, tuple arrays, and other
unsupported keywords are rejected during compilation rather than silently
weakened for a provider. A provider or model can still reject structured output
at runtime; in that case the Task fails.

Go ADK checks the answer of some Gemini models with its `set_model_response`
tool. That check needs exactly one type on every property, array item and nested
property, and an `items` schema on every array. A `null` type is allowed only
next to one other type, as in `type: [string, "null"]`. A type list with two or
more non-null types, `anyOf`, `type: "null"` alone, or a node with no type makes
the check reject valid values of that node. Compilation rejects such a schema
when the model name, after the last `/` and in any letter case, starts with
`gemini-`, as in `google/gemini-2.5-flash`, or is empty on Gemini, which then
runs a `gemini-` default. The Agent reports the error instead of the Task
failing. Compilation does not check GeminiVertexAI models.

The serialized schema is limited to 64 KiB. Its patterns and formats together
may compile to at most 100,000 regular expression instructions, about 100
`ipv6` fields or 49 patterns such as `^[0-9]{0,1000}$`. Conversion to the Go ADK
schema is also limited to 32 levels and 1,000 expanded schema nodes; these
bounds include nodes expanded through local `$defs` references. Schemas that
exceed any limit fail compilation.

## Strict mode

`outputSchemaStrict` sets `strict` on Chat Completions requests to OpenAI,
Azure OpenAI, and Foundry in the openai format, and on Responses API requests
to OpenAI and Azure OpenAI:

| `outputSchemaStrict` | Request | Generation |
| --- | --- | --- |
| `false` or omitted | `strict: false` | Not strict |
| `true` | `strict: true` | Strict, if the vendor supports it |

Strict generation means the provider constrains the answer to the schema.
Without it, the schema is guidance. `strict: true` depends on vendor support and
may not be respected. Other providers never receive `strict`, and an
OpenAI-compatible endpoint may ignore it. kagent validates the final answer
against the schema in every case. The Responses API is strict when a request
omits `strict`, so kagent always sends it.

Strict mode accepts fewer schemas. Every schema node must set `type` (`anyOf`,
`enum` or `$ref` may stand in for it), every object must set
`additionalProperties: false` and list every property in `required`, and the
root must not use `anyOf`. An untyped `{"const": "x"}` fails, for example. A
`$ref` must have no other keywords next to it, so put a `description` or
`title` in the referenced definition instead.
Strict mode cannot express a map with free-form keys at all: an object whose
`additionalProperties` is a schema fails with or without `type: object`. List
the keys as properties, or set `outputSchemaStrict: false`. When
`outputSchemaStrict` is `true`, compilation rejects a schema that breaks these
rules, so the Agent reports the error instead of every request failing at the
provider. To make a field optional under strict mode, give it a `null` type,
for example `type: [string, "null"]`. This form also passes the Gemini check
above, which rejects `anyOf`. Claude structured outputs allow at most 16
parameters that use `anyOf` or a type list in one request.

When `outputSchemaStrict` is `false`, compilation does not check these rules.
With `true`, compilation applies them for every provider, so a schema stays
valid when its ModelConfig changes provider.

The semantic resolution code is in
[`go/core/internal/translator/outputschema.go`](../../go/core/internal/translator/outputschema.go).
Runtime validation and A2A conversion are owned by the Go kagent ADK executor.

## References

- [OpenAI Structured Outputs](https://platform.openai.com/docs/guides/structured-outputs)
- [Gemini structured outputs](https://ai.google.dev/gemini-api/docs/structured-output)
- [Claude structured outputs](https://platform.claude.com/docs/en/build-with-claude/structured-outputs)
- [Amazon Bedrock structured outputs](https://docs.aws.amazon.com/bedrock/latest/userguide/structured-output.html)
