---
id: helm-sanitise-the-chart-version-in-the-app
fork: kagent
theme: chart-knobs
category: bugfix
status: carried
commits:
  - 'fix(helm): sanitise the chart version in the app.kubernetes.io/version label'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#695]
  exit: file in the chart batch
depends_on: []
verified: 2026-10-20
---
# fix(helm): sanitise the chart version in the app.kubernetes.io/version label

## Change

`fix(helm): sanitise the chart version in the app.kubernetes.io/version label`

## Reason

helm-controller installs the chart as `<version>+<digest>`, which is not a valid label value

## History

to file (kagent-dev/kagent#695 reports the same failure on Flux OCI installs, closed NOT_PLANNED 2026-05)
