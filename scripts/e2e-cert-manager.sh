#!/usr/bin/env bash

CERT_MANAGER_INSTALLED_BY_E2E=false

cert_manager_version_compatible() {
  local actual=${1#v} required=${2#v}
  local actual_major actual_minor actual_patch required_major required_minor required_patch
  IFS=. read -r actual_major actual_minor actual_patch <<<"$actual"
  IFS=. read -r required_major required_minor required_patch <<<"$required"
  [[ "$actual_major" =~ ^[0-9]+$ && "$actual_minor" =~ ^[0-9]+$ && "$actual_patch" =~ ^[0-9]+$ ]] || return 1
  [[ "$required_major" =~ ^[0-9]+$ && "$required_minor" =~ ^[0-9]+$ && "$required_patch" =~ ^[0-9]+$ ]] || return 1
  [[ "$actual_major" -eq "$required_major" && "$actual_minor" -eq "$required_minor" && "$actual_patch" -ge "$required_patch" ]]
}

prepare_cert_manager_dependency() {
  local version image actual_version
  version=$(awk -F'"' '/certManager:/ {print $2}' "${ROOT_DIR}/deploy/versions.lock.yaml")
  [[ -n "$version" ]] || fail "cert-manager version is missing from deploy/versions.lock.yaml"
  if k -n cert-manager get deployment cert-manager >/dev/null 2>&1; then
    image=$(k -n cert-manager get deployment cert-manager -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null || true)
    actual_version=$(sed -E 's/.*:v?([0-9]+\.[0-9]+\.[0-9]+)(@.*)?$/\1/' <<<"$image")
    cert_manager_version_compatible "$actual_version" "$version" || \
      fail "existing cert-manager is not compatible with locked baseline v${version}: ${image:-unknown}"
    log "reusing compatible cert-manager v${actual_version} (baseline v${version})"
    return
  fi

  log "installing locked cert-manager v${version}"
  retry 3 helm upgrade --install argus-e2e-cert-manager "https://charts.jetstack.io/charts/cert-manager-v${version}.tgz" \
    --kube-context "$KUBE_CONTEXT" --namespace cert-manager --create-namespace \
    --set crds.enabled=true --wait --timeout 10m >/dev/null
  CERT_MANAGER_INSTALLED_BY_E2E=true
}

cleanup_cert_manager_dependency() {
  if [[ "$CERT_MANAGER_INSTALLED_BY_E2E" == true ]]; then
    helm uninstall argus-e2e-cert-manager --kube-context "$KUBE_CONTEXT" --namespace cert-manager --wait >/dev/null 2>&1 || true
    k delete namespace cert-manager --wait=false >/dev/null 2>&1 || true
  fi
}
