# Database style guide

This package owns PostgreSQL persistence and transactional invariants. Use the
existing pgx driver and parameterized SQL alongside the operation that owns it.
Keep the code easy to read without adding another database library or query layer.
The repository [style guide](../../../../STYLE.md) also applies.

## Queries and reuse

- Start with inline SQL. Extract a private helper when production code or tests
  repeat the same query, or when a shared operation enforces a real invariant.
- Share the actual SQL. A helper that only calls another reader and returns one
  field adds little: call the reader and use `row.HistoryID` directly.
- Use the existing `dbExecutor` when a shared query needs to accept either a pool
  or transaction. Keep transaction ownership with the operation coordinating the
  writes; a helper receiving an executor does not make multiple writes atomic.
- Keep SQL as literals so [the preparation test](sql_test.go) can check it. Avoid
  column-list concatenation, query builders, and optional flags that combine
  unrelated queries. A locking read and an ordinary read have different contracts.
- Bind values with `$1`, `$2`, etc. Keep parameter order aligned with the SQL.
  List columns explicitly and format clauses so predicates and writes are easy
  to review.

## Rows and types

- Scan directly into an existing model when its fields and SQL semantics match.
  Keep a private row type when stored protobuf bytes, metadata, or nullability
  actually differ from the model.
- Reuse shared row types for ordinary reads. Do not create a type for every query
  merely to avoid selecting a few scalar columns.
- When a specialized read avoids large payloads or substantially unused data,
  define its small result struct inside the function. Scan a single value with
  `pgx.RowTo[T]`; it does not need a struct.
- Remove fields that nobody consumes from the scan type and selected columns.
  A column can still be needed by a predicate, write, or constraint without being
  returned to Go.
- Use pointers only for meaningful absence. A nullable table column may be
  nonnullable in a query result when its predicate excludes NULL. Do not silently
  skip malformed durable records.

## Schema and validation

- Enforce required values with `NOT NULL`, defaults, foreign keys, and `CHECK`
  constraints. Use `COALESCE` when the query intentionally maps meaningful NULLs
  to another value, rather than compensating for unnecessarily nullable columns.
- Store protobuf enum values using their full `.String()` representation. Keep
  schema constraints and SQL predicates consistent; validate stored values when
  decoding. Avoid prefix trimming, alternate spellings, or special mappings.
- Add metadata such as labels only for a concrete feature. Avoid copying fields
  through several records without a consumer.
- Validate wire inputs at their entry point. Declare gRPC request-intrinsic rules
  in protobuf annotations; service entry points used outside that interceptor
  must also enforce their input contract. Authorization and state-dependent
  checks stay with the owning service or store operation.
- Do not use `uuid.MustParse` on runtime inputs. Pass validated values as SQL
  parameters; use checked parsing when Go needs a UUID for comparison. Store
  calls must return errors instead of panicking on malformed IDs.
- [Migration 1](../../pkg/migrations/core/000001_initial.sql) is currently the
  unreleased baseline: fold approved schema changes into it and recreate local
  databases. After release, use new migrations. Keep both Up and Down valid.

## Atomicity and performance

- Prefer one statement for one logical change, such as deleting both spellings
  of an agent's memories. Use a transaction for coordinated writes.
- Preserve ownership predicates, idempotency checks, lock ordering, and lifecycle
  conditions when simplifying SQL. Never hold a transaction across a network call.
- Use `RETURNING` for values the caller needs from the database. If the validated
  result is already in memory, a conditional `Exec` and `RowsAffected` check may
  suffice. Preserve the distinction between a successful no-op and a conflict.
- Consider joins that remove preliminary reads, but preserve missing-record
  semantics. Missing instance and missing active task are equivalent for lookup;
  interruption distinguishes them.
- Match predicates to existing indexes before adding indexes. Use a stable
  timestamp such as `statement_timestamp()` when eligibility is defined at query
  start; retain `clock_timestamp()` when lease expiry requires actual elapsed
  time. Do not replace clocks indiscriminately.
- Use `EXPLAIN (ANALYZE, BUFFERS)` on representative disposable data for suspected
  performance problems. Report synthetic results as synthetic. Add batching or
  specialized projections when their benefit justifies the extra code.

## Comments and checks

Document every production database function's observable behavior: what it reads
or persists, ownership scope, ordering, missing-record behavior, conflicts, retry
semantics, and atomicity where relevant. Private readers should say when callers
must authorize access or supply a transaction. Avoid narrating each SQL line.

Exercise behavior through store methods in integration tests. Use shared query
helpers when inspecting durable representations; direct SQL is appropriate for
constraint tests or deliberately malformed fixtures. Do not copy production SQL
into a test. Cover meaningful failure paths, isolation, and rollback rather than
asserting the implementation's wording.

After SQL or schema changes, run from `go/` with Docker available:

```sh
go test -race ./core/internal/database ./core/pkg/migrations
make lint
```

The database suite prepares every inline statement against the migrated schema;
do not skip it with `-short`. Run affected caller tests when behavior changes,
format Go code, and check the diff. Use Go's default build cache; never put
`GOCACHE` under `/tmp`.
