---
id: harness-keep-the-compiled-skills-on-claude-turns
fork: kagent
theme: harness-variety
category: bugfix
status: carried
commits:
  - 'fix(harness): keep the compiled skills on Claude turns that need approval'
upstream:
  outcome: superseded
  refs: [kagent-dev/kagent#3203, giantswarm/kagent-upstream#685, kagent-dev/kagent#3191]
  exit: drops at next re-pin
depends_on: []
verified: 2026-10-19
---
# fix(harness): keep the compiled skills on Claude turns that need approval

## Change

`fix(harness): keep the compiled skills on Claude turns that need approval`

## Reason

Claude Code counts skills under an `--add-dir` directory as part of the project setting source, so the `--setting-sources ""` the driver passed on approval turns dropped every compiled skill (Claude Code 2.1.260); the driver now passes only `--settings` and `--permission-prompt-tool`, and the rendered `ask` rules still hold over user and project `allow` rules

## History

superseded by kagent-dev/kagent#3203 (merged 2026-10-16), which closed kagent-dev/kagent#3029: the compiled skills load as a `kagent-skills` plugin through `--plugin-dir`, which `--setting-sources ""` does not gate, and approval turns keep the empty setting sources. The patch drops at the next re-pin, and this file with it; the line's kagent-dev/kagent#3191 was closed in its favour. Plugin skills are named `kagent-skills:<skill>`, so the re-pin checks that the agent chart, portal templates and agentlab `skills-test` do not depend on bare skill names
