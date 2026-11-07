#!/usr/bin/env bash
# Tests scripts/get-kagent against a fake GitHub: curl answers the
# /releases/latest redirect and serves binaries from a fixture directory, and
# uname reports the platform under test.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

releases="https://github.com/kagent-dev/kagent/releases"
downloads="$releases/download"
fake_bin="$work/bin"
mkdir -p "$fake_bin"

cat >"$fake_bin/uname" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = "-m" ]; then echo "$FAKE_ARCH"; else echo "$FAKE_OS"; fi
EOF

# curl redirects /releases/latest to the tag in $FIXTURE/latest, prints or
# saves fixture files, and logs each requested URL.
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
  "$RELEASES/latest")
    if [ -f "$FIXTURE/latest" ]; then
      printf 'HTTP/2 302\r\nlocation: %s/tag/%s\r\n\r\n' "$RELEASES" "$(cat "$FIXTURE/latest")"
    else
      printf 'HTTP/2 404\r\n\r\n'
    fi
    exit 0 ;;
  "$RELEASES/download/"*) file="$FIXTURE/download/${url#"$RELEASES/download/"}" ;;
  *) exit 22 ;;
esac
[ -f "$file" ] || exit 22
if [ -n "$output" ]; then cp "$file" "$output"; else cat "$file"; fi
EOF
chmod +x "$fake_bin/uname" "$fake_bin/curl"

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
  PATH="$fake_bin:$install_dir:$PATH" FIXTURE="$fixture" RELEASES="$releases" \
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

scenario="Given GitHub marks a release candidate latest, when installing without --version, assert that the release candidate is installed"
new_fixture latest
echo v1.0.0-rc3 >"$fixture/latest"
publish v1.0.0-rc3 kagent-linux-amd64
publish v0.10.7 kagent-linux-amd64
run_installer Linux x86_64
assert_installed kagent v1.0.0-rc3

scenario="Given a requested version, when installing with --version, assert that it is installed without resolving latest"
new_fixture pinned
echo v1.0.0-rc3 >"$fixture/latest"
publish v0.10.7 kagent-linux-arm64
run_installer Linux aarch64 --version 0.10.7
assert_installed kagent v0.10.7
! grep -qx "$releases/latest" "$fixture/requests" || fail "resolved latest despite --version"

scenario="Given latest cannot be resolved, when installing without --version, assert that the installer fails and suggests --version"
new_fixture no-latest
run_installer Linux x86_64
[ "$status" -ne 0 ] || fail "installer succeeded"
grep -q -- "--version" "$fixture/output" || fail "output does not suggest --version"

scenario="Given latest lacks this platform's binary, when installing, assert that the installer fails without installing"
new_fixture missing-asset
echo v1.0.0 >"$fixture/latest"
publish v1.0.0 kagent-darwin-arm64
run_installer Linux x86_64
[ "$status" -ne 0 ] || fail "installer succeeded"
[ ! -e "$install_dir/kagent" ] || fail "installed a binary"

scenario="Given a Windows shell, when installing, assert that the .exe binary is downloaded and installed"
new_fixture windows
echo v1.0.0 >"$fixture/latest"
publish v1.0.0 kagent-windows-amd64.exe
run_installer MINGW64_NT-10.0 x86_64
assert_installed kagent.exe v1.0.0
grep -qx "$downloads/v1.0.0/kagent-windows-amd64.exe" "$fixture/requests" || fail "did not download the .exe asset"

scenario="Given an unpublished architecture, when installing, assert that the installer reports no prebuilt binary"
new_fixture unsupported
run_installer Linux i686
[ "$status" -ne 0 ] || fail "installer succeeded"
grep -q "No prebuilt binary for linux-386" "$fixture/output" || fail "missing unsupported-platform message"

scenario="Given latest is installed, when installing again, assert that nothing is downloaded and no server is contacted"
new_fixture already-installed
echo v1.0.0 >"$fixture/latest"
write_binary "$install_dir/kagent" v1.0.0
run_installer Linux x86_64
[ "$status" -eq 0 ] || fail "installer exited $status"
grep -q "kagent v1.0.0 is already latest" "$fixture/output" || fail "did not detect the installed version"
! grep -q "/releases/download/" "$fixture/requests" || fail "downloaded despite a matching install"
[ ! -e "$fixture/server-contacted" ] || fail "version check contacted the server"

echo "get-kagent tests passed"
