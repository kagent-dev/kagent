#!/usr/bin/env bash
# Tests scripts/get-kagent against a fake GitHub: curl serves release JSON and
# binaries from a fixture directory, and uname reports the platform under test.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

api="https://api.github.com/repos/kagent-dev/kagent/releases"
downloads="https://github.com/kagent-dev/kagent/releases/download"
fake_bin="$work/bin"
mkdir -p "$fake_bin"

cat >"$fake_bin/uname" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = "-m" ]; then echo "$FAKE_ARCH"; else echo "$FAKE_OS"; fi
EOF

# curl prints or saves fixture files and logs each requested URL.
cat >"$fake_bin/curl" <<'EOF'
#!/usr/bin/env bash
output="" url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) output="$2"; shift ;;
    -*) ;;
    *) url="$1" ;;
  esac
  shift
done
echo "$url" >>"$FIXTURE/requests"
case "$url" in
  "$API/latest") file="$FIXTURE/latest.json" ;;
  "$API?per_page=100") file="$FIXTURE/releases.json" ;;
  "$DOWNLOADS/"*) file="$FIXTURE/download/${url#"$DOWNLOADS/"}" ;;
  *) exit 22 ;;
esac
[ -f "$file" ] || exit 22
if [ -n "$output" ]; then cp "$file" "$output"; else cat "$file"; fi
EOF
chmod +x "$fake_bin/uname" "$fake_bin/curl"

# release TAG ASSET... prints one GitHub release object.
release() {
  local tag="$1" first=true
  shift
  printf '  {\n    "tag_name": "%s",\n    "assets": [\n' "$tag"
  for asset in "$@"; do
    $first || printf ',\n'
    first=false
    printf '      {\n        "name": "%s",\n        "browser_download_url": "%s/%s/%s"\n      }' "$asset" "$downloads" "$tag" "$asset"
    printf ',\n      {\n        "name": "%s.sha256",\n        "browser_download_url": "%s/%s/%s.sha256"\n      }' "$asset" "$downloads" "$tag" "$asset"
  done
  printf '\n    ]\n  }'
}

# releases RELEASE... prints a GitHub release list.
releases() {
  local first=true
  echo "["
  for item in "$@"; do
    $first || echo ","
    first=false
    echo "$item"
  done
  echo "]"
}

# publish TAG ASSET creates a downloadable binary and checksum. The binary
# answers only `version --client`; anything else records a server call.
publish() {
  local dir="$fixture/download/$1"
  mkdir -p "$dir"
  write_binary "$dir/$2" "$1"
  echo "$(openssl sha1 -sha256 "$dir/$2" | awk '{print $2}')  $2" >"$dir/$2.sha256"
}

write_binary() {
  cat >"$1" <<EOF
#!/usr/bin/env bash
if [ "\$*" = "version --client" ]; then
  echo '{"build_date":"","git_commit":"","kagent_version":"$2"}'
  exit 0
fi
touch "$fixture/server-contacted"
exit 1
EOF
  chmod +x "$1"
}

# new_fixture starts a scenario with an empty fake GitHub and install directory.
new_fixture() {
  fixture="$work/$1"
  install_dir="$fixture/install"
  mkdir -p "$fixture/download" "$install_dir"
  : >"$fixture/requests"
}

# run_installer OS ARCH ARGS... runs get-kagent and saves its output.
run_installer() {
  local os="$1" arch="$2"
  shift 2
  set +e
  PATH="$fake_bin:$install_dir:$PATH" FIXTURE="$fixture" API="$api" DOWNLOADS="$downloads" \
    FAKE_OS="$os" FAKE_ARCH="$arch" USE_SUDO=false KAGENT_INSTALL_DIR="$install_dir" \
    bash "$root/scripts/get-kagent" "$@" >"$fixture/output" 2>&1
  status=$?
  set -e
}

fail() {
  echo "FAIL: $scenario: $*" >&2
  echo "--- installer output ---" >&2
  cat "$fixture/output" >&2
  exit 1
}

assert_installed() {
  local binary="$1" want="$2"
  [ "$status" -eq 0 ] || fail "installer exited $status"
  [ -x "$install_dir/$binary" ] || fail "$binary was not installed"
  local got
  got="$("$install_dir/$binary" version --client)"
  [[ "$got" == *"\"kagent_version\":\"$want\""* ]] || fail "installed $got, want $want"
}

scenario="Given GitHub latest publishes this platform, when resolving without --version, assert that latest is installed"
new_fixture latest
release v0.10.7 kagent-linux-amd64 >"$fixture/latest.json"
releases "$(release v1.0.0-rc3 kagent-linux-amd64)" "$(release v0.10.7 kagent-linux-amd64)" >"$fixture/releases.json"
publish v0.10.7 kagent-linux-amd64
run_installer Linux x86_64
assert_installed kagent v0.10.7

scenario="Given latest lacks this platform, when resolving without --version, assert that the highest final release with the binary is installed"
new_fixture fallback
release v1.1.0 kagent-darwin-arm64 >"$fixture/latest.json"
releases "$(release v1.1.0 kagent-darwin-arm64)" "$(release v1.0.1-rc1 kagent-linux-arm64)" \
  "$(release v1.0.0 kagent-linux-arm64)" "$(release v0.10.7 kagent-linux-arm64)" >"$fixture/releases.json"
publish v1.0.0 kagent-linux-arm64
run_installer Linux aarch64
assert_installed kagent v1.0.0

scenario="Given only prereleases, when resolving without --version, assert that the installer fails and suggests --prerelease"
new_fixture prerelease-only
releases "$(release v1.0.0-rc3 kagent-linux-amd64)" >"$fixture/releases.json"
run_installer Linux x86_64
[ "$status" -ne 0 ] || fail "installer succeeded"
grep -q -- "--prerelease" "$fixture/output" || fail "output does not suggest --prerelease"

scenario="Given numbered prereleases, when resolving with --prerelease, assert that prerelease numbers compare numerically"
new_fixture prerelease-order
release v0.10.7 kagent-linux-amd64 >"$fixture/latest.json"
releases "$(release v1.0.0-rc9 kagent-linux-amd64)" "$(release v1.0.0-rc10 kagent-linux-amd64)" \
  "$(release v1.0.0-alpha17 kagent-linux-amd64)" "$(release v0.10.7 kagent-linux-amd64)" >"$fixture/releases.json"
publish v1.0.0-rc10 kagent-linux-amd64
run_installer Linux x86_64 --prerelease
assert_installed kagent v1.0.0-rc10

scenario="Given a final release and its prereleases, when resolving with --prerelease, assert that the final release wins"
new_fixture final-wins
releases "$(release v1.0.0-rc10 kagent-linux-amd64)" "$(release v1.0.0 kagent-linux-amd64)" >"$fixture/releases.json"
publish v1.0.0 kagent-linux-amd64
run_installer Linux x86_64 --prerelease
assert_installed kagent v1.0.0

scenario="Given a Windows shell, when installing, assert that the .exe binary is downloaded and installed"
new_fixture windows
release v1.0.0 kagent-windows-amd64.exe >"$fixture/latest.json"
publish v1.0.0 kagent-windows-amd64.exe
run_installer MINGW64_NT-10.0 x86_64
assert_installed kagent.exe v1.0.0
grep -qx "$downloads/v1.0.0/kagent-windows-amd64.exe" "$fixture/requests" || fail "did not download the .exe asset"

scenario="Given an unpublished architecture, when installing, assert that the installer reports no prebuilt binary"
new_fixture unsupported
run_installer Linux i686
[ "$status" -ne 0 ] || fail "installer succeeded"
grep -q "No prebuilt binary for linux-386" "$fixture/output" || fail "missing unsupported-platform message"

scenario="Given the resolved version is installed, when installing again, assert that nothing is downloaded and no server is contacted"
new_fixture already-installed
release v1.0.0 kagent-linux-amd64 >"$fixture/latest.json"
write_binary "$install_dir/kagent" v1.0.0
run_installer Linux x86_64 --version v1.0.0
[ "$status" -eq 0 ] || fail "installer exited $status"
grep -q "kagent v1.0.0 is already" "$fixture/output" || fail "did not detect the installed version"
! grep -q "/releases/download/" "$fixture/requests" || fail "downloaded despite a matching install"
[ ! -e "$fixture/server-contacted" ] || fail "version check contacted the server"

echo "get-kagent tests passed"
