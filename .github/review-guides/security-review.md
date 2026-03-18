# Security Review Guide

Load when reviewing PRs touching Helm templates, RBAC, security contexts, Dockerfiles, or credentials.

---

## Principles

- **Enforcement > defaults**: Security contexts must be enforced, not just defaulted
- **Never configurable**: `allowPrivilegeEscalation`, `privileged` must be hardcoded `false` — no env var overrides
- **System containers**: Controller, ADK runtime, UI must ALWAYS run non-root
- **Defense in depth**: Backend must enforce policy independently of UI. UI is convenience; controller is enforcement
- **Threat model required**: PRs adding security-configurable fields should answer: "What could a malicious agent definition do?"

## Container SecurityContext checklist

**NEVER user-configurable:** `privileged`, `add_capabilities`, `allowPrivilegeEscalation`
**ALWAYS hardcoded:** `drop: ["ALL"]`, `seccompProfile: RuntimeDefault`
**MAY be user-configurable:** `runAsUser` (warn on UID 0), `runAsGroup`, `readOnlyRootFilesystem`

- Container-level `securityContext` must not conflict with pod-level `PodSecurityContext`
- Admin-set contexts take precedence over user-specified values
- Tests must not normalize violations

## RBAC manifest review

- `resourceNames` restrictions on `update`/`patch`/`delete` where possible
- `create` can't be scoped by `resourceNames` — document why needed
- Helm chart RBAC templates use `.Values` for namespace
- New ClusterRoles follow `kagent-*` naming convention
- ServiceAccount per agent pod (not shared)

## Credentials and secrets

- No hardcoded credentials in any file
- API keys stored in Kubernetes Secrets, referenced via `apiKeySecret` in ModelConfig CRD
- `ValueRef` type used for secret references (namespace + name + key)
- MCP server auth headers referenced via `headersFrom` (SecretKeySelector)
- No credentials in container environment variables — mount as files or use Secret references

## Network security

- Agent pods communicate with controller via ClusterIP services
- MCP tool server connections should use TLS when available
- A2A protocol endpoints should validate request origin
- `allowedNamespaces` on RemoteMCPServer controls cross-namespace access

## General

- No secrets in CI workflow logs
- Pinned action versions in GitHub workflows (SHA, not tag)
- Docker images use non-root USER
- Helm values: sensitive defaults are empty, not example values
- Input validation on agent names and configurations
- ConfigMap/Secret cleanup on resource deletion (no orphans)
