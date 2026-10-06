---
name: kagent-dev
description: >
  Development guide for kagent's v1alpha3 Harness and AgentTemplate CRDs, Session gRPC control
  plane, upstream A2A integration, Substrate runtime provisioning, tests, generation, and PR workflow.
  Use for any implementation, debugging, review, or CI task in the kagent repository.
---

# kagent development guide

Use `docs/architecture` and the current implementation to understand component boundaries. Keep each PR focused on the requested change and its necessary dependencies.

Read the root [AGENTS.md](../../../AGENTS.md) and applicable nested guides before
editing. Directory-specific validation, persistence, transport, and UI rules live
there; use this skill for development workflows and the remaining specialized guidance.

## Architecture

- `Agent`, `Harness`, and `AgentTemplate` are `api.kagent.dev/v1alpha3` CRDs under `go/api/v1alpha3`.
- `Session` is stored in PostgreSQL and exposed through gRPC, not Kubernetes.
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
go/core/internal/database/       Inline pgx SQL and private typed rows
go/core/pkg/migrations/          PostgreSQL migrations
go/core/internal/controller/     CRD reconciliation and preparation
go/adk/                          Go runtime
python/packages/                 Python runtime packages
ui/                              static Vite browser UI
helm/                            installation charts
```

## Workflow

1. Read the relevant architecture docs and trace existing callers before editing.
2. Before adding a helper or public function, search for an equivalent in the owning package and its callers; reuse existing transport and credential paths when they fit. Keep model credentials as Secret references, not plaintext ModelConfig or serialized agent fields; Substrate injects them at the egress gateway.
3. Keep generated code generated; edit source types, protobufs, SQL, or templates first.
4. Add the smallest check that proves new behavior. Tests tied only to removed APIs should be deleted rather than translated.
5. Run focused tests first, then the relevant repository checks.

Useful commands:

```bash
make controller-manifests   # deepcopy, CRDs, and Helm CRD copies
make proto-lint proto-generate proto-breaking
make -C go test
make -C go lint
make -C python lint
```

For SQL and store changes, follow [database guidance](../../../go/core/internal/database/AGENTS.md), including the PostgreSQL statement-preparation check.

## CRD changes

- Modify only the intended v1alpha3 type and its validation.
- Use explicit typed fields; avoid extension maps and speculative options.
- Regenerate deepcopy code, CRDs, Helm CRDs, and RBAC when affected.
- Verify generated schemas contain the intended fields and omit forbidden runtime infrastructure fields.

## Protobuf changes

Follow [protobuf guidance](../../../proto/AGENTS.md) for validation, upstream schemas,
generation, and compatibility checks. When implementing an RPC, also read
[gRPC transport guidance](../../../go/core/internal/grpcserver/AGENTS.md).
Add generated contracts before registering implementations when the roadmap separates those PRs.

## Database changes

- PostgreSQL migrations use Goose with embedded SQL files.
- Add each migration as `NNNNNN_description.sql`.
- Do not add legacy split files ending in `.up.sql` or `.down.sql`.
- Include one `-- +goose Up` section and one `-- +goose Down` section.
- Goose makes the Down section optional, but Kagent requires it for the database CLI.
- Do not use `-- +goose NO TRANSACTION`.
- Goose must commit each schema change and its migration record in one transaction.
- Never change, rename, or delete a migration after it merges.
- Fix an accepted migration with a new migration.
- Keep migration SQL schema-agnostic.
- Change only objects that the migration source owns.
- Do not use existence guards for source-owned objects; a migration ledger mismatch must fail.
- Use existence guards only for shared bootstrap resources such as PostgreSQL extensions.
- Each migration source must use its own migration table and advisory lock.
- Register dependent sources after the sources that they need.
- Do not add automatic down migrations when a later source fails.
- A restart must continue from each source's last committed version.
- Allow non-destructive startup when the database is ahead of the binary for rolling compatibility.
- The Goose cutover requires a fresh PostgreSQL database.
- Do not add a golang-migrate bridge for the cutover.
- Keep `schema_migrations` for the core source.
- Keep `vector_schema_migrations` for the vector source.
- Run the PostgreSQL store tests after a migration change to validate every inline statement against the new schema.
- Test the Up and Down sections against PostgreSQL.
- Use PostgreSQL constraints for invariants that the database can enforce atomically.

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
