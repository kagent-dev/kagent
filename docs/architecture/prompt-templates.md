# Prompt Templates

`AgentTemplate.spec.systemPrompt` may be rendered as a Go `text/template` when
`spec.promptTemplate` is present. A template can interpolate these values:

- `AgentTemplateName`
- `AgentTemplateNamespace`
- `Description`
- `ToolNames`

It can also call `{{include "source/key"}}`. Each source is a same-namespace
ConfigMap named in `spec.promptTemplate.dataSources`; an optional alias replaces
the ConfigMap name in the include path. Included values are inserted as plain text
and are not recursively rendered.

`spec.systemPromptFrom` instead reads the complete prompt from one key in a
same-namespace ConfigMap. It is mutually exclusive with `spec.systemPrompt`.

Compilation resolves ConfigMaps, rejects missing keys and duplicate include
identifiers, renders the prompt, and passes the result to the selected Harness
compiler. The semantic implementation and tests are in
`go/core/v2/translator/template.go`.
