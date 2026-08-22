#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR"

VERSION=${ARGUS_RELEASE_VERSION:-local-$(date -u +%Y%m%dT%H%M%SZ)}
OUT="artifacts/m8-release/${VERSION}"
TMP=$(mktemp -d)
BACKEND_IMAGE="argus/argus-backend:${VERSION}"
WEB_IMAGE="argus/argus-web:${VERSION}"
cleanup() {
  docker image rm "$BACKEND_IMAGE" "$WEB_IMAGE" >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
generate_spdx_sbom() {
  local image=$1
  local output=$2

  if docker sbom --help 2>&1 | rg -q 'spdx-json'; then
    docker sbom "$image" --format spdx-json >"$output"
  elif docker scout sbom --help 2>&1 | rg -q -- '--format'; then
    docker scout sbom --format spdx "local://$image" >"$output"
  else
    echo "an SPDX-capable docker sbom or docker scout plugin is required" >&2
    return 1
  fi

  jq -e '.spdxVersion | startswith("SPDX-")' "$output" >/dev/null
}
trap cleanup EXIT
umask 077
mkdir -p "$OUT/bin" "$OUT/charts" "$OUT/images" "$OUT/sbom" "$OUT/signatures"

echo "running local release gates"
make contract-check contract-breaking query-parser-check
go test ./...
go vet ./...
pnpm typecheck
pnpm lint
pnpm test
pnpm build
pnpm check:bundle
pnpm check:real-build
pnpm e2e
make check-production-artifacts
git diff --check

for name in argus-server argus-worker argus-connector-gateway argus-telemetry argusctl argus-migrate; do
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o "$OUT/bin/$name" "./cmd/$name"
done

go list -m -json all >"$OUT/go-modules.sbom.jsonl"
pnpm licenses list --json >"$OUT/web-licenses.json"
if rg -i '"license"[[:space:]]*:[[:space:]]*"(AGPL|GPL)(-|\"|$)' "$OUT/web-licenses.json"; then
  echo "disallowed strong-copyleft runtime dependency found" >&2
  exit 1
fi

if command -v govulncheck >/dev/null 2>&1; then
  govulncheck -json ./... >"$OUT/govulncheck.json"
else
  go tool govulncheck -json ./... >"$OUT/govulncheck.json"
fi

docker build --quiet -f deploy/docker/backend.Dockerfile -t "$BACKEND_IMAGE" . >/dev/null
docker build --quiet -f deploy/docker/web.Dockerfile -t "$WEB_IMAGE" \
  --build-arg VITE_API_MODE=real \
  --build-arg VITE_API_BASE_URL=/api \
  --build-arg VITE_CARD_ORIGIN=http://localhost:4176 \
  --build-arg VITE_PLATFORM_URL=http://localhost:4174/login . >/dev/null
docker save "$BACKEND_IMAGE" | gzip -9 >"$OUT/images/argus-backend-linux-arm64.tar.gz"
docker save "$WEB_IMAGE" | gzip -9 >"$OUT/images/argus-web-linux-arm64.tar.gz"
generate_spdx_sbom "$BACKEND_IMAGE" "$OUT/sbom/argus-backend.spdx.json"
generate_spdx_sbom "$WEB_IMAGE" "$OUT/sbom/argus-web.spdx.json"

for chart in deploy/helm/*; do
  if [[ -f "$chart/Chart.yaml" ]]; then
    helm package "$chart" --destination "$OUT/charts" >/dev/null
  fi
done

git rev-parse HEAD >"$OUT/commit.txt"
printf '%s\n' "$VERSION" >"$OUT/version.txt"

openssl genpkey -algorithm ED25519 -out "$TMP/local-signing-key.pem" >/dev/null 2>&1
openssl pkey -in "$TMP/local-signing-key.pem" -pubout -out "$OUT/local-signing-public.pem" >/dev/null 2>&1
for artifact in "$OUT"/images/*.tar.gz "$OUT"/charts/*.tgz; do
  name=$(basename "$artifact")
  openssl pkeyutl -sign -rawin -inkey "$TMP/local-signing-key.pem" -in "$artifact" -out "$OUT/signatures/${name}.sig"
  openssl pkeyutl -verify -rawin -pubin -inkey "$OUT/local-signing-public.pem" -in "$artifact" -sigfile "$OUT/signatures/${name}.sig" >/dev/null
done

tar -czf "$OUT/argus-${VERSION}-linux-arm64.tar.gz" -C "$OUT" bin
cat >"$OUT/release.json" <<EOF
{
  "version": "${VERSION}",
  "completion_state": "local_hardening_complete",
  "platform": "linux/arm64",
  "production_ready": false,
  "production_profile_installable": false
}
EOF

find api/openapi/generated migrations deploy/helm "$OUT/bin" "$OUT/charts" "$OUT/images" "$OUT/sbom" "$OUT/signatures" \
  "$OUT/argus-${VERSION}-linux-arm64.tar.gz" "$OUT/release.json" -type f -print0 | sort -z | xargs -0 shasum -a 256 >"$OUT/offline-manifest.sha256"
openssl pkeyutl -sign -rawin -inkey "$TMP/local-signing-key.pem" -in "$OUT/offline-manifest.sha256" -out "$OUT/offline-manifest.sig"
openssl pkeyutl -verify -rawin -pubin -inkey "$OUT/local-signing-public.pem" -in "$OUT/offline-manifest.sha256" -sigfile "$OUT/offline-manifest.sig" >/dev/null
find "$OUT" -type d -exec chmod 700 {} +
find "$OUT" -type f -exec chmod 600 {} +
echo "local release evidence: $OUT"
