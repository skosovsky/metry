#!/usr/bin/env bash
set -euo pipefail
# Same fixtures, published dependencies, no source checkout replaces/workspace.
ROOT=$(cd "$(dirname "$0")/.." && pwd)
STAGING=$(mktemp -d "${TMPDIR:-/tmp}/metry-consumer.XXXXXX")
trap 'rm -rf "$STAGING"' EXIT
export GOWORK=off
export GO111MODULE=on
MODULE=github.com/skosovsky/metry
REF=${METRY_RELEASE_REF:-latest}
cd "$STAGING"
RESOLVED=$(go list -m -f '{{.Version}}' "$MODULE@$REF")
printf 'Published consumer: %s@%s\n' "$MODULE" "$RESOLVED"
mkdir mapping otlp
cp "$ROOT/go.mod" mapping/go.mod
cp "$ROOT/go.sum" mapping/go.sum
cp "$ROOT"/examples/observation_mapping/*.go mapping/
cd mapping
go mod edit -module=consumer.invalid/observation-mapping "-require=$MODULE@$RESOLVED"
go mod tidy
go list -m -json all > graph.json
# Check the entire resolved graph, not just the root dependency.
if [[ -n "$(go list -m -f '{{if .Replace}}{{.Path}}{{end}}' all)" ]]; then echo 'Local replacement in published graph' >&2; exit 1; fi
go list -m all
go test -race -count=1 ./...
go run .
cd ../otlp
cp "$ROOT/integration/otlp/go.mod" go.mod
cp "$ROOT"/integration/otlp/*_test.go .
go mod edit -module=consumer.invalid/otlp "-require=$MODULE@$RESOLVED" "-dropreplace=$MODULE"
go mod tidy
go list -m -json all > graph.json
if [[ -n "$(go list -m -f '{{if .Replace}}{{.Path}}{{end}}' all)" ]]; then echo 'Local replacement in published graph' >&2; exit 1; fi
go test -race -count=1 ./...
