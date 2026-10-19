---
id: harness-omp-harness-a-byo-adapter-for
fork: kagent
theme: harness-variety
category: gs-specific
status: carried
commits:
  - 'feat(harness): omp-harness, a BYO adapter for oh-my-pi'
  - 'fix(harness/omp): report the turn''s cost through the optional usage cost'
upstream:
  outcome: fork-only
  refs: []
  exit: move out of tree as a BYO image in agent-platform
depends_on: []
verified: 2026-10-19
---
# feat(harness): omp-harness, a BYO adapter for oh-my-pi

## Change

`feat(harness): omp-harness, a BYO adapter for oh-my-pi` and `fix(harness/omp): report the turn's cost through the optional usage cost`

## Reason

the platform offers oh-my-pi (omp) next to claude and codex, and kagent has no omp runtime; `go/harness/omp` is a BYO Harness image on kagent's public runtime packages (the private A2A executor, the continuation, `/readyz`, the TaskStore) that drives `omp --mode rpc` per turn, takes claude's Anthropic-protocol ModelConfig, reports each turn's usage, resumes from omp's session under `/data`, serves a binding's selected MCP tools through a loopback bridge, and pins omp's isolation so a repository's omp, `.claude` or `.codex` configuration adds no tool, server or provider

## History

fork-only: a BYO adapter of the platform's, a pure addition under `go/harness/omp` that a re-pin never conflicts with; an upstream omp runtime would be an API change the platform does not ask for
