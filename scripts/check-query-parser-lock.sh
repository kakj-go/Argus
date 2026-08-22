#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
LOCK_FILE="${ROOT_DIR}/deploy/query-parsers.lock.json"

jq -e '
  .schema_version == "argus.telemetry_query_engines/v2" and
  (.engines | length == 3) and
  ([.engines[].language] | sort == ["kql", "promql", "skywalking_graphql"]) and
  (all(.engines[]; .adapter | type == "string" and length > 0))
' "$LOCK_FILE" >/dev/null

while IFS= read -r path; do
  [[ -f "${ROOT_DIR}/${path}" ]] || { echo "query engine lock references missing file: ${path}" >&2; exit 1; }
done < <(jq -r '.engines[] | .adapter, (.notice // empty)' "$LOCK_FILE")

while IFS=$'\t' read -r module version license; do
  module_version=$(go list -m -f '{{.Version}}' "$module")
  [[ "$module_version" == "$version" ]] || { echo "query engine version drift: ${module} lock=${version} go.mod=${module_version}" >&2; exit 1; }
  module_dir=$(go mod download -json "${module}@${version}" | jq -er '.Dir')
  [[ -f "${module_dir}/LICENSE" ]] || { echo "query engine license missing: ${module}" >&2; exit 1; }
  [[ "$license" == "Apache-2.0" || "$license" == "MIT" ]] || { echo "unexpected query engine license: ${license}" >&2; exit 1; }
done < <(jq -r '.engines[] | select(.module != null) | [.module, .version, .license] | @tsv' "$LOCK_FILE")

prom_commit=$(jq -er '.engines[] | select(.language == "promql") | .commit' "$LOCK_FILE")
download=$(go mod download -json "github.com/prometheus/prometheus@v0.314.0")
[[ "$(jq -er '.Origin.Hash' <<<"$download")" == "$prom_commit" ]] || { echo "PromQL engine commit drift" >&2; exit 1; }
