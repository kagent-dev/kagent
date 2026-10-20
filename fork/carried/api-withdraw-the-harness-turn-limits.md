---
id: api-withdraw-the-harness-turn-limits
fork: kagent
theme: compat-migrations
category: feature
status: carried
commits:
  - 'feat(api): Harness.spec.claude.limits — a budget and a turn cap on every turn of a Claude Code agent'
  - 'feat(helm): harnesses[].limits renders Harness.spec.claude.limits'
  - 'feat(controller): count completed turns by the limit that ended them'
  - 'feat(api): withdraw the Harness turn limits from the API, the Claude harness and the chart'
  - 'test(api): apply the upgraded Harness through Client.Apply'
  - 'fix(helm): ignore harnesses[].limits instead of failing the render'
upstream:
  outcome: open
  refs: [giantswarm/kagent-upstream#615, kagent-dev/kagent#2461, kagent-dev/kagent#3020]
  exit: 'squash the net-zero commits; close #3020'
depends_on: [harness-guard-the-turn-usage-on-the, harness-omp-harness-a-byo-adapter-for, harness-the-codex-turns-usage-on-its-final]
verified: 2026-10-20
---
# feat(api): Harness.spec.claude.limits — a budget and a turn cap on every turn of a Claude Code agent

## Change

`feat(api): Harness.spec.claude.limits — a budget and a turn cap on every turn of a Claude Code agent`, `feat(helm): harnesses[].limits renders Harness.spec.claude.limits`, `feat(controller): count completed turns by the limit that ended them`, `feat(api): withdraw the Harness turn limits from the API, the Claude harness and the chart`, `test(api): apply the upgraded Harness through Client.Apply` and `fix(helm): ignore harnesses[].limits instead of failing the render`

## Reason

withdrawn: the platform bounds no Claude Code turn by a budget or a turn cap, so the last commit removes what the first three added. `Harness.spec.claude` is empty again (CRDs regenerated), the claude compiler and process driver pass no `--max-budget-usd` or `--max-turns`, a result Claude Code reports as `error_max_*` is a failure like any other error, no `stopped_by` is written and the controller's `kagent.agent.turns` counter is gone with its telemetry registry entries. The chart's `harnesses[]` ignores `limits`, so a release whose values still set it upgrades, and the key leaves its values afterwards. What stays: the usage a turn spent (prompt, cached and output tokens, `costUsd`, `numTurns`) under `kagent.dev/a2a/usage` on the final task status of a completed or failed turn. A CRD that still declares `limits` turns a chart's later `claude: {}` into null on a server-side apply (giantswarm/kagent-upstream#501): the CRDs upgrade before or with the chart that drops the key, and an envtest proves the apply after the CRD upgrade

## History

none: kagent-dev/kagent#3020 is to be withdrawn by a person; no agent touches it (still open upstream on 2026-10-19). The next re-pin drops the limits of all six commits together, and this file with them, but not the turn usage the first one carries: the usage, which `harness-the-codex-turns-usage-on-its-final.md` and the omp adapter build on, moves into a carried commit of its own at that re-pin (https://github.com/giantswarm/kagent-upstream/issues/615), and `harness-guard-the-turn-usage-on-the.md` fails the re-pin that loses it

kagent-dev/kagent#2461 merged 2026-10-16 (`9097d0d2`) is the usage extension of the kagent ADK runtimes, which Claude and Codex do not emit, so it is related work and the outcome stays open.
