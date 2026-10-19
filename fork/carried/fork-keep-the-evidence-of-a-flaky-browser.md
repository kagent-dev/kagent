---
id: fork-keep-the-evidence-of-a-flaky-browser
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(fork): keep the evidence of a flaky browser test too'
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
verified: 2026-10-19
---
# ci(fork): keep the evidence of a flaky browser test too

## Change

`ci(fork): keep the evidence of a flaky browser test too`

## Reason

the evidence step above ran only when the job failed, so a test that failed its first attempt and passed its retry — green job, reported flaky — left nothing: giantswarm/kagent-upstream#79's schedules lifecycle and extension-points page load could not be diagnosed for that reason. The step runs unless the job was cancelled; a run whose every attempt passed writes no files there (Playwright's `.last-run.json` is hidden and not uploaded) and keeps no artifact

## History

fork-only
