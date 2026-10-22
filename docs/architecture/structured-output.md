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
descriptions, non-recursive local references through `$defs`, and type lists
such as `type: [string, "null"]`. String schemas can also use `pattern`, in RE2
syntax, and `format`, limited to the formats that both OpenAI and Claude
document.

External or recursive references, conditional schemas, tuple arrays, and other
unsupported keywords are rejected during compilation rather than silently
weakened for a provider. A provider or model can still reject structured output
at runtime; in that case the Task fails.

The schema is enforced in two places. The provider may constrain generation,
and the runtime validates every final answer. Each provider receives the schema
in the form it accepts. OpenAI, Azure OpenAI, Foundry, Claude, and Bedrock get
it unchanged. Gemini and Mistral get a copy converted to Google's schema
format. It keeps `pattern` and the supported formats but drops keywords that
format lacks, such as `const`. Ollama and SAP AI Core get no schema. The final
validator checks the canonical schema, so a provider that ignores a keyword
still cannot complete a Task with a nonconforming answer. The one exception is
`format`, which JSON Schema 2020-12 treats as an annotation. kagent forwards it
to the provider and does not check it on the answer.

Compilation also rejects a schema that would fail every request on a known
provider, so the Agent reports the problem at reconcile time instead of each
Task failing.

Schemas that exceed fixed size and complexity limits fail compilation.

The semantic resolution code is in
[`go/core/internal/translator/outputschema.go`](../../go/core/internal/translator/outputschema.go).
Runtime validation and A2A conversion are owned by the Go kagent ADK executor.

## Strict mode

`outputSchemaStrict` asks OpenAI, Azure OpenAI, and Foundry to constrain
generation to the schema. kagent sends `strict` on every request to those
providers, `false` unless the field is `true`, because the Responses API is
strict when `strict` is omitted. Other providers never receive it, and the final
validator applies in both modes.

Strict mode accepts fewer schemas. Compilation applies its rules for every
provider, so a schema stays valid when its ModelConfig changes provider.

## References

- [OpenAI Structured Outputs](https://platform.openai.com/docs/guides/structured-outputs)
- [Gemini structured outputs](https://ai.google.dev/gemini-api/docs/structured-output)
- [Claude structured outputs](https://platform.claude.com/docs/en/build-with-claude/structured-outputs)
- [Amazon Bedrock structured outputs](https://docs.aws.amazon.com/bedrock/latest/userguide/structured-output.html)
