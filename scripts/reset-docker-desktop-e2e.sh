#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" != "--confirm-destructive-reset" ]]; then
  echo "Refusing destructive reset. Re-run with --confirm-destructive-reset." >&2
  exit 2
fi

command -v kubectl >/dev/null || { echo "kubectl is required" >&2; exit 127; }
command -v docker >/dev/null || { echo "docker is required" >&2; exit 127; }

context="$(kubectl config current-context 2>/dev/null || true)"
if [[ "$context" != "docker-desktop" ]]; then
  echo "Refusing reset: current Kubernetes context is '$context', expected 'docker-desktop'." >&2
  exit 3
fi

echo "Resources that will be affected in context docker-desktop:"
kubectl get namespace --no-headers 2>/dev/null || true
kubectl get all --all-namespaces --no-headers 2>/dev/null || true

if ! docker desktop kubernetes reset-cluster --help >/dev/null 2>&1; then
  echo "Docker Desktop CLI does not expose 'docker desktop kubernetes reset-cluster'; refusing partial cleanup." >&2
  exit 4
fi

node_uid_before="$(kubectl get node -o jsonpath='{.items[0].metadata.uid}' 2>/dev/null || true)"
if [[ -z "$node_uid_before" ]]; then
  echo "Refusing reset: unable to capture the current docker-desktop node identity." >&2
  exit 5
fi

echo "Starting Docker Desktop Kubernetes reset..."
docker desktop kubernetes reset-cluster
echo "Waiting for docker-desktop control plane..."
deadline=$((SECONDS + 600))
while (( SECONDS < deadline )); do
  if kubectl get node >/dev/null 2>&1; then
    break
  fi
  sleep 5
done
kubectl wait --for=condition=Ready node --all --timeout=10m

node_uid_after="$(kubectl get node -o jsonpath='{.items[0].metadata.uid}' 2>/dev/null || true)"
if [[ -z "$node_uid_after" || "$node_uid_after" == "$node_uid_before" ]]; then
  echo "Reset verification failed: Kubernetes node identity did not change." >&2
  exit 6
fi
if kubectl get namespace argus-system >/dev/null 2>&1; then
  echo "Reset verification failed: the previous argus-system namespace still exists." >&2
  exit 7
fi

while IFS=' ' read -r container_id container_name; do
  if [[ -n "$container_id" && "$container_name" == argus-registry-* ]]; then
    echo "Removing reset-owned local registry $container_name..."
    docker container rm --force "$container_id" >/dev/null
  fi
done < <(docker ps --all --filter label=argus.io/release-id --format '{{.ID}} {{.Names}}')

echo "Cluster reset complete. Deploy dependencies and Argus before running E2E."
