#!/usr/bin/env bash
# check-query-contraction.sh — query-contraction check.
#
# Compiles the PREVIOUS release's sqlc queries against the CURRENT schema (the
# migration files under go/core/pkg/migrations). It fails if a migration on this
# branch removed, renamed, or retyped a column or table that a query shipped in
# the previous release still references — a schema change that would break the
# previous release's code against the new schema.
#
# Static: no database and no cluster. sqlc derives the schema from the migration
# files (see go/core/internal/database/sqlc.yaml), so "does every previous query
# still type-check against the new schema" is answerable offline. It catches
# column/table/type-shape contraction; semantic breaks (a new NOT NULL, a
# tightened constraint, an index/ordering change) are out of scope for a static
# check and belong to a runtime regression suite.
#
# Inputs (env):
#   UPGRADE_FROM_VERSION  previous version without leading 'v' (default: derived
#                         from git tags via upgrade-from-version.sh).
#   SQLC                  sqlc binary to use (default: sqlc on PATH).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "$here" rev-parse --show-toplevel)"
sqlc_bin="${SQLC:-sqlc}"

prev="${UPGRADE_FROM_VERSION:-$("$here/upgrade-from-version.sh")}"
prev_tag="v${prev}"
queries_path="go/core/internal/database/queries"
core_migrations="$repo_root/go/core/pkg/migrations/core"
vector_migrations="$repo_root/go/core/pkg/migrations/vector"

if ! git -C "$repo_root" rev-parse -q --verify "refs/tags/${prev_tag}" >/dev/null; then
  echo "ERROR: previous release tag ${prev_tag} not found; fetch tags (git fetch --tags) or set UPGRADE_FROM_VERSION." >&2
  exit 1
fi

if [ -z "$(git -C "$repo_root" ls-tree "$prev_tag" -- "$queries_path" 2>/dev/null)" ]; then
  echo "NOTE: ${prev_tag} has no ${queries_path}; skipping contraction check (predates the sqlc query set)."
  exit 0
fi

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

# Self-contained sqlc project: sqlc resolves schema/queries relative to the
# config file, so stage everything under workdir. Current migrations supply the
# schema; the previous release supplies the queries.
mkdir -p "$workdir/schema/core" "$workdir/schema/vector" "$workdir/queries" "$workdir/gen" "$workdir/prev"
cp "$core_migrations"/*.sql "$workdir/schema/core/"
cp "$vector_migrations"/*.sql "$workdir/schema/vector/"
git -C "$repo_root" archive "$prev_tag" "$queries_path" | tar -x -C "$workdir/prev"
cp "$workdir/prev/$queries_path"/*.sql "$workdir/queries/"

# Minimal config: the go_type overrides in the real sqlc.yaml only affect the
# generated Go types, not whether a query type-checks against the schema, so they
# are intentionally omitted here.
cat >"$workdir/sqlc.yaml" <<'EOF'
version: "2"
sql:
  - engine: "postgresql"
    schema: ["schema/core", "schema/vector"]
    queries: "queries"
    gen:
      go:
        package: "dbgen"
        out: "gen"
EOF

echo "=== Contraction check: queries@${prev_tag} vs current schema ==="
( cd "$workdir" && "$sqlc_bin" compile -f sqlc.yaml )
echo "OK: previous-release (${prev_tag}) queries still type-check against the current schema."
