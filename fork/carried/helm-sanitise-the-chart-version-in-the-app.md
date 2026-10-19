---
id: helm-sanitise-the-chart-version-in-the-app
fork: kagent
theme: chart-knobs
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#695]
  exit: file in the chart batch
depends_on: []
commit: 7d896fa55
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(helm): sanitise the chart version in the app.kubernetes.io/version label` | helm-controller installs the chart as `<version>+<digest>`, which is not a valid label value | to file (kagent-dev/kagent#695 reports the same failure on Flux OCI installs, closed NOT_PLANNED 2026-05) |
