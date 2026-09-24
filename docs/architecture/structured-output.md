# Structured Output

An `AgentTemplate` can declare a JSON Schema for its successful terminal
response. The schema is either written inline in `spec.outputSchema` or stored
as JSON in a same-namespace ConfigMap selected by `spec.outputSchemaFrom`. The
two fields are mutually exclusive.

```yaml
apiVersion: kagent.dev/v1alpha3
kind: AgentTemplate
metadata:
  name: data-extractor
spec:
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
```

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
descriptions, and non-recursive local references through `$defs`.

External or recursive references, conditional schemas, tuple arrays, and other
unsupported keywords are rejected during compilation rather than silently
weakened for a provider. A provider or model can still reject structured output
at runtime; in that case the Task fails.

The serialized schema is limited to 64 KiB. Conversion to the Go ADK schema is
also limited to 32 levels and 1,000 expanded schema nodes; these bounds include
nodes expanded through local `$defs` references. Schemas that exceed any limit
fail compilation.

The semantic resolution code is in
[`go/core/internal/translator/outputschema.go`](../../go/core/internal/translator/outputschema.go).
Runtime validation and A2A conversion are owned by the Go kagent ADK executor.

## References

- [OpenAI Structured Outputs](https://platform.openai.com/docs/guides/structured-outputs)
- [Gemini structured outputs](https://ai.google.dev/gemini-api/docs/structured-output)
- [Claude structured outputs](https://platform.claude.com/docs/en/build-with-claude/structured-outputs)
- [Amazon Bedrock structured outputs](https://docs.aws.amazon.com/bedrock/latest/userguide/structured-output.html)
