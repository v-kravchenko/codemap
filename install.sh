#!/bin/sh
# Installs codemap from GitHub Releases on Linux or macOS, for CI and for those without Go:
#   curl -fsSL https://raw.githubusercontent.com/v-kravchenko/codemap/main/install.sh | sh
# CODEMAP_VERSION picks a release (v0.3.0 or 0.3.0; default the latest), CODEMAP_INSTALL_DIR the
# directory (default ~/.local/bin), CODEMAP_QUIET=1 keeps it to errors. The archive is checked against
# the release's checksums.txt. Steps go to stderr, with curl's progress bar when it is a terminal.
set -eu

repo=v-kravchenko/codemap
dir=${CODEMAP_INSTALL_DIR:-$HOME/.local/bin}

fail() {
	echo "codemap install: $*" >&2
	exit 1
}

say() {
	[ -n "${CODEMAP_QUIET:-}" ] || echo "codemap install: $*" >&2
}

# a bar on a terminal; nothing in a CI log, where it is noise
progress=-s
[ -t 2 ] && [ -z "${CODEMAP_QUIET:-}" ] && progress=--progress-bar

command -v curl >/dev/null || fail "needs curl"
case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) fail "no binary for $(uname -s) here; on Windows take the zip from https://github.com/$repo/releases" ;;
esac
case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "no binary for $(uname -m)" ;;
esac

version=${CODEMAP_VERSION:-}
if [ -z "$version" ]; then
	say "looking up the latest release"
	# the redirect of releases/latest names the tag, without the API and its rate limit
	latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") ||
		fail "cannot reach github.com"
	version=${latest##*/}
	case $version in v[0-9]*) ;; *) fail "no release found at $latest" ;; esac
fi
version=${version#v}

if command -v sha256sum >/dev/null; then
	sha() { sha256sum "$1"; }
elif command -v shasum >/dev/null; then
	sha() { shasum -a 256 "$1"; }
else
	fail "needs sha256sum or shasum to check the download"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
archive=codemap_${version}_${os}_${arch}.tar.gz
url=https://github.com/$repo/releases/download/v$version
say "downloading codemap $version for $os/$arch"
curl -fSL "$progress" -o "$tmp/$archive" "$url/$archive" || fail "no $archive in release v$version"
curl -fsSL -o "$tmp/checksums.txt" "$url/checksums.txt" || fail "no checksums.txt in release v$version"
say "checking the checksum"
want=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
got=$(sha "$tmp/$archive" | awk '{ print $1 }')
[ -n "$want" ] && [ "$want" = "$got" ] || fail "checksum of $archive does not match checksums.txt"

say "installing to $dir"
tar -xzf "$tmp/$archive" -C "$tmp" codemap
mkdir -p "$dir"
# a copy then a rename, so a codemap running from $dir is not overwritten in place
cp "$tmp/codemap" "$dir/.codemap.new"
chmod 755 "$dir/.codemap.new"
mv -f "$dir/.codemap.new" "$dir/codemap"
echo "codemap $version installed to $dir/codemap"
case :$PATH: in *:"$dir":*) ;; *) echo "add $dir to PATH to run it as codemap" ;; esac
