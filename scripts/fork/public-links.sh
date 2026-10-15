#!/usr/bin/env bash
# The fork's ledger cites public sources only (FORK.md, "Contributing"). This
# repository is public: FORK.md and fork/ link no issue, pull request or file
# of a private repository, so every reader can follow every citation, and a
# reason that lives in a private repository is written out in a sentence.
#
#   scripts/fork/public-links.sh                 # every reference in FORK.md and fork/
#   scripts/fork/public-links.sh --added <rev>   # the lines added since <rev> (a pull request)
#
# A citation is a github.com/giantswarm/<repo> URL or a giantswarm/<repo>#<n>
# reference. Each cited repository is looked up on the GitHub API (with
# GITHUB_TOKEN when it is set): one that is not public, or not found, is red.
# giantswarm/giantswarm is internal and red without a lookup.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
paths=(FORK.md fork)
mapfile -t files < <(git ls-files "${paths[@]}")
internal=(giantswarm)

# The giantswarm repositories the text on stdin cites, one per line.
cited_repos() {
  grep -oE 'github\.com/giantswarm/[A-Za-z0-9_.-]+|giantswarm/[A-Za-z0-9_.-]+#[0-9]+' |
    sed -E 's#^(github\.com/)?giantswarm/##; s/#[0-9]+$//; s/\.git$//; s/\.+$//' |
    sort -u || true
}

# public | private | <the HTTP status of a failed lookup>
visibility() {
  local auth=() out status
  [ -n "${GITHUB_TOKEN:-}" ] && auth=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
  out=$(curl -sS -w '\n%{http_code}' "${auth[@]}" -H 'Accept: application/vnd.github+json' \
    "https://api.github.com/repos/giantswarm/$1")
  status=${out##*$'\n'}
  case $status in
  200) [[ "$out" == *'"private": false'* || "$out" == *'"private":false'* ]] && echo public || echo private ;;
  404) echo private ;;
  *) echo "$status" ;;
  esac
}

case ${1:-} in
--added)
  [ -n "${2:-}" ] || { echo "usage: $0 --added <rev>" >&2; exit 2; }
  text=$(git diff --unified=0 "$2"...HEAD -- "${paths[@]}" | grep -E '^\+' | grep -vE '^\+\+\+ ' || true)
  scope="the lines this change adds to ${paths[*]}"
  ;;
"") text=$(cat "${files[@]}"); scope="${paths[*]}" ;;
*) echo "usage: $0 [--added <rev>]" >&2; exit 2 ;;
esac

private=()
while read -r repo; do
  [ -n "$repo" ] || continue
  if [[ " ${internal[*]} " == *" ${repo} "* ]]; then
    seen=private
  else
    seen=$(visibility "$repo")
  fi
  case $seen in
  public) ;;
  private) private+=("$repo") ;;
  *) echo "ERROR: cannot look up giantswarm/${repo} on the GitHub API (HTTP ${seen})" >&2; exit 1 ;;
  esac
done < <(cited_repos <<<"$text")

if [ ${#private[@]} -gt 0 ]; then
  echo "ERROR: ${scope} cite a repository that is not public; write the reason out in a sentence, or link a public issue here or upstream:" >&2
  for repo in "${private[@]}"; do
    echo "  giantswarm/${repo}" >&2
    grep -nE "github\.com/giantswarm/${repo}([^A-Za-z0-9_.-]|$)|giantswarm/${repo}#[0-9]" "${files[@]}" |
      cut -c1-200 | sed 's/^/    /' >&2 || true
  done
  exit 1
fi
echo "${scope}: every cited giantswarm repository is public"
