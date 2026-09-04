# Prompt Resolution

An `AgentTemplate` can provide its system prompt in one of three forms:

- literal `spec.systemPrompt`;
- a complete value selected by `spec.systemPromptFrom`; or
- `spec.systemPrompt` rendered as a Go `text/template` when
  `spec.promptTemplate` is configured.

The mutually exclusive forms are enforced by the API schema.

Prompt templates can reference:

- `AgentTemplateName`
- `AgentTemplateNamespace`
- `Description`
- `ToolNames`

They can also call `{{include "source/key"}}`. Each source is a same-namespace
ConfigMap named by `spec.promptTemplate.dataSources`; an optional alias replaces
the ConfigMap name in the include path. Included values are inserted as text and
are not recursively rendered.

Resolution happens before harness compilation. Missing ConfigMaps or keys,
duplicate include identifiers, and invalid templates fail the prepared revision
rather than producing a partially configured runtime. The semantic implementation
and focused tests live in
[`go/core/v2/translator/template.go`](../../go/core/v2/translator/template.go).
