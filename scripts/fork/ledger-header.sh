#!/bin/sh
# Fork (giantswarm/kagent-upstream): every carried-change file under the ledger
# directory opens with the YAML front-matter `fork-ledger validate` reads
# (FORK.md, the ledger; giantswarm/kagent-upstream#719). This check is the
# same rules in POSIX sh and awk, so CI needs no Go module to run it.
#
#   scripts/fork/ledger-header.sh <fork> <dir>   # one line per problem on stderr, exit 1 on any
#   scripts/fork/ledger-header.sh --self-test   # fixture runs of the check
#
# A header is the block between the opening `---` on line 1 and the next
# `---`: `id` is the file name without .md, `fork` is <fork>, `theme` is one
# of the twenty shared slugs, `category`, `status` and `upstream.outcome` take
# their listed values, `upstream.refs` are `owner/repo#N` and never name
# giantswarm/giantswarm, `depends_on` entries are such a reference, a file of
# this ledger, or `owner/repo:<id>` in another line's ledger, `verified` is a
# date, `upstream.exit` is set unless the outcome is merged or none, and
# `commit` is empty, a 7-12 hex SHA or `owner/repo#N`.
set -eu

# --self-test: fixture runs, one ledger directory per case.
self_test() {
  check=$0
  dir=$(mktemp -d)
  trap 'rm -rf "$dir"' EXIT
  failures=0
  good() {
    cat <<'GOOD'
---
id: a-good-row
fork: kagent
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: open
  refs: [agent-substrate/substrate#1543, giantswarm/kagent-upstream#12]
  exit: 'as#1543 merged; the line''s patch drops'
depends_on: [another-row, giantswarm/substrate:let-actors-egress-through-while-resumes, agent-substrate/substrate#2117]
commit: 52f1c49a
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(x): y` | because | none |
GOOD
  }
  # case_ <name> <want exit code> <file name> <file text> [text the output must name]
  case_() {
    name=$1; want=$2; file=$3; text=$4; names=${5:-}; got=0
    ledger="$dir/$(printf '%s' "$name" | tr ' ' -)"
    mkdir -p "$ledger"
    good >"$ledger/a-good-row.md"
    good | sed 's/^id: a-good-row$/id: another-row/' >"$ledger/another-row.md"
    printf '%s\n' "$text" >"$ledger/$file"
    out=$("$check" kagent "$ledger" 2>&1) || got=$?
    if [ "$got" != "$want" ] || { [ -n "$names" ] && ! printf '%s' "$out" | grep -qF -- "$names"; }; then
      echo "FAIL $name: exit $got, want $want${names:+ naming $names}"
      printf '%s\n' "$out" | sed 's/^/    /'
      failures=$((failures + 1))
    else
      echo "ok   $name"
    fi
  }
  case_ "a good ledger" 0 "third-row.md" "$(good | sed 's/^id: a-good-row$/id: third-row/')"
  case_ "no header" 1 "bare.md" "| a | b | c |" "no header"
  case_ "never closed" 1 "open-ended.md" "$(printf -- '---\nid: open-ended\nfork: kagent\n')" "never closes"
  case_ "id names another file" 1 "wrong-id.md" "$(good)" 'does not match the file name "wrong-id"'
  case_ "the other fork" 1 "other-fork.md" "$(good | sed 's/^id: a-good-row$/id: other-fork/; s/^fork: .*/fork: elsewhere/')" 'fork "elsewhere"'
  case_ "a theme off the catalogue" 1 "off-theme.md" "$(good | sed 's/^id: a-good-row$/id: off-theme/; s/^theme: .*/theme: misc/')" 'theme "misc"'
  case_ "a category off the list" 1 "off-category.md" "$(good | sed 's/^id: a-good-row$/id: off-category/; s/^category: .*/category: chore/')" 'category "chore"'
  case_ "an outcome off the list" 1 "off-outcome.md" "$(good | sed 's/^id: a-good-row$/id: off-outcome/; s/^  outcome: .*/  outcome: pending/')" 'upstream.outcome "pending"'
  case_ "a missing key" 1 "no-verified.md" "$(good | sed 's/^id: a-good-row$/id: no-verified/; /^verified:/d')" "verified is missing"
  case_ "a bad date" 1 "bad-date.md" "$(good | sed 's/^id: a-good-row$/id: bad-date/; s/^verified: .*/verified: yesterday/')" 'verified "yesterday"'
  case_ "a bare reference" 1 "bare-ref.md" "$(good | sed 's/^id: a-good-row$/id: bare-ref/; s/^  refs: .*/  refs: [#12]/')" 'upstream.refs "#12"'
  case_ "the internal tracker" 1 "tracker.md" "$(good | sed 's/^id: a-good-row$/id: tracker/; s/^  refs: .*/  refs: [giantswarm\/giantswarm#37742]/')" "internal tracker"
  case_ "an exit missing while open" 1 "no-exit.md" "$(good | sed 's/^id: a-good-row$/id: no-exit/; /^  exit:/d')" "upstream.exit is empty while outcome is open"
  case_ "an exit missing while merged" 0 "merged.md" "$(good | sed 's/^id: a-good-row$/id: merged/; s/^  outcome: .*/  outcome: merged/; /^  exit:/d')"
  case_ "a dependency on no file" 1 "no-dep.md" "$(good | sed 's/^id: a-good-row$/id: no-dep/; s/^depends_on: .*/depends_on: [missing-row]/')" 'depends_on "missing-row" names no file'
  case_ "a malformed dependency" 1 "bad-dep.md" "$(good | sed 's/^id: a-good-row$/id: bad-dep/; s/^depends_on: .*/depends_on: [Not A Row]/')" 'depends_on "Not A Row"'
  case_ "an empty commit" 0 "no-commit.md" "$(good | sed 's/^id: a-good-row$/id: no-commit/; s/^commit: .*/commit: ""/')"
  case_ "a short commit" 1 "short-commit.md" "$(good | sed 's/^id: a-good-row$/id: short-commit/; s/^commit: .*/commit: abc12/')" 'commit "abc12"'
  case_ "a pull request as the commit" 0 "pr-commit.md" "$(good | sed 's/^id: a-good-row$/id: pr-commit/; s/^commit: .*/commit: giantswarm\/kagent-upstream#7/')"
  case_ "a quoted commit" 0 "quoted-commit.md" "$(good | sed 's/^id: a-good-row$/id: quoted-commit/; s/^commit: .*/commit: "08779062"/')"
  if [ "$failures" -gt 0 ]; then
    echo "$failures case(s) failed"
    exit 1
  fi
  exit 0
}
if [ "${1:-}" = --self-test ]; then
  self_test
fi

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <fork> <dir>" >&2
  exit 64
fi
fork=$1
dir=$2
if [ ! -d "$dir" ]; then
  echo "$dir: not a directory" >&2
  exit 64
fi

ids=$(for f in "$dir"/*.md; do b=${f##*/}; [ "$b" = README.md ] && continue; printf '%s\n' "${b%.md}"; done)

problems=0
for f in "$dir"/*.md; do
  base=${f##*/}
  [ "$base" = README.md ] && continue
  if ! out=$(awk -v fork="$fork" -v id="${base%.md}" -v ids="$ids" '
    function unquote(v,    q) {
      sub(/^[ \t]+/, "", v); sub(/[ \t]+$/, "", v)
      q = substr(v, 1, 1)
      if ((q == "\"" || q == "'"'"'") && substr(v, length(v), 1) == q && length(v) >= 2) {
        v = substr(v, 2, length(v) - 2)
        if (q == "'"'"'") gsub(/'"''"'/, "'"'"'", v)
      }
      return v
    }
    function problem(m) { print m; problems++ }
    function islist(v) { return v ~ /^\[.*\]$/ }
    function items(v, arr,    n, i, raw) {
      v = substr(v, 2, length(v) - 2)
      if (v ~ /^[ \t]*$/) return 0
      n = split(v, raw, /,[ \t]*/)
      for (i = 1; i <= n; i++) arr[i] = unquote(raw[i])
      return n
    }
    BEGIN {
      split("session-lifecycle workspace-volumes credential-injection durable-pause egress-policies providers-vertex harness-variety golden-boot identity-authorizer mcp-forwarder jwks tenancy compat-migrations chart-knobs chart-shape ci-publish-infra test-flakes version-pins new-rpcs never-filed", t, " ")
      for (i in t) themes[t[i]] = 1
      split("pin infra bugfix feature gs-specific compat test", c, " "); for (i in c) categories[c[i]] = 1
      split("carried dropped superseded", s, " "); for (i in s) statuses[s[i]] = 1
      split("merged open declined superseded to-file fork-only drops-at-repin none", o, " "); for (i in o) outcomes[o[i]] = 1
      n = split(ids, idlist, "\n"); for (i = 1; i <= n; i++) known[idlist[i]] = 1
      problems = 0; closed = 0; in_upstream = 0
    }
    NR == 1 { if ($0 != "---") { problem("no header: the file must start with a --- front-matter"); exit } next }
    closed { next }
    $0 == "---" { closed = 1; next }
    /^[a-z_]+:/ {
      in_upstream = 0
      key = $0; sub(/:.*/, "", key)
      val = $0; sub(/^[a-z_]+:/, "", val)
      if (key == "upstream") { in_upstream = 1; seen["upstream"] = 1; next }
      seen[key] = 1; top[key] = val; next
    }
    in_upstream && /^  [a-z_]+:/ {
      key = $0; sub(/^  /, "", key); sub(/:.*/, "", key)
      val = $0; sub(/^  [a-z_]+:/, "", val)
      seen["upstream." key] = 1; up[key] = val; next
    }
    { problem("header: unexpected line " NR ": " $0) }
    END {
      if (NR == 0) { problem("no header: the file is empty"); print "PROBLEMS=" problems; exit }
      if (problems && !closed && NR == 1) { print "PROBLEMS=" problems; exit }
      if (!closed) problem("no header: the front-matter never closes with ---")
      split("id fork theme category status upstream.outcome verified", req, " ")
      for (i in req) if (!(req[i] in seen)) problem("header: " req[i] " is missing")
      v = unquote(top["id"]); if (("id" in seen) && v != id) problem("id \"" v "\" does not match the file name \"" id "\"")
      v = unquote(top["fork"]); if (("fork" in seen) && v != fork) problem("fork \"" v "\" is not \"" fork "\"")
      v = unquote(top["theme"]); if (("theme" in seen) && !(v in themes)) problem("theme \"" v "\" is not in the catalogue")
      v = unquote(top["category"]); if (("category" in seen) && !(v in categories)) problem("category \"" v "\" is not pin, infra, bugfix, feature, gs-specific, compat or test")
      v = unquote(top["status"]); if (("status" in seen) && !(v in statuses)) problem("status \"" v "\" is not carried, dropped or superseded")
      outcome = unquote(up["outcome"]); if (("upstream.outcome" in seen) && !(outcome in outcomes)) problem("upstream.outcome \"" outcome "\" is not merged, open, declined, superseded, to-file, fork-only, drops-at-repin or none")
      v = unquote(top["verified"]); if (("verified" in seen) && v !~ /^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]$/) problem("verified \"" v "\" is not a YYYY-MM-DD date")
      if ("upstream.refs" in seen) {
        v = unquote(up["refs"])
        if (!islist(v)) problem("upstream.refs is not a [list]")
        else { n = items(v, refs); for (i = 1; i <= n; i++) {
          if (refs[i] !~ /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+#[0-9]+$/) problem("upstream.refs \"" refs[i] "\" is not owner/repo#N")
          else if (refs[i] ~ /^giantswarm\/giantswarm#/) problem("upstream.refs \"" refs[i] "\" names the internal tracker")
        } }
      }
      exitv = unquote(up["exit"])
      if ((exitv == "" || exitv == "-") && outcome != "merged" && outcome != "none") problem("upstream.exit is empty while outcome is " outcome)
      if ("depends_on" in seen) {
        v = unquote(top["depends_on"])
        if (!islist(v)) problem("depends_on is not a [list]")
        else { n = items(v, deps); for (i = 1; i <= n; i++) {
          d = deps[i]
          if (d ~ /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+#[0-9]+$/) { if (d ~ /^giantswarm\/giantswarm#/) problem("depends_on \"" d "\" names the internal tracker"); continue }
          if (d ~ /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+:[a-z0-9][a-z0-9-]*$/) continue
          if (d ~ /^[a-z0-9][a-z0-9-]*$/) { if (!(d in known)) problem("depends_on \"" d "\" names no file in this ledger"); continue }
          problem("depends_on \"" d "\" is neither owner/repo#N nor [owner/repo:]id")
        } }
      }
      v = unquote(top["commit"])
      if (v != "" && v !~ /^[0-9a-f]+$/ && v !~ /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+#[0-9]+$/) problem("commit \"" v "\" is neither a SHA nor owner/repo#N")
      else if (v ~ /^[0-9a-f]+$/ && (length(v) < 7 || length(v) > 12)) problem("commit \"" v "\" is not a 7-12 hex SHA")
      print "PROBLEMS=" problems
    }
  ' "$f") || true; then :; fi
  count=$(printf '%s\n' "$out" | sed -n 's/^PROBLEMS=//p' | tail -1)
  printf '%s\n' "$out" | grep -v '^PROBLEMS=' | sed "s|^|$f: |" >&2 || true
  [ -z "$count" ] && count=1
  problems=$((problems + count))
done

if [ "$problems" -gt 0 ]; then
  echo "$problems problem(s) in $dir" >&2
  exit 1
fi
