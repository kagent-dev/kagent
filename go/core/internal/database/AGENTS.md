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

## Upgrade and rollback contracts

- Upgrade the DB before rolling out newer controllers. For rollback, replace all
  newer controllers with the older version before optionally downgrading the DB.
- During rolling upgrades and supported rollbacks, older controllers must start,
  read, and write against the newer schema, including data newer controllers wrote.
  Older controllers must not downgrade the schema on startup.
- Keep fields, stored formats, and constraints compatible with supported older
  controllers. Example: after adding `display_name`, keep `name` usable and
  populated until rollback to controllers that use `name` is no longer supported.
- New metadata must tolerate older controllers ignoring it and workers stopping
  mid-operation. Example: an older controller clears `dispatch_id` but leaves
  cleanup metadata behind. That write must succeed, and newer controllers must
  ignore the obsolete cleanup. Routine rollback must not require manual cleanup.
- Never edit merged migrations; add a new migration. Both `Up` and `Down` must
  commit schema changes and migration records atomically. `Down` must preserve
  data the older controller still uses.
- Test upgrade, downgrade, and re-upgrade with existing data. Run the previous
  supported controller's store tests against the newer schema. For new durable
  work, also test writes from both controller versions and recovery after restart.
