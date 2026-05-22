# Cross-Namespace Agent-to-Agent Communication

Proves A2A routing between agents in different Kubernetes namespaces through
the kagent Agent Gateway, with a complete production security governance stack.

## Scenario

```
team-alpha/orchestrator  →  kagent gateway (:8083)  →  team-beta/specialist
```

- `team-alpha`: orchestrator delegates math problems to the specialist
- `team-beta`: specialist accepts delegations only from `team-alpha`

## Security governance — layered model

```
┌─────────────────────────────────────────────────────────────────────┐
│  Layer 1 — Admission (OPA Gatekeeper)                               │
│  Blocks Agent CRDs with cross-ns refs from non-consumer namespaces  │
│  at the API server webhook, before etcd write.                      │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 2 — Reconciler (kagent AllowedNamespaces)                    │
│  AllowsNamespace() check at reconcile time. Target agent opts in    │
│  via spec.allowedNamespaces selector. Mirrors Gateway API pattern.  │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 3 — Network (NetworkPolicy + Istio mTLS)                     │
│  Default-deny ingress on both namespaces. Only kagent controller    │
│  SPIFFE identity may open connections to agent pods (Istio           │
│  AuthorizationPolicy). Direct pod-to-pod A2A is impossible.        │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 4 — Identity (RBAC + cert-manager)                           │
│  Each agent ServiceAccount reads only its own namespace Secrets.    │
│  Leaf TLS certs signed by cluster CA; rotated by cert-manager.      │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 5 — Token (headersFrom + External Secrets)                   │
│  Delegation token injected as X-Agent-Token from namespace-local    │
│  Secret. Vault rotates both sides via ExternalSecret every 24h.     │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 6 — Audit (K8s audit policy → SIEM)                          │
│  RequestResponse on all kagent CRD mutations. Request on Secret     │
│  reads. Feeds Loki/Splunk for compliance trail.                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Current auth gaps (dev mode)

| Component | Dev default | Production fix |
|---|---|---|
| `UnsecureAuthenticator` | Trusts `X-User-Id` header — spoofable | Apply EP-476 OIDC/JWT when merged |
| `NoopAuthorizer` | No authz enforcement in kagent gateway | Istio `AuthorizationPolicy` (file 08) until EP-476 |
| Token validation | `X-Agent-Token` forwarded but not validated by specialist | Validate in agent systemMessage prompt + EP-476 JWT |

## File layout

| File | Purpose |
|---|---|
| `00-namespaces.yaml` | Namespaces with consumer/provider labels |
| `01-rbac.yaml` | ServiceAccounts, Roles, Bindings (least-privilege) |
| `02-network-policy.yaml` | Default-deny + allow kagent-only ingress/egress |
| `03-secrets.yaml` | API keys + delegation tokens (replace values before apply) |
| `04-model-configs.yaml` | ModelConfig CRDs per namespace |
| `05-specialist-agent.yaml` | team-beta specialist with `allowedNamespaces` selector |
| `06-orchestrator-agent.yaml` | team-alpha orchestrator with cross-ns agent tool + `headersFrom` |
| `07-verify.sh` | Smoke test: routing, delegation, security rejection |
| `08-istio-authz.yaml` | Istio `PeerAuthentication` (mTLS STRICT) + `AuthorizationPolicy` |
| `09-pod-security.yaml` | Pod Security Admission restricted profile on both namespaces |
| `10-gatekeeper-policy.yaml` | OPA `ConstraintTemplate` + `Constraint` for admission-level enforcement |
| `11-cert-manager.yaml` | cert-manager `Certificate` resources for agent mTLS |
| `12-external-secrets.yaml` | ESO `ExternalSecret` for Vault-backed key rotation |
| `13-audit-policy.yaml` | K8s audit policy targeting kagent CRDs and agent namespace Secrets |

## Prerequisites

**Core (required for basic A2A):**
```bash
# kagent installed
helm install kagent kagent/kagent --namespace kagent --create-namespace
```

**Production governance (optional layers — apply incrementally):**
```bash
# Istio (layer 3 runtime)
istioctl install --set profile=default
kubectl label ns team-alpha team-beta istio-injection=enabled

# OPA Gatekeeper (layer 1 admission)
helm install gatekeeper opa/gatekeeper --namespace gatekeeper-system --create-namespace

# cert-manager (layer 4 TLS)
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager --create-namespace --set installCRDs=true

# External Secrets Operator (layer 5 rotation)
helm install external-secrets external-secrets/external-secrets \
  --namespace external-secrets --create-namespace
```

## Apply

```bash
# Core stack (layers 1-5 baseline)
kubectl apply -f examples/cross-namespace-a2a/00-namespaces.yaml
kubectl apply -f examples/cross-namespace-a2a/01-rbac.yaml
kubectl apply -f examples/cross-namespace-a2a/02-network-policy.yaml
kubectl apply -f examples/cross-namespace-a2a/03-secrets.yaml   # update values first
kubectl apply -f examples/cross-namespace-a2a/04-model-configs.yaml
kubectl apply -f examples/cross-namespace-a2a/05-specialist-agent.yaml
kubectl apply -f examples/cross-namespace-a2a/06-orchestrator-agent.yaml

# Production governance (requires prerequisites above)
kubectl apply -f examples/cross-namespace-a2a/08-istio-authz.yaml
kubectl apply -f examples/cross-namespace-a2a/09-pod-security.yaml
kubectl apply -f examples/cross-namespace-a2a/10-gatekeeper-policy.yaml
kubectl apply -f examples/cross-namespace-a2a/11-cert-manager.yaml
kubectl apply -f examples/cross-namespace-a2a/12-external-secrets.yaml
# 13-audit-policy.yaml: mount at API server --audit-policy-file (control plane config)

# Verify
bash examples/cross-namespace-a2a/07-verify.sh
```

## What the verify script proves

1. Orchestrator agent card reachable via gateway
2. Specialist agent card reachable via gateway (cross-namespace)
3. Math task delegated from orchestrator → specialist → response returned
4. Rogue agent in unlabelled namespace rejected at reconcile/admission
5. Direct pod-to-pod call blocked by NetworkPolicy

## Roadmap to full compliance

1. **EP-476 merge** — replace `UnsecureAuthenticator` with OIDC JWT validation.
   Once live, remove Istio `AuthorizationPolicy` workaround (keep mTLS).
2. **Vault PKI issuer** — replace `selfSigned` ClusterIssuer in `11-cert-manager.yaml`
   with `vault` issuer for auditable certificate lifecycle.
3. **SIEM integration** — ship `13-audit-policy.yaml` events to Loki/Splunk and
   set alerts on unexpected Secret reads and cross-namespace Agent mutations.
4. **Token validation prompt** — until EP-476, add validation logic to the
   specialist's systemMessage to reject requests missing a valid `X-Agent-Token`.
