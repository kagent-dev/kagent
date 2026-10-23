# Kagent Tools

`kagent-tools` provides shared filesystem, shell, and workspace helpers used by `kagent-adk` and
`kagent-openai`. Import helpers from `kagent.tools`. The package has no runtime
dependencies.

The workspace is `/data/workspace`, shared within an Actor and preserved by
Substrate snapshots. It is created on first use; applications create any
subdirectories they need. Paths follow normal filesystem semantics inside the
sandbox.

ADK uses upstream skill discovery and `SkillToolset`. BYO harnesses, including
the OpenAI adapter, leave skill discovery and loading to user code.
