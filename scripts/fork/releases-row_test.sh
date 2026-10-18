#!/usr/bin/env bash
# Tests of the Releases table's writer and gate (FORK.md, "Releases"):
# scripts/fork/releases-row.sh and `scripts/fork/ledger-check.sh releases`,
# run against a throwaway repository with its own tags, `ledger` branch and
# consumed branch.
#
#   scripts/fork/releases-row_test.sh
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }
pass() { echo "ok: $*"; }
g() { git -c user.name=t -c user.email=t@example.com -c commit.gpgsign=false -c tag.gpgsign=false "$@"; }

# origin: a consumed branch `giantswarm` with FORK.md and a Makefile, the
# stables v1.6.0 (row written) and v1.7.0 (no row yet), the candidate
# v1.7.0-rc.2 on v1.7.0's commit, and a `ledger` branch with v1.7.0's build.
origin=$work/origin
git init -q -b giantswarm "$origin"
cd "$origin"
mkdir -p scripts/fork
cp "$here/releases-row.sh" "$here/ledger-check.sh" scripts/fork/
printf 'SUBSTRATE_VERSION ?= 1.9.0\n' > Makefile
cat > FORK.md <<'EOF'
# Fork

### Releases

| Release | Tag | Commit | Upstream pin | Images (index digests: controller, ui, golang-adk, claude-harness) | Charts (kagent, kagent-crds) |
|---|---|---|---|---|---|
| `1.5.0` | `v1.5.0` (old) | `aaaaaaaa` | `p` | none | none |
| `1.6.0` | `v1.6.0` (old) | `bbbbbbbb` | `p` | none | none |

## CI and security
EOF
g add -A && g commit -q -m base
g tag v1.5.0 && g tag v1.6.0
g commit -q --allow-empty -m 'fix: the change' && g tag v1.7.0-rc.1
g commit -q --allow-empty -m 'fix: another' && g tag v1.7.0-rc.2 && g tag v1.7.0
sha=$(git rev-parse HEAD)
g checkout -q --orphan ledger
git rm -q -rf . && cat > builds.md <<EOF
| published (UTC) | version | commit | upstream pin | controller | ui | golang-adk | claude-harness | chart kagent | chart kagent-crds | run |
|---|---|---|---|---|---|---|---|---|---|---|
| 2026-10-15 02:19 | \`1.7.0\` | \`${sha:0:7}\` | \`92dd6ca\` (2026-10-11) | \`sha256:c1\` | \`sha256:u1\` | \`sha256:a1\` | \`sha256:h1\` | \`sha256:k1\` | \`sha256:r1\` | [run](https://circleci.com/gh/o/r/1) |
EOF
g add -A && g commit -q -m ledger
g checkout -q giantswarm

clone=$work/clone
git clone -q "$origin" "$clone"
cd "$clone"
git fetch -q origin ledger:refs/remotes/origin/ledger

# 1. The gate refuses 1.8.0 while 1.7.0 has no row, and names the way out.
if out=$(scripts/fork/ledger-check.sh releases v1.8.0 2>&1); then
  fail "the gate passed 1.8.0 without a row for 1.7.0"
else
  grep -qF 'no row for 1.7.0: merge the pull request "docs(fork): the Releases table carries 1.7.0"' <<<"$out" &&
    pass "the gate refuses 1.8.0 naming 1.7.0's row pull request" || fail "refusal does not name the pull request: $out"
fi

# 2. The writer builds the row from the tags, the Makefile and builds.md.
scripts/fork/releases-row.sh v1.7.0 >/dev/null
row=$(grep -F '| `1.7.0` |' FORK.md || true)
want="| \`1.7.0\` | \`v1.7.0\` (published 2026-10-15T02:19Z, on \`${sha:0:8}\`, promoted from \`v1.7.0-rc.2\`: \`v1.6.0\` plus the changes of its [release notes](https://github.com/giantswarm/kagent-upstream/releases/tag/v1.7.0). Substrate \`1.9.0\`. Published by CircleCI, [run](https://circleci.com/gh/o/r/1)) | \`${sha:0:8}\` | \`92dd6ca\` (2026-10-11) | \`sha256:c1\`, \`sha256:u1\`, \`sha256:a1\`, \`sha256:h1\` | \`sha256:k1\`, \`sha256:r1\` |"
[ "$row" = "$want" ] && pass "the row of 1.7.0 is written from the tags, the Makefile and builds.md" || fail "row:
 got  $row
 want $want"
[ "$(grep -n -F '| `1.7.0` |' FORK.md | cut -d: -f1)" = "$(( $(grep -n -F '| `1.6.0` |' FORK.md | cut -d: -f1) + 1 ))" ] &&
  pass "the row goes after 1.6.0's" || fail "the row is not after 1.6.0's"

# 3. A re-run writes nothing twice.
scripts/fork/releases-row.sh 1.7.0 >/dev/null
[ "$(grep -cF '| `1.7.0` |' FORK.md)" = 1 ] && pass "a re-run writes nothing twice" || fail "a re-run wrote the row again"

# 3b. The gate counts a row still waiting in the pull request the publish of
#     1.7.0 opened (its branch fork-ledger/releases-1.7.0 on origin), and
#     refuses, naming that pull request, while its branch lacks the row.
cp FORK.md "$work/FORK.row"
git checkout -q FORK.md
(cd "$origin" && g checkout -q -b fork-ledger/releases-1.7.0 && g commit -q --allow-empty -m 'docs(fork): no row' && g checkout -q giantswarm)
if out=$(scripts/fork/ledger-check.sh releases v1.8.0 2>&1); then
  fail "the gate passed 1.8.0 with a row branch that lacks 1.7.0's row"
else
  grep -qF '(https://github.com/giantswarm/kagent-upstream/pulls?q=is%3Apr+head%3Afork-ledger%2Freleases-1.7.0)' <<<"$out" &&
    pass "the gate refuses 1.8.0 naming where 1.7.0's row pull request is" || fail "refusal does not link the pull request: $out"
fi
(cd "$origin" && g checkout -q fork-ledger/releases-1.7.0 && cp "$work/FORK.row" FORK.md &&
  g commit -q -am 'docs(fork): the Releases table carries 1.7.0' && g checkout -q giantswarm)
if out=$(scripts/fork/ledger-check.sh releases v1.8.0 2>&1); then
  grep -qF 'the row of 1.7.0 is not on giantswarm yet: it waits in the pull request of fork-ledger/releases-1.7.0' <<<"$out" &&
    pass "the gate passes 1.8.0 with 1.7.0's row waiting in its pull request" || fail "pass without the notice: $out"
else
  fail "the gate refused 1.8.0 with 1.7.0's row in its pull request: $out"
fi
(cd "$origin" && g branch -q -D fork-ledger/releases-1.7.0)
cp "$work/FORK.row" FORK.md

# 4. An unpublished stable or a candidate gets no row.
scripts/fork/releases-row.sh v1.5.0 >/dev/null 2>&1 && pass "a version with a row is left as it is" || fail "1.5.0 with a row failed"
(cd "$origin" && g tag v1.9.0 v1.7.0) && git fetch -q --tags origin
scripts/fork/releases-row.sh v1.9.0 >/dev/null 2>&1 && fail "1.9.0 without a build got a row" || pass "an unpublished stable is refused"
scripts/fork/releases-row.sh v1.7.0-rc.2 >/dev/null 2>&1 && fail "a candidate got a row" || pass "a candidate is refused"
(cd "$origin" && git tag -d v1.9.0 >/dev/null) && git tag -d v1.9.0 >/dev/null

# 5. The gate passes a candidate cut before the row merged: the row on the
#    consumed branch counts though the checkout's FORK.md lacks it.
(cd "$origin" && cp "$clone/FORK.md" FORK.md && g commit -q -am 'docs(fork): the Releases table carries 1.7.0')
git checkout -q FORK.md
git -c advice.detachedHead=false checkout -q v1.7.0
grep -qF '| `1.7.0` |' FORK.md && fail "the candidate's checkout already has the row"
scripts/fork/ledger-check.sh releases v1.8.0 >/dev/null 2>&1 &&
  pass "the gate passes 1.8.0 with 1.7.0's row only on the consumed branch" || fail "the gate refused 1.8.0 with the row on the consumed branch"

# 6. A build row with the codex-harness column lists that image last.
(
  cd "$origin"
  g tag v1.10.0 giantswarm
  sha10=$(git rev-parse v1.10.0)
  g checkout -q ledger
  printf '| 2026-10-16 09:00 | `1.10.0` | `%s` | `92dd6ca` (2026-10-11) | `sha256:c2` | `sha256:u2` | `sha256:a2` | `sha256:h2` | `sha256:k2` | `sha256:r2` | [run](https://circleci.com/gh/o/r/2) | `sha256:x2` |\n' "${sha10:0:7}" >> builds.md
  g commit -q -am ledger
  g checkout -q giantswarm
)
git fetch -q --tags origin && git fetch -q origin +ledger:refs/remotes/origin/ledger
git checkout -q giantswarm && git pull -q --ff-only origin giantswarm
scripts/fork/releases-row.sh v1.10.0 >/dev/null
grep -F '| `1.10.0` |' FORK.md | grep -qF '| `sha256:c2`, `sha256:u2`, `sha256:a2`, `sha256:h2`, `sha256:x2` | `sha256:k2`, `sha256:r2` |' &&
  pass "a build with codex-harness lists it after claude-harness" || fail "row of 1.10.0: $(grep -F '| `1.10.0` |' FORK.md)"

# 7. A build row with the omp-harness column lists that image after codex-harness.
(
  cd "$origin"
  g tag v1.11.0 giantswarm
  sha11=$(git rev-parse v1.11.0)
  g checkout -q ledger
  printf '| 2026-10-17 09:00 | `1.11.0` | `%s` | `92dd6ca` (2026-10-11) | `sha256:c3` | `sha256:u3` | `sha256:a3` | `sha256:h3` | `sha256:k3` | `sha256:r3` | [run](https://circleci.com/gh/o/r/3) | `sha256:x3` | `sha256:o3` |\n' "${sha11:0:7}" >> builds.md
  g commit -q -am ledger
  g checkout -q giantswarm
)
git fetch -q --tags origin && git fetch -q origin +ledger:refs/remotes/origin/ledger
git checkout -q giantswarm && git pull -q --ff-only origin giantswarm
scripts/fork/releases-row.sh v1.11.0 >/dev/null
grep -F '| `1.11.0` |' FORK.md | grep -qF '| `sha256:c3`, `sha256:u3`, `sha256:a3`, `sha256:h3`, `sha256:x3`, `sha256:o3` | `sha256:k3`, `sha256:r3` |' &&
  pass "a build with omp-harness lists it after codex-harness" || fail "row of 1.11.0: $(grep -F '| `1.11.0` |' FORK.md)"

[ "$failures" -eq 0 ] || { echo "${failures} failed" >&2; exit 1; }
echo "all passed"
