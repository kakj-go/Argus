#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
before=$(mktemp)
after=$(mktemp)
trap 'rm -f "$before" "$after"' EXIT

cd "$root"
find api/openapi/generated internal/gen web/packages/api-client/src/generated -type f \
  -exec shasum -a 256 {} + | LC_ALL=C sort >"$before"

make contract-generate

find api/openapi/generated internal/gen web/packages/api-client/src/generated -type f \
  -exec shasum -a 256 {} + | LC_ALL=C sort >"$after"

diff -u "$before" "$after"
