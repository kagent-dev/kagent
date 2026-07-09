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
- **Skills** — extra content/code an `Agent` loads at startup from user-specified
  **git repositories** (`spec.skills.gitRefs`) and/or **OCI images**
  (`spec.skills.refs`), materialized under `/skills` in the agent pod by the
  `skills-init` init container (`go/core/internal/skillsinit`). Fetch credentials
  (`gitAuthSecretRef`, `imagePullSecrets`) are **same-namespace** Secrets.
- **Sandboxing** — two distinct mechanisms:
  - `spec.sandbox` (`SandboxConfig`) on a regular `Agent` — configures sandboxed
    *declarative* execution, notably a **default-deny outbound network** allowlist
    (`spec.sandbox.network.allowedDomains`; egress is denied when unset/empty).
  - The `SandboxAgent` CRD — runs the agent as an isolated actor on an **external
    Agent Substrate** (`agent-substrate/substrate`, a.k.a. `ate.dev`: WorkerPools,
    actor templates, snapshots). `spec.skills` is *not* supported for SandboxAgents.
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

### 2.1 Skills: choosing code to run, not crossing a boundary

`spec.skills` lets an `Agent` creator fetch content from arbitrary **git repos**
(`gitRefs`) and **OCI images** (`refs`) into `/skills` in their own agent pod. By
§2, this is the same act as choosing a container `image` for a `Deployment`: the
creator is selecting what code runs **in their own namespace, as their own
workload, using their own namespace's Secrets** (`gitAuthSecretRef` /
`imagePullSecrets` are same-namespace by API contract). So:

- **Not a threat:** "an `Agent` can fetch and run arbitrary skill code." That is
  the feature, and it is namespace-bounded and Deployment-equivalent.
- **Not a threat (but fix for hygiene):** injection *reachable only by the `Agent`
  creator* — e.g. the historical `skills-init` `ENDVAL` heredoc breakout
  (GHSA #1842). The creator already had code-exec in that namespace. The current
  implementation is nonetheless hardened: user values flow through structured JSON
  into **argv** `exec.Command` calls (never a shell), and archive extraction uses
  `os.Root` + `filepath.Localize` to reject absolute paths, `..` traversal, and
  escaping symlink targets (`go/core/internal/skillsinit/oci.go`, `git.go`).
- **Potential threat (in scope):** if skills fetching can be steered to reach a
  **cross-namespace or off-limits internal** target — i.e. **SSRF via a git/OCI
  URL** to an internal service, or a `secretRef` resolving cross-namespace. The
  namespace-scoping of secret refs and the fetch path should be treated as a
  boundary to defend and verified (→ §6/§8).

### 2.2 Sandboxing: an execution/isolation abstraction, not a claimed multi-tenant boundary

kagent has two sandbox mechanisms; be precise about what each is trusted to do:

- **`spec.sandbox.network` (egress allowlist)** is a genuine, useful **control**:
  outbound network is **default-deny**, permitting only listed `allowedDomains`
  (`SandboxConfig`, `go/core/pkg/sandboxbackend/network_host.go`). It constrains
  where a sandboxed/declarative agent can send data — a real mitigation for
  exfiltration and SSRF *from within* the agent. Treat weakening or bypassing this
  allowlist as in scope.
- **`SandboxAgent` / Agent Substrate** runs the agent as an isolated actor on an
  **external substrate** (`agent-substrate/substrate`). For this threat model the
  substrate is an **execution/isolation abstraction**, and the *strength* of its
  isolation is a property of that external provider, **not a security boundary
  kagent core claims or enforces**. kagent should not be assumed to provide
  cross-tenant isolation via SandboxAgent (consistent with multi-tenancy being a
  documented non-goal, §5). Over-claiming sandbox isolation is itself a
  documentation risk to avoid.

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
| **TB-10** | `skills-init` init container → external git / OCI registries | same-namespace Secret (`gitAuthSecretRef` / `imagePullSecrets`) | n/a | Runs in the agent's namespace fetching creator-chosen content. Boundary to defend: fetch must not be steerable to a **cross-namespace/internal** target (SSRF) and secret refs must stay **namespace-scoped**. Hardened against path-traversal & shell injection (§2.1). |
| **TB-11** | Sandboxed agent → outbound network | n/a | `spec.sandbox.network.allowedDomains` (**default-deny** egress) | A real egress control. In scope: bypassing/weakening the allowlist. **Not** a claimed cross-tenant isolation boundary. |
| **TB-12** | `SandboxAgent` → external Agent Substrate (`ate.dev`) | substrate-provider dependent | substrate-provider dependent | Execution/isolation abstraction offloaded to an external substrate. Isolation strength is the provider's property, **not** a boundary kagent core enforces (§2.2). |

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
| `spec.skills.insecureSkipVerify` / `insecureOci` (HTTP + skip TLS verify on skill image pulls) | `go/api/v1alpha2/agent_types.go`; `go/core/internal/skillsinit` | leave false; use TLS registries |
| `spec.sandbox.network` unset ⇒ **no egress restriction** on non-sandboxed agents | `go/api/v1alpha2/agent_types.go` (`SandboxConfig`) | set `allowedDomains` for sandboxed/declarative execution; egress is default-deny only when a `sandbox.network` block is present |

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
| `Agent` creator fetches arbitrary git/OCI **skills** into `/skills` in their own pod | No boundary crossed (§2.1). Selecting skill content is equivalent to choosing a `Deployment` image; runs in the creator's namespace with same-namespace secrets. |
| `SandboxAgent` isolation "not strong enough" as a cross-tenant boundary | kagent core does not claim SandboxAgent/Substrate as a cross-tenant security boundary (§2.2); isolation strength is the external substrate provider's property. Multi-tenancy is a documented non-goal (§5). |
| Sandbox egress allowlist "too permissive" because the creator set `allowedDomains: ["*"]` | No boundary crossed — the `Agent` creator opts into their own egress policy (Deployment-equivalent). Bypassing a *configured* allowlist, however, IS in scope (TB-11). |
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
