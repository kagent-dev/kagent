#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fake_bin="$(mktemp -d)"
trap 'rm -rf "$fake_bin"' EXIT

cat >"$fake_bin/git" <<'EOF'
#!/usr/bin/env bash
case "$1 $2" in
  "describe --tags")
    [[ "$*" != *--exclude* ]] || exit 1
    echo v0.10.0-rc1
    ;;
  "ls-remote --heads")
    printf '%s\n' \
      'a refs/heads/release/v0.9.x' \
      'b refs/heads/release/v0.10.x'
    ;;
  "ls-remote --tags")
    printf '%s\n' \
      'a refs/tags/v0.9.12' \
      'b refs/tags/v0.10.0-beta11' \
      'c refs/tags/v0.10.0-rc1'
    case "${TAG_SET:-prerelease}" in
      ga|next_patch) echo 'd refs/tags/v0.10.0' ;;
    esac
    [ "${TAG_SET:-prerelease}" != next_patch ] || echo 'e refs/tags/v0.10.1-beta1'
    ;;
  *)
    exit 1
    ;;
esac
EOF
chmod +x "$fake_bin/git"

check() {
  expected="$1"
  shift
  actual="$(PATH="$fake_bin:$PATH" "$@")"
  [ "$actual" = "$expected" ] || {
    echo "expected $expected, got $actual" >&2
    exit 1
  }
}

check 0.10.0-rc1 "$root/scripts/upgrade-from-version.sh"
check 0.10.0-rc1 env CURRENT_REF=main "$root/scripts/prev-stable-version.sh"
check 0.10.0 env TAG_SET=ga CURRENT_REF=main "$root/scripts/prev-stable-version.sh"
check 0.10.1-beta1 env TAG_SET=next_patch CURRENT_REF=main "$root/scripts/prev-stable-version.sh"
check 0.9.12 env CURRENT_REF=release/v0.10.x "$root/scripts/prev-stable-version.sh"
