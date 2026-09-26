# Kubernetes API type conventions

Use this guide when adding or changing configuration types under
`go/api/v1alpha3`. The [system overview](architecture/README.md) explains what
belongs in Kubernetes.

These are target conventions for new APIs and the pre-release cleanup. **Must**
rules are review requirements; departures need a domain reason documented here.
Go API types are the source of truth; regenerate CRD schemas from them.

## Object shape

Use native Kubernetes objects with `TypeMeta`, `ObjectMeta`, `Spec`, and `Status`.
List types use `ListMeta` and `Items`. New CRD surface belongs in `v1alpha3`.

| Part | Owner and purpose |
| --- | --- |
| `metadata` | Kubernetes identity, labels, annotations, and concurrency metadata |
| `spec` | User-authored desired configuration |
| `status` | Controller-observed results and conditions |

Harness owns runtime implementation and admission policy. AgentTemplate owns
portable agent behavior. Creating either object prepares configuration; it does
not create an AgentInstance. Existing instances pin immutable prepared revisions,
so later spec edits affect future preparation and creation.

## Identity and references

Namespace and name identify an object; UID distinguishes deletion and recreation.
Validate names according to the referenced kind, not a universal DNS-label rule.
Use native structured reference types such as `LocalObjectReference`,
`TypedLocalObjectReference`, and `SecretKeySelector`. References are same-namespace
unless a separately designed authorization policy allows otherwise.

Do not encode references as `"namespace/name"` strings or embed another resource's
full spec as a reference. Keep secret values out of spec and status; use credential
references resolved at the owning boundary. An object reference grants no access
by itself.

## Fields and presence

- Use named, typed fields with lowerCamelCase JSON names. Do not add arbitrary
  JSON, generic extension maps, or embedded Pod specs for future flexibility.
- Every field declares `+required` or `+optional`. Optional scalar and struct
  fields use pointers and `omitempty`. Maps and slices use `omitempty` without
  pointer wrappers. Requiredness is explicit in the generated schema.
- Declare defaults and bounds with kubebuilder markers. Explain the meaning of
  absent, empty, and zero values; defaults must pass validation.
- Prefer native Kubernetes types, including quantities, selectors, references,
  and conditions, when their semantics match.
- Declare list/map semantics, keys, and uniqueness where merge behavior matters.
  Use inline embedded structs only for actual shared sub-specs.
- Interfaces in API packages carry `+kubebuilder:object:generate=false`.

Read the generated schema after generation. Go pointer choices, JSON tags, and
admission rules together determine what clients can send.

## Validation and unions

Enforce request-intrinsic rules through structural schemas and CEL `XValidation`:
requiredness, bounds, enums, immutable fields, and relationships between fields.
Exclusive variants use typed sibling fields with exactly-one or at-most-one CEL
validation. Do not leave union checks to reconciliation or add a second
discriminator that can contradict the selected branch.

For example, a tool binding selects exactly one of its typed `mcp` and `agent`
branches. Admission rejects both-set and neither-set values. Tests must exercise
the generated schema, not only Go constructors.

Checks requiring other objects or external systems belong to the owning
controller/service. Report unresolved references and incompatible configuration
through status. Do not silently default invalid configuration into a usable one.

## Status and reconciliation

Enable the status subresource. Controllers update observed state without rewriting
the user's spec. Use `observedGeneration` and `[]metav1.Condition` with stable
types/reasons and `meta.SetStatusCondition`.

Readiness must describe the observed generation and the operation users can
perform. Unknown or stale status must not appear healthy. Keep the last successful
prepared revision available when a newer revision fails, and report the failed
desired revision separately.

Status may contain necessary resolved references, capabilities, and preparation
results. Avoid convenience booleans that duplicate conditions. Never expose
credentials or private Actor routing details. Status reports observations;
security-sensitive operations still enforce current authorization and policy.

Reconciliation must tolerate retries and restart. Compilers produce inputs;
controllers and adapters apply them. Do not hold database locks across external
calls. Define cleanup and retention before introducing external resources or
finalizers, and use finalizers only for actual cleanup obligations.

## Updates and deletion

Preserve Kubernetes update, patch, server-side apply, and field-ownership
semantics. `resourceVersion` is opaque; never parse it as a numeric public version.
Identity is immutable, and immutable spec fields must be validated as such.

Kagent's gRPC replacement updates require the client's UID and resourceVersion.
Do not fetch the latest metadata and attach it to a stale replacement spec.
Editors preserve fields outside their scope. Status writes use the status
subresource and cannot be smuggled into a spec update.

Create retains native namespace/name collision behavior. Delete honors supplied
UID/resourceVersion preconditions. Acceptance of a Kubernetes deletion request is
not proof that finalizers have finished; adapters must state their completion
contract. Document which external resources and retained revisions survive
configuration deletion.

## Review and generation

An API PR explains field ownership, presence/defaults, admission rules, status
meaning, and update/deletion behavior. Include valid and invalid manifests and
tests for admission, reconciliation, and affected runtime behavior. Update real
clients and fixtures from the generated schema.

Run `make controller-manifests`, Go/API lint, and relevant tests. Check generated
deepcopy code, CRDs, Helm copies, and RBAC as applicable; never edit generated
outputs directly. Coordinate pre-release schema changes with stored objects and
clients.

Use [Gateway API](https://github.com/kubernetes-sigs/gateway-api) as prior art when
a Kubernetes modeling choice needs a worked example.
