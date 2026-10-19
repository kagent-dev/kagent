---
id: fork-keep-the-browser-suites-evidence
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(fork): keep the browser suite''s evidence of a failed run'
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
verified: 2026-10-19
---
# ci(fork): keep the browser suite's evidence of a failed run

## Change

`ci(fork): keep the browser suite's evidence of a failed run`

## Reason

`ui-tests` uploads `ui/test-results` as a seven-day artifact when the job fails: the screenshot and video of every failed attempt, the trace of every retry. The log names the step that timed out; only the trace shows what was on the page and under the pointer while it did, and the hosted runner discarded it with the workspace (giantswarm/kagent-upstream#68 had to be diagnosed from a local reproduction for that reason)

## History

fork-only
