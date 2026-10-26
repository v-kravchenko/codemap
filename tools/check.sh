#!/bin/sh
# The full checks before a commit or in CI: the quick ones from AGENTS.md plus static analysis,
# known vulnerabilities, data races and the builds for the other systems. The tools' versions are
# pinned in tools/go.mod. govulncheck reads vuln.go.dev, the only step that needs the network.
set -eu
cd "$(dirname "$0")/.."
tool() { go tool -modfile=tools/go.mod "$@"; }

unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
	echo "gofmt: $unformatted" >&2
	exit 1
fi
go vet ./...
tool staticcheck ./...
for os in windows darwin; do
	GOOS=$os go build -o /dev/null ./cmd/codemap
done
go test -race ./...
tool govulncheck ./...
