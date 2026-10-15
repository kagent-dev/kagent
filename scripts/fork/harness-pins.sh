#!/usr/bin/env bash
# The Claude Code and Codex CLI pins of the harness images. Renovate moves a
# pin's version (renovate.json: the ARG line of go/harness/<cli>/Dockerfile and
# the Pinned*Version constant the harness checks the installed binary against);
# the per-architecture sha256 ARGs come from the vendor's own checksums, which
# no Renovate datasource reads, so this script owns them.
#
#   scripts/fork/harness-pins.sh check   # versions agree, checksums are the vendor's; exit 1 otherwise
#   scripts/fork/harness-pins.sh update  # rewrite the checksums to the vendor's for the pinned versions
#
# Claude Code: downloads.claude.ai/claude-code-releases/<version>/manifest.json.
# Codex: the release's codex-package_SHA256SUMS.
set -euo pipefail

cd "$(dirname "$0")/../.."

claude_df=go/harness/claude/Dockerfile
codex_df=go/harness/codex/Dockerfile
claude_go=go/harness/claude/config/config.go
codex_go=go/harness/codex/config/config.go

mode=${1:-}
case "$mode" in check | update) ;; *)
	echo "usage: $0 check|update" >&2
	exit 2
	;;
esac

arg() { sed -n "s/^ARG $2=//p" "$1"; }
const() { sed -n "s/^[[:space:]]*$2[[:space:]]*=[[:space:]]*\"\\(.*\\)\"/\\1/p" "$1"; }
fetch() { curl -fsSL --retry 3 --max-time 60 "$1"; }

problems=0
fail() {
	echo "harness-pins: $*" >&2
	problems=1
}

# pin <label> <dockerfile> <version ARG> <amd64 ARG> <arm64 ARG> <go file> <go const> <amd64 sha> <arm64 sha>
pin() {
	local label=$1 df=$2 vararg=$3 amdarg=$4 armarg=$5 gofile=$6 goconst=$7 amd=$8 arm=$9
	local version
	version=$(arg "$df" "$vararg")
	[ -n "$version" ] || {
		fail "$label: no ARG $vararg in $df"
		return
	}
	[ "$(const "$gofile" "$goconst")" = "$version" ] ||
		fail "$label: $df pins $version, $gofile $goconst is '$(const "$gofile" "$goconst")'"
	if [ "$mode" = check ]; then
		[ "$(arg "$df" "$amdarg")" = "$amd" ] ||
			fail "$label $version: $amdarg in $df is not the vendor's checksum ($amd); run: scripts/fork/harness-pins.sh update"
		[ "$(arg "$df" "$armarg")" = "$arm" ] ||
			fail "$label $version: $armarg in $df is not the vendor's checksum ($arm); run: scripts/fork/harness-pins.sh update"
	fi
	if [ "$mode" = update ]; then
		sed -i "s/^ARG $amdarg=.*/ARG $amdarg=$amd/;s/^ARG $armarg=.*/ARG $armarg=$arm/" "$df"
	fi
}

claude_version=$(arg "$claude_df" CLAUDE_CODE_VERSION)
manifest=$(fetch "https://downloads.claude.ai/claude-code-releases/${claude_version}/manifest.json")
claude_amd=$(jq -r '.platforms["linux-x64-musl"].checksum' <<<"$manifest")
claude_arm=$(jq -r '.platforms["linux-arm64-musl"].checksum' <<<"$manifest")

codex_version=$(arg "$codex_df" CODEX_CLI_VERSION)
sums=$(fetch "https://github.com/openai/codex/releases/download/rust-v${codex_version}/codex-package_SHA256SUMS")
codex_amd=$(awk '$2 == "codex-package-x86_64-unknown-linux-musl.tar.gz" {print $1}' <<<"$sums")
codex_arm=$(awk '$2 == "codex-package-aarch64-unknown-linux-musl.tar.gz" {print $1}' <<<"$sums")

for sum in "$claude_amd" "$claude_arm" "$codex_amd" "$codex_arm"; do
	[[ $sum =~ ^[0-9a-f]{64}$ ]] || {
		echo "harness-pins: a vendor checksum is missing or malformed ('$sum')" >&2
		exit 1
	}
done

pin "Claude Code" "$claude_df" CLAUDE_CODE_VERSION CLAUDE_CODE_AMD64_SHA256 CLAUDE_CODE_ARM64_SHA256 \
	"$claude_go" PinnedClaudeVersion "$claude_amd" "$claude_arm"
pin "Codex" "$codex_df" CODEX_CLI_VERSION CODEX_CLI_AMD64_SHA256 CODEX_CLI_ARM64_SHA256 \
	"$codex_go" PinnedCodexVersion "$codex_amd" "$codex_arm"

if [ "$mode" = update ]; then
	# The checksums are now the vendor's; only a version disagreement remains a problem.
	"$0" check || exit 1
	exit 0
fi
exit "$problems"
