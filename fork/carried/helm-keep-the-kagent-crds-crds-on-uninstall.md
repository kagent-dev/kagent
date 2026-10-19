---
id: helm-keep-the-kagent-crds-crds-on-uninstall
fork: kagent
theme: chart-knobs
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3206]
  exit: 're-check against #3206'
depends_on: []
commit: 7e26bc287
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(helm): keep the kagent-crds CRDs on uninstall (helm.sh/resource-policy: keep)` | the `kagent-crds` chart ships the CRDs as plain templates (no `crds/` directory), so an uninstall of that release deleted the five CRDs and every `AgentTemplate`/`RemoteMCPServer` with them; the meta chart relies on both surviving. Added as a `+kubebuilder:metadata:annotations` marker on every `api.kagent.dev` type (the seven of the line: the five carried from kagent.dev plus `Agent` and `SandboxTemplate`, which 1.3.0 shipped without it, so an uninstall of its kagent-crds release deleted every Agent), so controller-gen emits it and `make controller-manifests` stays the only producer of the templates | to file — prepared in the fork: branch `upstream/kagent-crds-resource-policy-keep` |
