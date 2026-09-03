---
name: kagent-dev
description: >
  Development guide for kagent's v1alpha3 Harness and AgentTemplate CRDs, AgentInstance gRPC control
  plane, upstream A2A integration, Substrate runtime provisioning, tests, generation, and PR workflow.
  Use for any implementation, debugging, review, or CI task in the kagent repository.
---

# kagent development guide

Use `docs/plans/api-v2-execution-plan.md` as the implementation roadmap and dependency graph. Work in the smallest numbered PR that can own the change; do not pull later milestones forward without a concrete dependency.

## Architecture

- `Harness` and `AgentTemplate` are `kagent.dev/v1alpha3` CRDs under `go/api/v1alpha3`.
- `AgentInstance` is stored in PostgreSQL and exposed through gRPC, not Kubernetes.
- Upstream A2A owns public interaction and history semantics.
- Substrate Actors are the only runtime compute path.
- DurableDir owns private runtime state needed across lifecycle operations and snapshots.
- kagent, Codex, and Claude are release-blocking Harness adapters.

The public API must not acquire Kubernetes scheduling, service-account, workload deployment, arbitrary runtime-container, channel, profile, or generic extension fields.

## Repository map

```text
go/api/v1alpha3/                 CRD types and validation
go/api/config/crd/bases/         generated CRDs
proto/                           protobuf and Buf inputs
go/api/gen/                      generated Go protobuf code
go/core/internal/grpcserver/     gRPC transport and policy
go/core/internal/service/        transport-independent services
go/core/internal/database/       sqlc queries and generated accessors
go/core/pkg/migrations/          PostgreSQL migrations
go/core/internal/controller/     CRD reconciliation and preparation
go/adk/                          Go runtime
python/packages/                 Python runtime packages
ui/                              browser UI and BFF
helm/                            installation charts
```

## Workflow

1. Read the relevant roadmap PR and trace existing callers before editing.
2. Reuse current compiler, service, database, and runtime code where its behavior matches the new boundary.
3. Keep generated code generated; edit source types, protobufs, SQL, or templates first.
4. Add the smallest check that proves new behavior. Tests tied only to removed APIs should be deleted rather than translated.
5. Run focused tests first, then the relevant repository checks.

Useful commands:

```bash
make controller-manifests   # deepcopy, CRDs, and Helm CRD copies
buf lint
buf generate
make -C go test
make -C go lint
make -C python lint
```

After SQL changes, run `sqlc generate` in `go/core/internal/database` and commit the query, migration, and generated accessors together.

## CRD changes

- Modify only the intended v1alpha3 type and its validation.
- Use explicit typed fields; avoid extension maps and speculative options.
- Regenerate deepcopy code, CRDs, Helm CRDs, and RBAC when affected.
- Verify generated schemas contain the intended fields and omit forbidden runtime infrastructure fields.

## Protobuf changes

- Pin upstream A2A definitions; do not maintain an editable copy.
- Keep lifecycle/catalog APIs separate from A2A interaction APIs.
- Add generated contracts before registering implementations when the roadmap separates those PRs.
- Put request-intrinsic API validation in the source `.proto` with `buf.validate` annotations. Prefer standard rules, use message or field CEL for one-off domain rules, and add a predefined rule only when the same rule is reused across schemas.
- The gRPC Protovalidate interceptor enforces these rules before handlers run. Do not duplicate them in handlers or services; keep authorization and checks requiring database, Kubernetes, or network state in the owning service or workflow.
- Protovalidate stores rules in protobuf descriptors and does not generate validator files. Regenerate Go protobuf code after changing annotations.
- Run Buf lint, breaking checks when configured, generation, and generated-output verification.

## Database changes

- Add paired migrations and sqlc queries.
- Preserve transaction boundaries for idempotency, ownership, lifecycle fencing, and task ordering.
- Use PostgreSQL constraints for invariants that can be enforced atomically.
- Keep migrations schema-agnostic and safe for multiple controller replicas.

## Authorization changes

For resource or collection authorization work, read the [scoped authorization guide](references/scoped-authorization.md) before changing services or list queries.

## Testing and CI

- Focused unit and generation checks are required for implemented behavior.
- Clean-install coverage is authoritative during the API transition.
- Substrate end-to-end coverage may remain non-blocking until the final conformance milestone, but the final release requires it.
- Do not spend time preserving tests whose sole subject no longer exists.

## PR discipline

- Follow the roadmap dependency graph.
- Keep Codex and Claude adapter work in separate PRs consuming the same resolved-bundle boundary.
- Avoid concurrent ownership of protobuf registration, migrations, controller wiring, or generated CRDs.
- Use Conventional Commits and sign commits with `-s`.
