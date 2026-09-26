# Kubernetes API type conventions

Use this guide when adding or changing configuration types under
`go/api/v1alpha3`. These are design and review guidelines; resource-specific
behavior belongs in [docs/architecture](architecture/README.md). See the
[protobuf guide](protobuf-api.md) for gRPC API design and
[STYLE.md](../STYLE.md#crd-api-design) for Go conventions.

Go API types are the source of truth. Regenerate CRD schemas from them rather
than editing generated manifests.

## Object shape and ownership

Use native Kubernetes objects with `TypeMeta`, `ObjectMeta`, and `Spec`. Add
`Status` only when the resource owns observed state. List types use `ListMeta`
and `Items`.

| Part | Owner and purpose |
| --- | --- |
| `metadata` | Kubernetes identity, labels, annotations, and concurrency metadata |
| `spec` | User-authored desired configuration |
| `status` | Controller-observed results and conditions |

Give each resource a clear responsibility. Reusable configuration does not need
runtime status merely because another resource consumes it. Keep implementation
details out of the public API unless callers need them to express intent.

## Identity and references

Namespace and name identify an object; UID distinguishes deletion and recreation.
Validate names according to the referenced kind, not a universal DNS-label rule.
Use native structured types such as `LocalObjectReference`,
`TypedLocalObjectReference`, and `SecretKeySelector`. Start with same-namespace
references; crossing namespaces requires an explicit authorization policy.

Do not encode references as `"namespace/name"` strings. Keep inline configuration
and references in separate, typed fields. If both forms are supported, define
which combinations are valid, how nested references resolve, and whether inline
values are complete configurations or overrides.

Keep secret values out of spec and status. Resolve credential references at the
owning boundary. An object reference grants no access by itself.

## Fields and presence

- Use named, typed fields with lowerCamelCase JSON names. Avoid arbitrary JSON,
  generic extension maps, and embedded Pod specs added for future flexibility.
- Every field declares `+required` or `+optional`. Optional scalar and struct
  fields use pointers and `omitempty`. Maps and slices use `omitempty` without
  pointer wrappers. Requiredness must be explicit in the generated schema.
- Declare defaults and bounds with kubebuilder markers. Explain the meaning of
  absent, empty, and zero values; defaults must pass validation.
- Prefer native Kubernetes quantities, selectors, references, and conditions
  when their semantics match.
- Declare list/map semantics, keys, and uniqueness where merge behavior matters.
  Use inline embedded structs for shared sub-specs.
- Interfaces in API packages carry `+kubebuilder:object:generate=false`.

Read the generated schema after generation. Go pointer choices, JSON tags, and
admission rules together determine what clients can send.

## Validation and unions

Enforce rules that depend only on the submitted object through structural schemas
and CEL `XValidation`: requiredness, bounds, enums, immutable fields, and
relationships between fields. Exclusive variants use typed sibling fields with
exactly-one or at-most-one CEL validation. Avoid a second discriminator that can
contradict the selected branch.

Admission must reject invalid combinations. Test both-set and neither-set cases
against the generated schema, not only Go constructors. Defaults must not turn
invalid input into an apparently valid choice.

Checks requiring other objects or external systems belong to the owning
controller or service. Report unresolved references and incompatible
configuration through status.

## Status and reconciliation

Enable the status subresource for resources that own observed state. Controllers
update it without rewriting the user's spec. Use `observedGeneration` and
`[]metav1.Condition` with stable types and reasons, updated through
`meta.SetStatusCondition`.

Readiness describes the observed generation and the operation users can perform.
Unknown or stale status must not appear healthy. When applying a new
configuration fails, distinguish the failed desired state from any previously
applied state that remains usable.

Status may contain necessary resolved references and reconciliation results.
Avoid convenience booleans that duplicate conditions. Never expose credentials
or private backend routing details. Security-sensitive operations must enforce
current authorization rather than treating status as proof of access.

Reconciliation must tolerate retries and restart. Requeue for external state
changes instead of blocking until they finish. Define ownership, cleanup, and
retention before creating dependent resources; use owner references for garbage
collection and finalizers for cleanup that Kubernetes cannot perform itself.

## Updates and deletion

Preserve Kubernetes update, patch, server-side apply, and field-ownership
semantics. `resourceVersion` is opaque. Keep identity immutable and validate
immutable spec fields explicitly. Editors preserve fields outside their scope;
status writes use the status subresource.

Replacement updates through gRPC should carry the client's UID and
resourceVersion and reject stale writes. Fetching current metadata and attaching
it to a stale replacement spec defeats that protection. See
[Kubernetes objects over gRPC](protobuf-api.md#kubernetes-objects-over-grpc).
These are design requirements, not guarantees about every existing adapter.

Create retains native namespace/name collision behavior. Delete honors supplied
UID/resourceVersion preconditions. Distinguish acceptance of deletion from
completion of finalizers, and document which external resources survive deletion.
A recreated object must not silently inherit state belonging to a previous UID.

## Review and generation

An API change explains field ownership, presence/defaults, admission rules,
status meaning, and update/deletion behavior. Include valid and invalid manifests
and focused tests for changed admission and reconciliation behavior. Review
compatibility with stored objects and update affected clients and fixtures.

Run `make controller-manifests`, relevant Go/API lint and tests, and any additional
generation required by the change. Review generated deepcopy code, CRDs, Helm
copies, and RBAC as applicable; never edit generated outputs directly.

Use [Gateway API](https://github.com/kubernetes-sigs/gateway-api) as prior art when
a Kubernetes modeling choice needs a worked example.
