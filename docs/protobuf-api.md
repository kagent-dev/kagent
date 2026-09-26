# Protobuf API conventions

Use this guide when adding or changing the gRPC APIs under
[`proto/kagent/api/v1alpha1`](../proto/kagent/api/v1alpha1). These are design and
review guidelines; resource-specific behavior belongs in
[docs/architecture](architecture/README.md). The source `.proto` files define the
wire format. See the [Kubernetes guide](kubernetes-api.md) for CRD design.

## Resource and operation design

Give each service a clear responsibility. Reuse the owning protocol's types and
semantics instead of creating competing representations of the same operation.
Keep backend mechanics private unless callers need them to express intent.

Use method-specific request and response messages. Reuse a resource message
across reads and mutations when it represents the same thing. Keep identity,
display names, mutable configuration, and observed state distinct without
forcing every resource into a universal metadata or status envelope.

Expose meaningful operations rather than requiring full CRUD for every resource.
Define whether each mutation returns the resulting resource, an acknowledgement,
or an operation callers can observe. Reads must not implicitly start work or
change lifecycle state.

## Fields and presence

- Use snake_case field names, plural collection names, and names that describe
  domain meaning. Keep field numbers stable.
- Use `oneof` for exclusive variants. If one variant is required, validate that
  the `oneof` is set; mutual exclusion alone still permits none.
- Use scalar `optional` when absence differs from zero. Message fields already
  have presence; repeated and map fields do not distinguish absent from empty.
- Define empty, zero, and omitted behavior for every mutable field. Document
  defaults where they are applied.
- Prefix enum values with the enum name and provide an `UNSPECIFIED` zero value.
  Validate allowed input values. Clients must handle unfamiliar output values
  without interpreting them as a known successful state.
- Prefer typed fields and standard timestamp/duration messages. Use unstructured
  payloads only at boundaries that require them, with explicit size and
  validation rules.

Reuse common types from [common.proto](../proto/kagent/api/v1alpha1/common.proto)
when their semantics fit. Treat resource IDs as opaque, validate their declared
format, and keep mutable display names separate from identity. References should
be structured and have explicit lookup and authorization scope.

## Validation and authorization

Declare request-intrinsic validation in `.proto` with `buf.validate`. Prefer
standard rules for required messages, strings, ranges, and enums; use CEL for
cross-field relationships. Add reusable predefined rules only when multiple
schemas need the same rule.

The shared unary Protovalidate interceptor enforces these annotations before
handlers run. Rules live in generated descriptors; there are no generated Go
validator files. Do not duplicate schema validation in transport handlers.
Verify validation coverage for streaming methods explicitly. Protovalidate does
not apply defaults.

Every RPC needs an entry in the [method policy table](../go/core/internal/grpcserver/policy.go).
Keep authorization and checks requiring database, Kubernetes, or network state
in the owning service so all transports and direct callers receive the same
checks. Preserve the authenticated principal when resolving delegated access.
Enforce runtime-only authority separately from public access.

## Updates and concurrency

Choose replacement, field-mask, or operation-specific update semantics explicitly.
Define how callers leave fields unchanged, clear values, and update nested
messages or collections. Do not infer patch semantics from nonzero fields.
Identify immutable fields separately from mutable input.

Where stale writes would lose data, require a version or etag. Compare it and
persist the mutation atomically, returning `ABORTED` on a stale version. Treat
version tokens as opaque and define their resource scope. The request must
express any concurrency guarantee the API promises; fetching the latest version
inside a handler does not protect against a stale client.

## Completion and retries

For each mutation, state whether success means configuration committed, durable
work accepted, or the operation completed. A deadline or disconnected stream
does not imply rollback. Define how a caller observes work after an ambiguous
response.

When supporting request IDs, document their scope, retention, and which inputs
must match on reuse. Retries must not repeat an accepted side effect; conflicting
reuse returns `ALREADY_EXISTS`. Define retry responses after deletion and later
edits. A retry must not silently create new work or bypass current authorization.

Commit database-only transitions in one transaction. Work spanning storage and
external systems needs durable phases, idempotent steps, and compensating
cleanup. Never hold a database transaction or lock across a network call.

## Lists

Reuse `PageRequest` and `PageResponse` for paginated control-plane methods. Define
default and maximum page sizes, stable ordering, filters, and cursor scope. Keep
page tokens opaque and document whether they remain valid when data changes.
Clients keep filters and scope unchanged while paging and continue until
`next_page_token` is empty, even after an empty intermediate page.

Apply authorization before selecting result slots and exposed cursors. Recheck
access on every page. When adapting a paginated upstream API, preserve its token
and ordering contract and account for filtering that can leave an empty page.
Make partial results and upstream failures explicit in the response contract.

## Errors

Use [serviceerrors](../go/core/internal/service/serviceerrors/errors.go) for
transport-independent failures and the shared
[gRPC error mapping](../go/core/internal/grpcserver/interceptors.go) at the boundary.
Keep unexpected internal details out of public errors.

| Code | Meaning |
| --- | --- |
| `INVALID_ARGUMENT` | Malformed request or failed input validation |
| `UNAUTHENTICATED`, `PERMISSION_DENIED` | Authentication or authorization failure |
| `NOT_FOUND` | Resource absent from the authorized lookup |
| `ALREADY_EXISTS` | Identity collision or conflicting idempotency reuse |
| `FAILED_PRECONDITION` | State or dependency prevents the operation |
| `ABORTED` | Stale version or conflicting operation |
| `RESOURCE_EXHAUSTED` | Capacity or quota exceeded |
| `UNAVAILABLE` | Temporary failure; retry only under the method's guarantees |
| `CANCELLED`, `DEADLINE_EXCEEDED` | Request cancelled or deadline elapsed; durable work may already exist |
| `INTERNAL` | Unexpected server failure |

When callers need to distinguish failures within a status code, use stable
structured details such as `google.rpc.ErrorInfo`. Define their meaning and
recovery behavior; clients must not parse error prose.

## Kubernetes objects over gRPC

Keep Go CRD types as the source of truth for Kubernetes fields. Use
`ResourceReference` and `StructuredObject` to carry native objects rather than
duplicating their specs in protobuf. Validate the wrapper kind and API version,
reject inconsistent identities, and bound unstructured payloads. Decoding does
not replace Kubernetes schema validation.

Preserve Kubernetes identity, concurrency, and field ownership through the
adapter. Replacement updates should carry caller UID/resourceVersion guards;
deletes should expose preconditions where callers need protection against
replacement. Map Kubernetes conflicts to the appropriate gRPC status and
specify whether deletion waits for finalizer completion.

These are design requirements for adapters. Check each implementation before
promising a guarantee to clients, and document any gap in that API's contract.

## Evolution and generation

Reserve removed field names and numbers. Review wire, JSON, validation, and
behavioral compatibility, including durable protobuf data. Package versions for
protobuf and Kubernetes are independent. Coordinate breaking changes with all
callers and stored data; regenerating clients alone does not migrate their usage.

Keep upstream schemas, SDK versions, and import mappings aligned. Do not fork
upstream types to add local semantics. See the [upstream schema notes](../proto/README.md).

Run protobuf workflows from the repository root, using the PR's target branch
for the compatibility comparison (`origin/main` in this example):

```sh
make proto-generate
make proto-lint
make proto-breaking PROTO_BREAKING_BRANCH=origin/main
make proto-check
```

Generation is configured in [buf.gen.yaml](../proto/buf.gen.yaml),
[buf.gen.typescript.yaml](../proto/buf.gen.typescript.yaml), and
[buf.gen.python-validation.yaml](../proto/buf.gen.python-validation.yaml).
Review and commit affected generated outputs; never edit them directly.
`proto-check` runs lint and generation and rejects drift from committed output.

Buf's [breaking-check configuration](../proto/buf.yaml) ignores unstable
packages, so a passing check does not establish alpha API compatibility.
Test changed validation, authorization, concurrency, retries, and partial failures
at their owning boundaries, and update affected callers and fixtures.
