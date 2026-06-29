#!/usr/bin/env bash
# check-query-contraction.sh — query-contraction check.
#
# Compiles a PREVIOUS release's sqlc queries against the CURRENT schema (the
# migration files under go/core/pkg/migrations) and fails if a migration on this
# branch removed, renamed, or retyped a column or table that an older query still
# references — a change that would break that release's code against the new
# schema (the windowed-contraction invariant).
#
# It checks two targets, deduplicated:
#   A) the latest released tag reachable from HEAD — the in-line previous release;
#      catches a contraction introduced during the current line's development.
#   B) the previous stable line's latest patch (release/vX.Y.x tip, via
#      prev-stable-version.sh) — the supported rollback-window floor.
# Today these usually resolve to the same tag (one compile); they diverge once a
# new minor releases or the stable line gets a backport patch.
#
# Static: no database and no cluster. sqlc derives the schema from the migration
# files (see go/core/internal/database/sqlc.yaml), so "does every old query still
# type-check against the new schema" is answerable offline. It catches
# column/table/type-shape contraction; semantic breaks (a new NOT NULL, a
# tightened constraint, an index/ordering change) are out of scope for a static
# check and belong to a runtime regression suite.
#
# Inputs (env):
#   TARGET_VERSIONS  space-separated versions without leading 'v' to check
#                    instead of the auto-derived A/B (for local runs).
#   SQLC             sqlc binary to use (default: sqlc on PATH).
#   REMOTE           git remote for target B (default: origin).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "$here" rev-parse --show-toplevel)"
sqlc_bin="${SQLC:-sqlc}"
queries_path="go/core/internal/database/queries"
core_migrations="$repo_root/go/core/pkg/migrations/core"
vector_migrations="$repo_root/go/core/pkg/migrations/vector"

# Resolve the target versions.
targets=()
if [ -n "${TARGET_VERSIONS:-}" ]; then
  read -ra targets <<<"${TARGET_VERSIONS}"
else
  a="$(git -C "$repo_root" describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || true)"
  b="$("$here/prev-stable-version.sh" 2>/dev/null || true)"
  [ -n "${a}" ] && targets+=("${a}")
  [ -n "${b}" ] && targets+=("${b}")
fi
if [ "${#targets[@]}" -eq 0 ]; then
  echo "ERROR: no contraction target versions resolved; ensure tags are fetched and a release branch exists, or set TARGET_VERSIONS." >&2
  exit 1
fi
# Deduplicate, preserving order (version strings are space- and glob-free).
targets=($(printf '%s\n' "${targets[@]}" | awk 'NF && !seen[$0]++'))

workroot="$(mktemp -d)"
trap 'rm -rf "$workroot"' EXIT

# A target resolved (non-empty version) but whose tag is absent locally means
# the checkout didn't fetch tags — a misconfiguration that would otherwise let
# the whole check pass having compiled nothing. Track the two outcomes so the
# post-loop guard can fail on that case while still allowing a legitimately
# empty run (every resolved target predates the sqlc query set).
compiled=0
missing_tag=0

check_target() {
  local prev="$1"
  local prev_tag="v${prev}"

  if ! git -C "$repo_root" rev-parse -q --verify "refs/tags/${prev_tag}" >/dev/null; then
    echo "NOTE: tag ${prev_tag} not present locally; skipping (fetch tags to include it)."
    missing_tag=$((missing_tag + 1))
    return 0
  fi
  if [ -z "$(git -C "$repo_root" ls-tree "$prev_tag" -- "$queries_path" 2>/dev/null)" ]; then
    echo "NOTE: ${prev_tag} has no ${queries_path}; skipping (predates the sqlc query set)."
    return 0
  fi

  # Self-contained sqlc project: sqlc resolves schema/queries relative to the
  # config file, so stage everything under a per-target dir. Current migrations
  # supply the schema; the previous release supplies the queries.
  local wd="$workroot/$prev"
  mkdir -p "$wd/schema/core" "$wd/schema/vector" "$wd/queries" "$wd/gen" "$wd/prev"
  cp "$core_migrations"/*.sql "$wd/schema/core/"
  cp "$vector_migrations"/*.sql "$wd/schema/vector/"
  git -C "$repo_root" archive "$prev_tag" "$queries_path" | tar -x -C "$wd/prev"
  cp "$wd/prev/$queries_path"/*.sql "$wd/queries/"

  # Minimal config: the go_type overrides in the real sqlc.yaml only affect the
  # generated Go types, not whether a query type-checks against the schema.
  cat >"$wd/sqlc.yaml" <<'EOF'
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
  ( cd "$wd" && "$sqlc_bin" compile -f sqlc.yaml )
  echo "OK: ${prev_tag} queries still type-check against the current schema."
  compiled=$((compiled + 1))
}

for t in "${targets[@]}"; do
  check_target "$t"
done

# Guard against a vacuous green: if nothing compiled because resolved targets had
# no local tag, the checkout almost certainly didn't fetch tags. Fail loudly
# rather than report success on an empty run. An all-predate run (no missing
# tags) is legitimately empty and stays green.
if [ "$compiled" -eq 0 ]; then
  if [ "$missing_tag" -gt 0 ]; then
    echo "ERROR: no targets compiled — ${missing_tag} resolved version(s) had no local tag; fetch tags (fetch-depth: 0, fetch-tags: true) so the contraction check actually runs." >&2
    exit 1
  fi
  echo "NOTE: no targets compiled; all resolved versions predate the sqlc query set."
fi
