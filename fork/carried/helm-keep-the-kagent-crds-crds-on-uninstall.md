---
id: helm-keep-the-kagent-crds-crds-on-uninstall
fork: kagent
theme: chart-knobs
category: bugfix
status: carried
commits:
  - 'fix(helm): keep the kagent-crds CRDs on uninstall (helm.sh/resource-policy: keep)'
  - 'fix(helm): keep the Agent and SandboxTemplate CRDs on uninstall like the rest of the chart'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3206]
  exit: file
depends_on: []
verified: 2026-10-20
---
# fix(helm): keep the kagent-crds CRDs on uninstall (helm.sh/resource-policy: keep)

## Change

`fix(helm): keep the kagent-crds CRDs on uninstall (helm.sh/resource-policy: keep)`

## Reason

the `kagent-crds` chart ships the CRDs as plain templates (no `crds/` directory), so an uninstall of that release deleted the five CRDs and every `AgentTemplate`/`RemoteMCPServer` with them; the meta chart relies on both surviving. Added as a `+kubebuilder:metadata:annotations` marker on every `api.kagent.dev` type (the seven of the line: the five carried from kagent.dev plus `Agent` and `SandboxTemplate`, which 1.3.0 shipped without it, so an uninstall of its kagent-crds release deleted every Agent), so controller-gen emits it and `make controller-manifests` stays the only producer of the templates

## History

to file — prepared in the fork: branch `upstream/kagent-crds-resource-policy-keep`

kagent-dev/kagent#3206 merged 2026-10-14 (`e7d18cde`) generates the CRDs into the chart and adds no `helm.sh/resource-policy: keep` (none on upstream `main` on 2026-10-20), so it stays in `refs` as related work and the outcome stays to-file.
