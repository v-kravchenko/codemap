#!/bin/sh
# The section of CHANGELOG.md for VERSION (without v), for the release notes; fails when there is none,
# so a tag without its changelog entry makes no release.
set -eu
cd "$(dirname "$0")/.."
notes=$(awk -v v="$1" '
	/^## \[/ { if (on) exit; on = index($0, "## [" v "]") == 1; next }
	on { print }
' CHANGELOG.md)
if [ -z "$(printf '%s' "$notes" | tr -d '[:space:]')" ]; then
	echo "CHANGELOG.md: no section for $1" >&2
	exit 1
fi
printf '%s\n' "$notes"
