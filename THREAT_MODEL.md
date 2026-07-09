# Threat Model: kagent

> **Status:** First pass (system context, trust boundaries, and triage rubric).
> The threats and mitigations tables are expanded iteratively — see
> [§6 Threats](#6-threats) and [§7 Deprioritized](#7-deprioritized-not-a-kagent-threat).
>
> **Audience:** kagent maintainers triaging inbound security reports. The primary
> job of this document is to make **in-scope vs. out-of-scope** decisions fast,
> consistent, and defensible — so that genuine privilege-boundary violations get
> fixed quickly and reports that merely restate a documented, intended default
> can be closed with a clear, reusable rationale.

---

## 1. System context

kagent is a Kubernetes-native framework for building, deploying, and running AI
agents. It is a CNCF sandbox-stage project in an alpha (`v0.x`) state. It is
composed of:

- **Controller** (Go, `go/core/internal/controller`) — a Kubernetes controller
  that reconciles kagent CRDs (`Agent`, `ModelConfig`, `ToolServer`,
  `RemoteMCPServer`, `Memory`, …) and creates the workloads (`Deployment`,
  `Service`, `ConfigMap`, `ServiceAccount`) that run agents.
- **HTTP / API server** (Go, `go/core/internal/httpserver`) — serves the REST API
  and A2A (agent-to-agent) endpoints the UI and clients use.
- **UI** (Next.js, `ui/`) — web interface; talks only to the HTTP server.
- **Agent runtime / Engine** — runs an agent's conversation loop, invoking tools
  and MCP servers. kagent ships **two runtimes**: a **Python** runtime
  (`python/packages/kagent-adk`, built on Google ADK) and a **Go** runtime
  (`go/adk`, the Go ADK: runner, session, models, mcp, memory, a2a). Both expose
  the same A2A surface and share the credential-forwarding behavior described in
  TB-6/TB-7.
- **MCP tool servers** — Model Context Protocol servers that expose tools to
  agents. The convenience `kagent-tools` server (separate repo/subchart) and
  user-registered `RemoteMCPServer`s.
- **Database** (PostgreSQL, `go/core/pkg/migrations`) — stores agents, sessions,
  events, and configuration. (SQLite is no longer supported.)
- **LLM providers** — external model APIs (OpenAI, Anthropic, Azure, Vertex,
  Ollama, …), reached with credentials stored in Kubernetes Secrets.

**Deployment shape.** All in-cluster Services default to `ClusterIP`
(`helm/kagent/values.yaml`): nothing is reachable from outside the cluster until
an operator deliberately exposes it (Ingress / LoadBalancer / `port-forward`).
Reaching a useful state (using the UI/API) generally requires such exposure.

**Guiding design principle.** kagent operates *within* existing Kubernetes
security boundaries; it is explicitly **not** a replacement for Kubernetes RBAC,
admission control, or network policy (see
[`contrib/cncf/security-self-assessment.md`](contrib/cncf/security-self-assessment.md),
"Non-goals"). Several defaults are intentionally insecure-but-convenient for
getting started and are documented as such; they are **not** production
guidance. See [§5 Documented insecure-by-default posture](#5-documented-insecure-by-default-posture).

---

## 2. The trust boundary that matters: the Kubernetes namespace

kagent's core objects — most importantly the `Agent` CR — are **workload-creating
resources, semantically equivalent to a `Deployment`.** An `Agent` lets its
creator specify a pod template: `serviceAccountName`, `volumes`, `extraContainers`,
`securityContext`, container `image`, and `command`.

This is deliberate and mirrors native Kubernetes. When you create a `Deployment`,
the API server does **not** run a `SubjectAccessReview` to verify you are allowed
to *use* the `serviceAccountName` you name, nor that you may read the Secrets you
mount. If you can create workloads in a namespace, you can attach any
ServiceAccount, Secret, or volume **in that namespace**. The namespace — not the
individual resource — is the privilege boundary.

**Consequence for triage:** a principal who can create an `Agent` in a namespace
can already run arbitrary code in that namespace with any identity available
there. Reports that a "malicious `Agent` can run a privileged sidecar / attach a
ServiceAccount / mount a Secret **in its own namespace**" are therefore **not**
kagent threats — they are the documented, inherited semantics of a
workload-creating resource. See [§7 Deprioritized](#7-deprioritized-not-a-kagent-threat).

What *is* a threat is any path that lets an actor **cross a boundary they were not
granted** — most importantly reaching **across a namespace** (e.g. reading Secrets
in a namespace they have no access to) or reaching in from **outside the cluster
without authentication**.

---

## 3. Actors

| # | actor | in scope? | notes |
|---|-------|-----------|-------|
| A1 | **External unauthenticated** (internet / off-cluster) | ✅ yes | Applies when an operator exposes the UI / API / A2A endpoints beyond the cluster. The highest-severity boundary. |
| A2 | **Authenticated low-privilege API / UI user** | ✅ yes (roadmap) | The API server currently ships a `NoopAuthorizer` and no user impersonation; low-priv users can act beyond their intended scope. Tracked as a multi-tenancy/authz gap (#476). |
| A3 | **Any in-cluster pod / SSRF pivot** | ✅ yes | A compromised workload or SSRF-able service reaching an internal kagent/MCP endpoint. |
| A4 | **Cross-tenant user** in a shared cluster | ✅ yes (roadmap) | Reaching another tenant's data/agents. Bounded by the multi-tenancy gap (#476). |
| A5 | **The LLM itself / prompt injection** | ❌ **no** | Out of scope for kagent. Guarding against a manipulated model driving unintended tool calls is the responsibility of a purpose-built layer (e.g. **agentgateway**), not kagent core. |
| A6 | **User-registered `RemoteMCPServer` / external MCP tool** | ❌ **no** | Out of scope for kagent. Constraining what a user-supplied MCP tool can do or receive is the responsibility of a gateway/policy layer (e.g. **agentgateway**), not kagent core. |
| A7 | **Low-privilege K8s principal who can create `Agent`/CRDs** but not Pods directly | ❌ **no** | Equivalent to a `Deployment` author. In-namespace escalation via `serviceAccountName`/`volumes`/`extraContainers` is expected behavior, not a threat (see §2). |

**Cross-namespace secret access is explicitly in scope** for every actor: reading
or referencing a Secret in a namespace the actor was not granted access to is a
namespace-boundary violation and a genuine threat, regardless of which actor
achieves it.

---

## 4. Trust boundaries

Boundaries are labeled `TB-n`. Threats (§6) reference these.

| id | boundary | authn today | authz today | notes |
|----|----------|-------------|-------------|-------|
| **TB-1** | External client → UI / API / A2A | oauth2-proxy (opt-in `trusted-proxy` mode) **or none** (`unsecure` default) | none | In `unsecure` mode the user defaults to `admin@kagent.dev`; the `X-User-Id` header is trusted unauthenticated. Only reachable externally if the operator exposes it. |
| **TB-2** | UI → HTTP/API server | JWT (trusted-proxy) or none | **`NoopAuthorizer`** (allows everything) | No per-resource authorization; no user impersonation. Multi-tenancy/authz roadmap (#476). |
| **TB-3** | HTTP/API server → Kubernetes API | `kagent-controller` ServiceAccount | Kubernetes RBAC (cluster-wide read + write by default) | Calls execute as the controller SA, **not** as the end user — a confused-deputy surface for A2. |
| **TB-4** | K8s principal → `Agent`/CRD (workload creation) | Kubernetes authn | Kubernetes RBAC on `kagent.dev` resources | **The namespace is the boundary** (§2). No admission control on pod-template fields — by design, mirroring `Deployment`. |
| **TB-5** | Controller → agent pods | n/a (controller creates pods) | n/a (controller is privileged) | Copies user-supplied pod-template fields into the `Deployment` it creates. |
| **TB-6** | Agent runtime (Go & Python) → MCP servers | forwarded headers (`KAGENT_PROPAGATE_TOKEN` / `apiKeyPassthrough` / `allowedHeaders`) | MCP-server-dependent | Credential forwarding to user-registered MCP URLs is a documented footgun. Constraining what user-supplied MCP tools (A6) or a manipulated model (A5) may do is **out of scope for kagent core** — use a gateway/policy layer such as **agentgateway**. Applies to both runtimes (`go/adk/pkg/mcp/registry.go` and the Python runtime). |
| **TB-7** | Agent runtime (Go & Python) → LLM provider | API key from Secret (or passthrough) | provider-side | Secret reference is user-controlled but namespace-scoped. |
| **TB-8** | HTTP server / runtime → Database | DB credential | single DB user, **no row-level security** | A DB compromise exposes all users' data; relevant to A3/A4. |
| **TB-9** | In-cluster pod → internal kagent / MCP endpoints | none by default (ClusterIP, no NetworkPolicy shipped) | none | The A3 surface: any pod on the cluster network can reach these ports. |

---

## 5. Documented insecure-by-default posture

These defaults are **known, intentional, and documented** as getting-started
convenience — not production guidance. A report that merely restates one of these,
without demonstrating a boundary crossing beyond what the default already implies,
is **not** a new vulnerability (it may still be a valid *hardening* suggestion).

| default | where | intended production posture |
|---------|-------|-----------------------------|
| `controller.auth.mode: unsecure` (trusts `X-User-Id`, defaults to `admin@kagent.dev`) | `helm/kagent/values.yaml`; `go/core/internal/httpserver/auth/authn.go` | oauth2-proxy + `trusted-proxy` mode |
| `NoopAuthorizer` (no per-resource authz), no user impersonation | `go/core/internal/httpserver/auth/authz.go` | real authorizer / impersonation — roadmap #476 |
| Cluster-wide controller RBAC (wildcard `resources: "*"`, incl. Secrets) when `rbac.namespaces: []` | `helm/kagent/templates/rbac/*-role.yaml`; `values.yaml` | scope with `rbac.namespaces: [...]` |
| No `NetworkPolicy` shipped (default-allow pod-to-pod) | `helm/kagent/` (none present) | operator-applied NetworkPolicies |
| Bundled PostgreSQL with hardcoded `kagent/kagent` credentials | `helm/kagent/values.yaml` (`database.postgres.bundled`) | external managed DB |
| `kagent-tools` convenience server: unauthenticated, `shell` tool, broad RBAC | `kagent-dev/tools` subchart | read-only / scoped RBAC / gateway auth / NetworkPolicy |
| Credential forwarding (`KAGENT_PROPAGATE_TOKEN`, `apiKeyPassthrough`, `allowedHeaders`) | `go/adk/pkg/mcp/registry.go`; `go/api/v1alpha2` | avoid forwarding to untrusted MCP URLs |

The CNCF self-assessment states plainly that **secure multi-tenancy and session
isolation are not yet implemented** (roadmap #476), and that Direct Cluster
Administration is a **non-goal**. Threats that exist *only* because multi-tenancy
is unimplemented are real but classified **`risk_accepted` / roadmap** in §6: they
are documented so maintainers can track the gap and reject duplicate reports,
without treating each restatement as a new CVE.

---

## 6. Triage rubric

Apply these tests **in order** to any inbound report. The first one that matches
decides the outcome.

**Step 1 — Is a privilege boundary actually crossed?**
Does the report let an actor reach something they could **not already reach with
the permissions they legitimately hold**? Concretely:
- Reaching **across a namespace** they were not granted (e.g. reading another
  namespace's Secrets)?
- Reaching in from **outside the cluster without authentication** (A1), where the
  operator exposed an endpoint?
- Getting **another tenant's / user's** data or agents (A2/A4)?

If **no boundary is crossed** — the actor already had the capability — it is **not
a threat**. → **Deprioritize** (§7). This is the single most common reason to close
a report.

Note also what is **out of scope by design**: guarding against a manipulated model
(A5) or a malicious user-registered MCP tool (A6) is **not** kagent's job — that
protection belongs to a gateway/policy layer such as **agentgateway**. Reports in
that class are deprioritized regardless of the steps below.

**Step 2 — Is it purely a restatement of a documented insecure default (§5)?**
If the report describes an intended getting-started default and adds no boundary
crossing beyond what that default already implies, it is **not a new
vulnerability**. Acknowledge, point to the production guidance, optionally accept
as a *hardening* suggestion. → **Deprioritize** (§7).

**Step 3 — Is it solely the known multi-tenancy / authz gap (#476)?**
If the boundary crossed is A2/A4 and exists *only* because real authz / tenant
isolation is unimplemented, it is a **real threat** but **`risk_accepted` /
roadmap** — record it against #476, do not treat as a novel CVE.

**Step 4 — Otherwise, it is an in-scope threat.**
Genuine boundary crossings — especially A1 (external unauth), A3 (in-cluster
pivot to a cross-namespace/cluster capability), and ungranted cross-namespace
secret access — are **in scope**. Add to §6 threats with actor, surface (TB),
asset, impact, likelihood, status.

### Worked examples

**Example A — `kagent-tools` unauthenticated in-cluster RCE as cluster-admin**
*(reported as Critical).*
Chain: any in-cluster pod (A3) → unauthenticated `kagent-tools` `shell` tool
(TB-9) → runs as a cluster-admin ServiceAccount.
- Step 1: The capability comes entirely from the **documented default posture**
  (§5): the convenience tool server is intentionally unauthenticated with broad
  RBAC, and no NetworkPolicy ships by default. The report itself acknowledges this
  is known prior art.
- Step 2: It is a restatement of documented getting-started defaults; the fix is
  configuration (read-only mode, scoped RBAC, NetworkPolicy, gateway auth).
- **Outcome: not a CVE.** Valid *hardening* feedback; close with production
  guidance. (This matches the maintainers' actual response.)

**Example B — `skills-init` shell injection via `Agent` `gitRefs`/`refs` fields**
*(reported as High; was fixed in code).*
A principal with `create`/`update` on `Agent` injects an `ENDVAL` heredoc breakout
into a user-controlled field and runs arbitrary commands in the init container.
- Step 1: The attacker is A7 — a principal who **can already create an `Agent`**,
  i.e. can already run arbitrary code in that namespace (it is a workload-creating
  resource, §2). The injection lets them do something they **already had the
  privilege to do**. No boundary is crossed.
- **Outcome: not a CVE.** It is genuinely ugly and confusing behavior — and worth
  fixing for defense-in-depth and hygiene (and it *was* fixed) — but it does not
  cross a privilege boundary, so it does not rise to a CVE.

> **Rule of thumb:** "This looks bad / ugly / surprising" is not the test.
> "This lets someone reach across a boundary they weren't granted" is.

---

## 7. Deprioritized (not a kagent threat)

*(Populated iteratively alongside §6. Each entry names the boundary that is NOT
crossed and the trusted actor/component, following the triage rubric.)*

| candidate | reason it is deprioritized |
|-----------|----------------------------|
| `Agent` creator sets a privileged `serviceAccountName` / mounts a Secret / adds a privileged `extraContainer` **in their own namespace** | No boundary crossed (§2). Identical to `Deployment` semantics; the namespace is the trust boundary; the creator already has code-exec there. |
| `skills-init` / template injection reachable only by an `Agent` creator | Attacker (A7) already has arbitrary code-exec in the namespace via the workload they can create. Fix for hygiene, not a CVE (worked example B). |
| Unauthenticated `kagent-tools` / broad tool RBAC / missing NetworkPolicy on default install | Documented getting-started default (§5); configuration-fixable; not production guidance (worked example A). |
| Prompt injection / manipulated model (A5) driving unintended tool calls | Out of scope for kagent core. Model-behavior guardrails are the responsibility of a gateway/policy layer such as **agentgateway**. |
| Malicious or over-permissioned user-registered `RemoteMCPServer` / external MCP tool (A6) | Out of scope for kagent core. Constraining external tool behavior and credential exposure is the responsibility of a gateway/policy layer such as **agentgateway**. |
| Missing authentication in `unsecure` mode on a **non-exposed** (ClusterIP) install | Documented default (§5); no external boundary exists unless the operator exposes the endpoint. |

---

## 8. Threats (in scope)

*(To be expanded in the next pass — A1 external-exposure paths, A3 in-cluster
pivots to cross-namespace/cluster capability, cross-namespace secret access, plus
multi-tenancy/#476 `risk_accepted` items.)*

| id | threat | actor | surface | asset | impact | likelihood | status |
|----|--------|-------|---------|-------|--------|------------|--------|
| _TBD_ | _next pass_ | | | | | | |

---

## 9. Recommended mitigations

*(To be expanded alongside §8.)*
