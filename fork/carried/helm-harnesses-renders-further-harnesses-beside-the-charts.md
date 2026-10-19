---
id: helm-harnesses-renders-further-harnesses-beside-the-charts
fork: kagent
theme: harness-variety
category: gs-specific
status: carried
upstream:
  outcome: declined
  refs: [kagent-dev/kagent#2529, kagent-dev/kagent#2536]
  exit: move to agent-platform
depends_on: [helm-controller-agentimage-digest-and-an-optional-default]
commit: a09b1a021
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `feat(helm): harnesses renders further Harnesses beside the chart's own` (on `api.kagent.dev/v1alpha3`: an entry renders no admission selector and no egress; an `Agent` names its Harness in `harnessRef` and declares its egress itself) | the platform ships coding agents as AgentTemplates admitted by a `claude` Harness, one per toolchain image, and the chart rendered one Harness only, the Go ADK's; the lab applied the Claude one by hand. `harnesses[]` (empty by default) renders one Harness per entry with the policy shape of `harness.*` plus `runtime: claude\|kagent`; a claude entry defaults to the chart's `claude-harness` image at its published digest and to the selector `kagent.dev/harness: <name>`; names must be distinct from the chart's own Harness, and overlapping matchLabels selectors fail the render (an AgentTemplate matched by two Harnesses runs under both); each entry's image joins the pre-pull ConfigMap; the selector helper is shared. Transitional pairing: the selectors go when the line moves to upstream's `Agent.harnessRef` | fork-only with the default-Harness row: kagent-dev/kagent#2529 closed NOT_PLANNED 2026-09-27 and kagent-dev/kagent#2536 closed unmerged, so the branch `upstream/helm-default-harness` is not sent (see `helm-controller-agentimage-digest-and-an-optional-default.md`) |
