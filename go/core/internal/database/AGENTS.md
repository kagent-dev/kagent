# PostgreSQL persistence

Read the [database style guide](README.md) before changing queries or store behavior.

- The store owns persistence and transactional invariants. Runtime provisioning,
  asset fetching, and proxy registration belong outside this package.
- Keep parameterized SQL literals beside their owning operation so
  `TestInlineSQLPrepares` can validate them. Reuse existing pgx helpers and models;
  use private row types when storage representation or nullability differs.
- Commit related state and history changes atomically. Preserve ownership predicates,
  idempotency, lock ordering, and lifecycle conditions; never hold a transaction
  across a network call. Reject malformed durable records instead of omitting them.
- Prefer database constraints for invariants that PostgreSQL can enforce atomically.
- Test observable store behavior, isolation, rollback, and meaningful failure paths
  against PostgreSQL. Direct SQL in tests is for constraints or malformed fixtures;
  do not copy production queries into tests.
- After SQL or schema changes, run from `go/` with Docker available:
  `go test -race ./core/internal/database ./core/pkg/migrations` and `make lint`.
  Do not use `-short`: it skips PostgreSQL coverage, including statement preparation.
