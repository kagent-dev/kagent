---
id: helm-a-harnesses-entry-runs-the-codex-runtime
fork: kagent
theme: harness-variety
category: gs-specific
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: move harnesses[] rendering to agent-platform
depends_on: [helm-harnesses-renders-further-harnesses-beside-the-charts]
commit: fa37990ea
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `feat(helm): a harnesses[] entry runs the codex runtime on runtimeImages.codexHarness` | the platform offers codex next to claude as a coding Harness an installation opts into with values, and the chart's `harnesses[]` failed the render on `runtime: codex`; an entry renders `spec.codex: {}` on its own image or on `runtimeImages.codexHarness` at the digest the publish pipeline stamps, and the runtime-images ConfigMap lists that image through the entry, so Substrate pre-pulls it only where an entry runs it | fork-only: extends the fork's `harnesses[]` (row `feat(helm): harnesses renders further Harnesses beside the chart's own`), which upstream declined |
