---
id: helm-trim-every-trailing-non-alphanumeric
fork: kagent
theme: chart-knobs
category: bugfix
status: carried
commits:
  - 'fix(helm): trim every trailing non-alphanumeric from the truncated label values'
upstream:
  outcome: to-file
  refs: []
  exit: file in the chart batch
depends_on: []
verified: 2026-10-19
---
# fix(helm): trim every trailing non-alphanumeric from the truncated label values

## Change

`fix(helm): trim every trailing non-alphanumeric from the truncated label values`

## Reason

`helm.sh/chart` and `app.kubernetes.io/version` are cut to 63 characters and only a trailing `-` was trimmed; a dev build of a long branch (`0.11.0-dev.fork-repin-substrate-gs5.…`) was cut after a `.` and the apiserver refused every labelled object, so the upgrade failed and rolled back

## History

to file
