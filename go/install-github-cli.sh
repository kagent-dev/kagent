#!/bin/sh
# Installs the GitHub CLI release pinned in the given file into /usr/local/bin.
# Each line of the pin file is "<release tag> <arch> <sha256 of the release's
# gh_<version>_linux_<arch>.tar.gz>"; Renovate moves the tag and the checksum
# together (renovate.json). Usage: install-github-cli.sh <pin file> <arch>
set -eu

pins=$1
arch=$2

set -- $(awk -v arch="$arch" '$2 == arch { print $1, $3 }' "$pins")
if [ "$#" -ne 2 ]; then
  echo "no GitHub CLI pin for architecture ${arch} in ${pins}" >&2
  exit 1
fi
tag=$1
checksum=$2
name="gh_${tag#v}_linux_${arch}"

dir=$(mktemp -d)
wget -T 60 -t 3 -O "${dir}/${name}.tar.gz" \
  "https://github.com/cli/cli/releases/download/${tag}/${name}.tar.gz"
echo "${checksum}  ${dir}/${name}.tar.gz" | sha256sum -c -
tar -xzf "${dir}/${name}.tar.gz" -C "$dir" "${name}/bin/gh"
install -m 0755 "${dir}/${name}/bin/gh" /usr/local/bin/gh
rm -rf "$dir"
