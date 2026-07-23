#!/bin/sh

set -eu

repo="theaaravagarwal/nexus"
version="${NEXUS_VERSION:-latest}"
install_dir="${NEXUS_INSTALL_DIR:-$HOME/.local/bin}"

fail() {
  printf 'nexus installer: %s\n' "$1" >&2
  exit 1
}

command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"

case "$(uname -s)" in
  Darwin) release_os="Darwin" ;;
  Linux) release_os="Linux" ;;
  *) fail "unsupported operating system: $(uname -s)" ;;
esac

case "$(uname -m)" in
  arm64 | aarch64) release_arch="arm64" ;;
  x86_64 | amd64) release_arch="x86_64" ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

archive="nexus_${release_os}_${release_arch}.tar.gz"
if [ "$version" = "latest" ]; then
  release_url="https://github.com/$repo/releases/latest/download"
else
  case "$version" in
    v*) ;;
    *) version="v$version" ;;
  esac
  release_url="https://github.com/$repo/releases/download/$version"
fi

temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/nexus-install.XXXXXX")"
trap 'rm -rf "$temp_dir"' EXIT HUP INT TERM

printf 'Downloading %s...\n' "$archive"
curl -fsSL "$release_url/$archive" -o "$temp_dir/$archive"
curl -fsSL "$release_url/checksums.txt" -o "$temp_dir/checksums.txt"

checksum_line="$(awk -v name="$archive" '$2 == name { print; exit }' "$temp_dir/checksums.txt")"
[ -n "$checksum_line" ] || fail "release checksum for $archive was not found"
printf '%s\n' "$checksum_line" >"$temp_dir/archive.checksum"

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$temp_dir" && sha256sum -c archive.checksum)
elif command -v shasum >/dev/null 2>&1; then
  (cd "$temp_dir" && shasum -a 256 -c archive.checksum)
else
  fail "sha256sum or shasum is required to verify the download"
fi

tar -xzf "$temp_dir/$archive" -C "$temp_dir"
[ -f "$temp_dir/nexus" ] || fail "release archive did not contain the nexus binary"

mkdir -p "$install_dir"
install -m 0755 "$temp_dir/nexus" "$install_dir/nexus"

printf 'Installed nexus to %s/nexus\n' "$install_dir"
if ! command -v nexus >/dev/null 2>&1; then
  printf 'Add this directory to PATH:\n'
  printf '  export PATH="%s:$PATH"\n' "$install_dir"
fi

for dependency in ssh rsync fzf; do
  if ! command -v "$dependency" >/dev/null 2>&1; then
    printf 'Optional dependency missing: %s\n' "$dependency" >&2
  fi
done

"$install_dir/nexus" --version
