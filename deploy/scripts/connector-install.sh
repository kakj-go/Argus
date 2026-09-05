#!/bin/sh
# Verified installer for Linux and Kubernetes Connectors. The bootstrap that
# invokes this file has already verified its SHA-256; every network request
# still validates TLS with the supplied Argus Trust Bundle.
set -eu

MANIFEST=""
KEY_ID=""
PUBLIC_KEY=""
CONNECTOR_ID=""
TOKEN_FILE=""
SERVER=""
ROLE="bastion"
SCOPE="linux-system"
CA_FILE=""
REPLACE=0
ELEVATED=0
KUBERNETES_NAMESPACE="argus-system"
CONNECTOR_IMAGE=""
IMAGE_PULL_SECRETS=""

while [ $# -gt 0 ]; do
  case "$1" in
    --manifest) MANIFEST="${2-}"; shift 2 ;;
    --key-id) KEY_ID="${2-}"; shift 2 ;;
    --public-key) PUBLIC_KEY="${2-}"; shift 2 ;;
    --connector-id) CONNECTOR_ID="${2-}"; shift 2 ;;
    --token-file) TOKEN_FILE="${2-}"; shift 2 ;;
    --server) SERVER="${2-}"; shift 2 ;;
    --role) ROLE="${2-}"; shift 2 ;;
    --scope) SCOPE="${2-}"; shift 2 ;;
    --ca-file) CA_FILE="${2-}"; shift 2 ;;
    --replace) REPLACE=1; shift ;;
    --elevated) ELEVATED=1; shift ;;
    --kubernetes-namespace) KUBERNETES_NAMESPACE="${2-}"; shift 2 ;;
    --connector-image) CONNECTOR_IMAGE="${2-}"; shift 2 ;;
    --image-pull-secrets) IMAGE_PULL_SECRETS="${2-}"; shift 2 ;;
    *) echo "argus connector-install: unknown argument $1" >&2; exit 2 ;;
  esac
done

log() { echo "argus connector-install: $*" >&2; }
die() { log "$*"; exit 1; }
decode_raw_b64() {
  VALUE=$1
  case $((${#VALUE} % 4)) in 2) VALUE="${VALUE}==" ;; 3) VALUE="${VALUE}=" ;; esac
  printf '%s' "$VALUE" | base64 -d
}
https_url() { case "$1" in https://*) return 0 ;; *) return 1 ;; esac; }

[ -r "$TOKEN_FILE" ] || die "a readable --token-file is required"
[ -r "$CA_FILE" ] || die "a readable --ca-file is required"
[ -n "$MANIFEST" ] && [ -n "$KEY_ID" ] && [ -n "$PUBLIC_KEY" ] && [ -n "$CONNECTOR_ID" ] && [ -n "$SERVER" ] || \
  die "manifest, signing material, Connector ID, token file, server, and CA file are required"
https_url "$MANIFEST" || die "manifest URL must use HTTPS"
https_url "$SERVER" || die "Argus server URL must use HTTPS"
[ "$ROLE" = "bastion" ] || [ "$ROLE" = "kubernetes" ] || die "role must be bastion or kubernetes"
case "$SCOPE:$ROLE" in
  linux-system:bastion|linux-user:bastion|kubernetes:kubernetes) ;;
  *) die "installation scope is incompatible with Connector role" ;;
esac
printf '%s' "$KEY_ID" | grep -Eq '^[A-Za-z0-9._-]+$' || die "signing key id is invalid"
for required_command in curl sha256sum base64 openssl sed tr grep install; do
  command -v "$required_command" >/dev/null 2>&1 || die "$required_command is required"
done
openssl crl2pkcs7 -nocrl -certfile "$CA_FILE" >/dev/null 2>&1 || die "CA Bundle is not valid PEM"
TOKEN=$(sed -n '1p' "$TOKEN_FILE")
[ -n "$TOKEN" ] || die "enrollment token is empty"

# Privilege is requested only after arguments, dependencies, and trust
# material have passed validation.
if [ "$SCOPE" = "linux-system" ] && [ "$(id -u)" -ne 0 ]; then
  command -v sudo >/dev/null 2>&1 || die "sudo is unavailable; select Linux user installation instead"
  set -- --manifest "$MANIFEST" --key-id "$KEY_ID" --public-key "$PUBLIC_KEY" --connector-id "$CONNECTOR_ID" \
    --token-file "$TOKEN_FILE" --server "$SERVER" --role "$ROLE" --scope "$SCOPE" --ca-file "$CA_FILE" --elevated
  [ "$REPLACE" -eq 1 ] && set -- "$@" --replace
  exec sudo sh "$0" "$@"
fi
[ "$SCOPE" != "linux-system" ] || [ "$(id -u)" -eq 0 ] || die "system installation requires root"
[ "$ELEVATED" -eq 0 ] || [ "$(id -u)" -eq 0 ] || die "privilege elevation failed"

ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported architecture $ARCH" ;;
esac

TMP=$(mktemp -d "${TMPDIR:-/tmp}/argus-connector-install.XXXXXX")
trap 'rm -rf "$TMP"' EXIT HUP INT TERM
umask 077

log "downloading immutable release manifest with the Argus Trust Bundle"
curl -fsSL --proto '=https' --tlsv1.2 --cacert "$CA_FILE" --max-time 30 -o "$TMP/manifest.json" "$MANIFEST" || die "manifest download failed"
COMPACT=$(tr -d '\n\r ' < "$TMP/manifest.json")
printf '%s' "$COMPACT" | grep -F '"schema_version":"argus.connector_release/v2"' >/dev/null || die "manifest schema rejected"
printf '%s' "$COMPACT" | grep -F "\"signing_key_id\":\"$KEY_ID\"" >/dev/null || die "manifest signing key changed"
ENTRY=$(printf '%s' "$COMPACT" | sed 's/},{/}\
{/g' | grep -F "\"architecture\":\"$ARCH\"" | head -1)
[ -n "$ENTRY" ] || die "manifest has no linux/$ARCH artifact"
field() { printf '%s' "$ENTRY" | sed -n "s/.*\"$1\":\"\([^\"]*\)\".*/\1/p" | head -1; }
number() { printf '%s' "$ENTRY" | sed -n "s/.*\"$1\":\([0-9][0-9]*\).*/\1/p" | head -1; }
URI=$(field uri)
SHA=$(field sha256)
SIGNATURE=$(field signature)
ARTIFACT_KEY_ID=$(field signing_key_id)
SIZE=$(number byte_size)
https_url "$URI" || die "artifact URL must use HTTPS"
[ -n "$SHA" ] && [ -n "$SIGNATURE" ] && [ "$ARTIFACT_KEY_ID" = "$KEY_ID" ] && [ -n "$SIZE" ] || die "artifact manifest entry rejected"

log "downloading and verifying Connector artifact"
curl -fsSL --proto '=https' --tlsv1.2 --cacert "$CA_FILE" --max-time 300 -o "$TMP/argus-connector" "$URI" || die "artifact download failed"
printf '%s  %s\n' "$SHA" "$TMP/argus-connector" | sha256sum -c - >/dev/null 2>&1 || die "artifact sha256 mismatch"
[ "$(wc -c < "$TMP/argus-connector" | tr -d ' ')" = "$SIZE" ] || die "artifact size mismatch"
decode_raw_b64 "$PUBLIC_KEY" > "$TMP/public.raw" || die "public key encoding rejected"
[ "$(wc -c < "$TMP/public.raw" | tr -d ' ')" = 32 ] || die "public key size rejected"
{ printf '\x30\x2a\x30\x05\x06\x03\x2b\x65\x70\x03\x21\x00'; cat "$TMP/public.raw"; } > "$TMP/public.der"
openssl pkey -pubin -inform DER -in "$TMP/public.der" -out "$TMP/public.pem" >/dev/null 2>&1 || die "public key rejected"
decode_raw_b64 "$SIGNATURE" > "$TMP/signature" || die "signature encoding rejected"
[ "$(wc -c < "$TMP/signature" | tr -d ' ')" = 64 ] || die "signature size rejected"
openssl dgst -sha256 -binary "$TMP/argus-connector" > "$TMP/digest"
openssl pkeyutl -verify -pubin -inkey "$TMP/public.pem" -rawin -in "$TMP/digest" -sigfile "$TMP/signature" >/dev/null 2>&1 || \
  die "artifact Ed25519 signature verification failed"
chmod 0755 "$TMP/argus-connector"

if [ "$SCOPE" = "kubernetes" ]; then
  command -v kubectl >/dev/null 2>&1 || die "kubectl is required for Kubernetes installation"
  [ -n "$CONNECTOR_IMAGE" ] || die "--connector-image is required for Kubernetes installation"
  printf '%s' "$KUBERNETES_NAMESPACE" | grep -Eq '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$' || die "Kubernetes namespace is invalid"
  printf '%s' "$CONNECTOR_IMAGE" | grep -Eq '^[A-Za-z0-9._/:@-]+$' || die "Connector image reference is invalid"
  IMAGE_PULL_SECRETS_JSON='[]'
  if [ -n "$IMAGE_PULL_SECRETS" ]; then
    IMAGE_PULL_SECRETS_JSON='['
    OLD_IFS=$IFS
    IFS=,
    COUNT=0
    for SECRET_NAME in $IMAGE_PULL_SECRETS; do
      printf '%s' "$SECRET_NAME" | grep -Eq '^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$' || die "image pull Secret name is invalid"
      COUNT=$((COUNT + 1))
      [ "$COUNT" -le 16 ] || die "at most 16 image pull Secrets are supported"
      [ "$COUNT" -eq 1 ] || IMAGE_PULL_SECRETS_JSON="$IMAGE_PULL_SECRETS_JSON,"
      IMAGE_PULL_SECRETS_JSON="$IMAGE_PULL_SECRETS_JSON{\"name\":\"$SECRET_NAME\"}"
    done
    IFS=$OLD_IFS
    IMAGE_PULL_SECRETS_JSON="$IMAGE_PULL_SECRETS_JSON]"
  fi
  DATA="$TMP/identity"
  ARGUS_CONNECTOR_CA_FILE="$CA_FILE" "$TMP/argus-connector" enroll --connector-id "$CONNECTOR_ID" --token "$TOKEN" \
    --server "$SERVER" --role kubernetes --data-dir "$DATA"
  kubectl create namespace "$KUBERNETES_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "$KUBERNETES_NAMESPACE" create configmap argus-trust-bundle --from-file=ca.crt="$DATA/connector-ca.pem" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  kubectl -n "$KUBERNETES_NAMESPACE" create secret generic argus-connector-identity \
    --from-file=identity.json="$DATA/identity.json" --from-file=connector-key.pem="$DATA/connector-key.pem" \
    --from-file=connector-cert.pem="$DATA/connector-cert.pem" --from-file=connector-ca.pem="$DATA/connector-ca.pem" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  sed -e "s|ARGUS_CONNECTOR_IMAGE|$CONNECTOR_IMAGE|g" -e "s|ARGUS_NAMESPACE|$KUBERNETES_NAMESPACE|g" <<'EOF' | kubectl apply -f - >/dev/null
apiVersion: v1
kind: ServiceAccount
metadata: {name: argus-kubernetes-connector, namespace: ARGUS_NAMESPACE}
---
apiVersion: v1
kind: Namespace
metadata:
  name: argus-telemetry
  labels: {app.kubernetes.io/part-of: argus}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: argus-kubernetes-connector-reader}
rules:
- apiGroups: [""]
  resources: [namespaces,nodes,pods,pods/log,services,endpoints,events]
  verbs: [get,list,watch]
- apiGroups: [apps]
  resources: [deployments,daemonsets,statefulsets,replicasets]
  verbs: [get,list,watch]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: {name: argus-kubernetes-connector-reader}
subjects: [{kind: ServiceAccount, name: argus-kubernetes-connector, namespace: ARGUS_NAMESPACE}]
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: argus-kubernetes-connector-reader}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: {name: argus-kubernetes-connector-bundle, namespace: ARGUS_NAMESPACE}
rules:
- apiGroups: [""]
  resources: [configmaps]
  resourceNames: [argus-trust-bundle]
  verbs: [get,update,patch]
- apiGroups: [""]
  resources: [secrets]
  resourceNames: [argus-connector-identity]
  verbs: [get,update,patch]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: argus-kubernetes-connector-bundle, namespace: ARGUS_NAMESPACE}
subjects: [{kind: ServiceAccount, name: argus-kubernetes-connector, namespace: ARGUS_NAMESPACE}]
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: Role, name: argus-kubernetes-connector-bundle}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: {name: argus-kubernetes-connector-collector-repair, namespace: argus-telemetry}
rules:
- apiGroups: [""]
  resources: [configmaps]
  resourceNames: [argus-otelcol-config]
  verbs: [get,update,patch]
- apiGroups: [""]
  resources: [secrets]
  resourceNames: [argus-otelcol-identity]
  verbs: [get,update,patch]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: argus-kubernetes-connector-collector-repair, namespace: argus-telemetry}
subjects: [{kind: ServiceAccount, name: argus-kubernetes-connector, namespace: ARGUS_NAMESPACE}]
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: Role, name: argus-kubernetes-connector-collector-repair}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: {name: argus-kubernetes-connector-collector-manager, namespace: argus-telemetry}
rules:
- apiGroups: [""]
  resources: [configmaps]
  resourceNames: [argus-otelcol-config]
  verbs: [get,update,patch,delete]
- apiGroups: [""]
  resources: [secrets]
  resourceNames: [argus-otelcol-identity]
  verbs: [get,update,patch,delete]
- apiGroups: [""]
  resources: [serviceaccounts]
  resourceNames: [argus-otelcol]
  verbs: [get,update,patch,delete]
- apiGroups: [""]
  resources: [services]
  resourceNames: [argus-otelcol-gateway]
  verbs: [get,update,patch,delete]
- apiGroups: [apps]
  resources: [deployments]
  resourceNames: [argus-otelcol-gateway]
  verbs: [get,update,patch,delete]
- apiGroups: [apps]
  resources: [daemonsets]
  resourceNames: [argus-otelcol-agent]
  verbs: [get,update,patch,delete]
- apiGroups: [rbac.authorization.k8s.io]
  resources: [roles,rolebindings]
  resourceNames: [argus-otelcol-identity]
  verbs: [get,update,patch,delete]
- apiGroups: [""]
  resources: [configmaps,secrets,serviceaccounts,services]
  verbs: [create]
- apiGroups: [apps]
  resources: [deployments,daemonsets]
  verbs: [create]
- apiGroups: [rbac.authorization.k8s.io]
  resources: [roles,rolebindings]
  verbs: [create]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: argus-kubernetes-connector-collector-manager, namespace: argus-telemetry}
subjects: [{kind: ServiceAccount, name: argus-kubernetes-connector, namespace: ARGUS_NAMESPACE}]
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: Role, name: argus-kubernetes-connector-collector-manager}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: argus-otelcol-reader}
rules:
- apiGroups: [""]
  resources: [nodes,nodes/proxy,nodes/stats,namespaces,pods,services,endpoints,replicationcontrollers,resourcequotas]
  verbs: [get,list,watch]
- apiGroups: [apps]
  resources: [deployments,statefulsets,daemonsets,replicasets]
  verbs: [get,list,watch]
- apiGroups: [batch]
  resources: [jobs,cronjobs]
  verbs: [get,list,watch]
- apiGroups: [autoscaling]
  resources: [horizontalpodautoscalers]
  verbs: [get,list,watch]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: {name: argus-otelcol-reader}
subjects: [{kind: ServiceAccount, name: argus-otelcol, namespace: argus-telemetry}]
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: argus-otelcol-reader}
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: argus-kubernetes-connector, namespace: ARGUS_NAMESPACE}
spec:
  replicas: 1
  selector: {matchLabels: {app.kubernetes.io/name: argus-kubernetes-connector}}
  template:
    metadata: {labels: {app.kubernetes.io/name: argus-kubernetes-connector, app.kubernetes.io/part-of: argus}}
    spec:
      serviceAccountName: argus-kubernetes-connector
      securityContext: {runAsNonRoot: true, runAsUser: 10001, fsGroup: 10001}
      initContainers:
      - name: bootstrap-identity
        image: ARGUS_CONNECTOR_IMAGE
        command: [/usr/local/bin/argus-connector]
        args: [bootstrap-state, --source=/bootstrap, --data-dir=/state]
        securityContext: {allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: {drop: [ALL]}}
        volumeMounts:
        - {name: identity-bootstrap, mountPath: /bootstrap, readOnly: true}
        - {name: identity-state, mountPath: /state}
      containers:
      - name: connector
        image: ARGUS_CONNECTOR_IMAGE
        command: [/usr/local/bin/argus-connector]
        args: [run, --data-dir=/var/lib/argus-connector]
        env:
        - {name: ARGUS_CONNECTOR_KUBERNETES_STATE, value: "1"}
        - {name: ARGUS_OTELCOL_ARTIFACT_CA_PATH, value: /var/run/secrets/argus/trust/ca.crt}
        - name: ARGUS_CONNECTOR_KUBERNETES_NAMESPACE
          valueFrom: {fieldRef: {fieldPath: metadata.namespace}}
        securityContext: {allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: {drop: [ALL]}}
        volumeMounts:
        - {name: identity-state, mountPath: /var/lib/argus-connector}
        - {name: trust, mountPath: /var/run/secrets/argus/trust, readOnly: true}
        readinessProbe:
          exec: {command: [/usr/local/bin/argus-connector, probe, --data-dir=/var/lib/argus-connector]}
          initialDelaySeconds: 5
          periodSeconds: 10
      volumes:
      - name: identity-bootstrap
        secret: {secretName: argus-connector-identity, defaultMode: 0440}
      - name: identity-state
        emptyDir: {}
      - name: trust
        configMap: {name: argus-trust-bundle, defaultMode: 0444}
EOF
  if [ "$IMAGE_PULL_SECRETS_JSON" != '[]' ]; then
    kubectl -n "$KUBERNETES_NAMESPACE" patch deployment argus-kubernetes-connector --type merge \
      -p "{\"spec\":{\"template\":{\"spec\":{\"imagePullSecrets\":$IMAGE_PULL_SECRETS_JSON}}}}" >/dev/null
  fi
  kubectl -n "$KUBERNETES_NAMESPACE" rollout status deployment/argus-kubernetes-connector --timeout=180s
  log "Kubernetes Connector installed with namespace-scoped Argus trust material"
  exit 0
fi

if [ "$SCOPE" = "linux-system" ]; then
  ROOT=/var/lib/argus-connector
  ETC=/etc/argus-connector
  BIN=/usr/local/bin/argus-connector
  SYSTEMCTL="systemctl"
  UNIT=/etc/systemd/system/argus-connector.service
  WANTED_BY=multi-user.target
  if ! id argus-connector >/dev/null 2>&1; then
    command -v useradd >/dev/null 2>&1 || die "useradd is required"
    useradd --system --home-dir "$ROOT" --shell /usr/sbin/nologin argus-connector
  fi
  install -d -m 0700 -o argus-connector -g argus-connector "$ROOT"
  install -d -m 0755 "$ETC"
else
  [ "$(id -u)" -ne 0 ] || die "Linux user installation must run as the target non-root user"
  command -v systemctl >/dev/null 2>&1 || die "systemd user services are required"
  ROOT="${XDG_DATA_HOME:-$HOME/.local/share}/argus-connector"
  ETC="${XDG_CONFIG_HOME:-$HOME/.config}/argus-connector"
  BIN="${XDG_BIN_HOME:-$HOME/.local/bin}/argus-connector"
  SYSTEMCTL="systemctl --user"
  UNIT="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/argus-connector.service"
  WANTED_BY=default.target
  install -d -m 0700 "$ROOT" "$ETC"
  install -d -m 0755 "$(dirname "$BIN")" "$(dirname "$UNIT")"
fi

TRUST="$ETC/ca.pem"
install -m 0600 "$CA_FILE" "$TRUST"
install -m 0755 "$TMP/argus-connector" "$BIN.tmp"
mv -f "$BIN.tmp" "$BIN"
printf '{"%s":"%s"}\n' "$KEY_ID" "$PUBLIC_KEY" > "$ETC/otelcol-signing-keys.json"
chmod 0600 "$ETC/otelcol-signing-keys.json"

MARKER="$ROOT/.desired-connector-id"
CURRENT_ID=""
[ -f "$MARKER" ] && CURRENT_ID=$(sed -n '1p' "$MARKER")
if [ "$REPLACE" -eq 1 ] && [ "$CURRENT_ID" != "$CONNECTOR_ID" ]; then
  $SYSTEMCTL disable --now argus-connector.service >/dev/null 2>&1 || true
  find "$ROOT" -mindepth 1 -maxdepth 1 -type f -delete
fi
printf '%s' "$CONNECTOR_ID" > "$MARKER"

log "enrolling Connector with strict CA and hostname verification"
ARGUS_CONNECTOR_CA_FILE="$TRUST" "$BIN" enroll --connector-id "$CONNECTOR_ID" --token "$TOKEN" --server "$SERVER" --role bastion --data-dir "$ROOT"

if [ "$SCOPE" = "linux-system" ]; then
  chown -R argus-connector:argus-connector "$ROOT" "$ETC"
  SERVICE_USER="User=argus-connector
Group=argus-connector"
else
  SERVICE_USER=""
fi
printf '%s\n' \
  '[Unit]' 'Description=Argus Bastion Connector' 'After=network-online.target' 'Wants=network-online.target' '' \
  '[Service]' 'Type=simple' "$SERVICE_USER" "Environment=ARGUS_OTELCOL_SIGNING_PUBLIC_KEYS_FILE=$ETC/otelcol-signing-keys.json" \
  "Environment=ARGUS_OTELCOL_ARTIFACT_CA_PATH=$TRUST" \
  "ExecStart=$BIN run --data-dir=$ROOT" 'Restart=always' 'RestartSec=5' 'NoNewPrivileges=true' 'PrivateTmp=true' \
  'ProtectSystem=strict' 'ProtectKernelTunables=true' 'ProtectKernelModules=true' 'ProtectControlGroups=true' 'RestrictSUIDSGID=true' '' \
  '[Install]' "WantedBy=$WANTED_BY" > "$UNIT"
$SYSTEMCTL daemon-reload
$SYSTEMCTL enable --now argus-connector.service
$SYSTEMCTL is-active --quiet argus-connector.service || die "Connector service failed to start"
if [ "$SCOPE" = "linux-user" ] && ! loginctl show-user "$(id -un)" -p Linger 2>/dev/null | grep -q '=yes$'; then
  log "warning: systemd linger is disabled; the Connector stops after the last login session ends"
fi
log "Connector installed, enrolled, and running"
