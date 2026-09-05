#!/bin/sh
# Verified self-enrolled Collector installer. The caller supplies a dedicated
# Argus CA Bundle and a token file after verifying this script's SHA-256.
set -eu

TOKEN_FILE=""
BASE=""
CA_FILE=""
SCOPE="linux-system"
UNINSTALL=0
FOREGROUND=0
ELEVATED=0

while [ $# -gt 0 ]; do
  case "$1" in
    --token-file) TOKEN_FILE="${2-}"; shift 2 ;;
    --base) BASE="${2-}"; shift 2 ;;
    --ca-file) CA_FILE="${2-}"; shift 2 ;;
    --scope) SCOPE="${2-}"; shift 2 ;;
    --uninstall) UNINSTALL=1; shift ;;
    --foreground) FOREGROUND=1; shift ;;
    --elevated) ELEVATED=1; shift ;;
    *) echo "host-install: unknown argument $1" >&2; exit 2 ;;
  esac
done

log() { echo "argus host-install: $*" >&2; }
die() { log "$*"; exit 1; }
decode_raw_b64() {
  VALUE=$1
  case $((${#VALUE} % 4)) in 2) VALUE="${VALUE}==" ;; 3) VALUE="${VALUE}=" ;; esac
  printf '%s' "$VALUE" | base64 -d
}
https_url() { case "$1" in https://*) return 0 ;; *) return 1 ;; esac; }

[ "$SCOPE" = "linux-system" ] || [ "$SCOPE" = "linux-user" ] || die "scope must be linux-system or linux-user"
[ -r "$TOKEN_FILE" ] || die "a readable --token-file is required"
[ -r "$CA_FILE" ] || die "a readable --ca-file is required"
BASE=$(printf '%s' "$BASE" | sed 's:/*$::')
https_url "$BASE" || die "--base must be an HTTPS URL"
for required_command in curl sha256sum base64 sed openssl tar install; do
  command -v "$required_command" >/dev/null 2>&1 || die "$required_command is required"
done
openssl crl2pkcs7 -nocrl -certfile "$CA_FILE" >/dev/null 2>&1 || die "CA Bundle is not valid PEM"
TOKEN=$(sed -n '1p' "$TOKEN_FILE")
[ -n "$TOKEN" ] || die "bootstrap token is empty"

# Download and integrity verification happened without privilege. This is the
# first point at which a system installation may ask for sudo.
if [ "$SCOPE" = "linux-system" ] && [ "$(id -u)" -ne 0 ]; then
  command -v sudo >/dev/null 2>&1 || die "sudo is unavailable; select Linux user installation instead"
  set -- --token-file "$TOKEN_FILE" --base "$BASE" --ca-file "$CA_FILE" --scope "$SCOPE" --elevated
  [ "$UNINSTALL" -eq 1 ] && set -- "$@" --uninstall
  [ "$FOREGROUND" -eq 1 ] && set -- "$@" --foreground
  exec sudo sh "$0" "$@"
fi
[ "$ELEVATED" -eq 0 ] || [ "$(id -u)" -eq 0 ] || die "privilege elevation failed"

if [ "$SCOPE" = "linux-system" ]; then
  ROOT=/var/lib/argus-otelcol
  ETC=/etc/argus-otelcol
  BIN=/usr/local/bin/argus-otelcol
  UNIT=/etc/systemd/system/argus-otelcol.service
  SYSTEMCTL="systemctl"
  WANTED_BY=multi-user.target
else
  [ "$(id -u)" -ne 0 ] || die "Linux user installation must run as the target non-root user"
  ROOT="${XDG_DATA_HOME:-$HOME/.local/share}/argus-otelcol"
  ETC="${XDG_CONFIG_HOME:-$HOME/.config}/argus-otelcol"
  BIN="${XDG_BIN_HOME:-$HOME/.local/bin}/argus-otelcol"
  UNIT="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/argus-otelcol.service"
  SYSTEMCTL="systemctl --user"
  WANTED_BY=default.target
fi
STATE="$ROOT/state.json"

ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported architecture $ARCH" ;;
esac
HOSTNAME_VALUE=$(hostname 2>/dev/null || true)
ADDRESS=$( (command -v ip >/dev/null 2>&1 && ip -4 route get 1 2>/dev/null | sed -n 's/.*src \([0-9.]*\).*/\1/p' | head -1) || true )
if [ "$UNINSTALL" -eq 1 ]; then
  BOOTSTRAP_URL=$(printf '%s/v1/host-uninstall/%s' "$BASE" "$TOKEN")
  log "exchanging uninstall token"
else
  BOOTSTRAP_URL=$(printf '%s/v1/host-install/%s' "$BASE" "$TOKEN")
  log "exchanging install token"
fi
JSON=$(curl -fsSL --proto '=https' --tlsv1.2 --cacert "$CA_FILE" --max-time 30 \
  --get --data-urlencode "arch=$ARCH" --data-urlencode "hostname=$HOSTNAME_VALUE" --data-urlencode "address=$ADDRESS" \
  "$BOOTSTRAP_URL") || die "token exchange failed (invalid, expired, or consumed by another device)"

field() { printf '%s' "$JSON" | sed -n "s/.*\"$1\": *\"\([^\"]*\)\".*/\1/p" | head -1; }
number() { printf '%s' "$JSON" | sed -n "s/.*\"$1\": *\([0-9][0-9]*\).*/\1/p" | head -1; }
MODE=$(field mode)
DESIRED=$(number desired_revision || echo 0)

if [ "$UNINSTALL" -eq 1 ]; then
  COMPLETION_URL=$(field completion_url)
  COMPLETION_TOKEN=$(field completion_token)
  https_url "$COMPLETION_URL" || die "uninstall completion URL must use HTTPS"
  [ -n "$COMPLETION_TOKEN" ] || die "uninstall payload missing completion token"
  $SYSTEMCTL disable --now argus-otelcol.service >/dev/null 2>&1 || true
  rm -f "$UNIT" "$BIN"
  rm -rf "$ETC" "$ROOT"
  $SYSTEMCTL daemon-reload >/dev/null 2>&1 || true
  curl -fsSL --proto '=https' --tlsv1.2 --cacert "$CA_FILE" --max-time 30 -X POST \
    -H "X-Argus-Uninstall-Completion-Token: $COMPLETION_TOKEN" "$COMPLETION_URL" >/dev/null || \
    die "local cleanup succeeded but platform completion failed; rerun the same uninstall instruction"
  log "Collector uninstalled and confirmed"
  exit 0
fi

[ "$MODE" = "install" ] || die "unexpected bootstrap mode"
# A repeated bootstrap at the same configuration revision may still carry a
# newer Trust Bundle epoch or replacement identity. Never skip solely on the
# Collector configuration revision.

CONFIG_B64=$(field config_bundle)
CA_B64=$(field trust_bundle)
TRUST_BUNDLE_EPOCH=$(number trust_bundle_epoch || echo 0)
TRUST_BUNDLE_SHA256=$(field trust_bundle_sha256)
ENROLLMENT_TOKEN=$(field enrollment_token)
ENROLLMENT_ENDPOINT=$(field enrollment_endpoint)
GRPC_ENDPOINT=$(field ingest_grpc_endpoint)
HTTP_ENDPOINT=$(field ingest_http_endpoint)
ART_URI=$(field uri)
ART_SHA=$(field sha256)
ART_SIG=$(field signature)
ART_KEY_ID=$(field signing_key_id)
ART_PUB=$(field signing_public_key)
ART_SIZE=$(number byte_size)
[ -n "$CONFIG_B64" ] || die "bootstrap payload missing config"
[ -n "$CA_B64" ] || die "bootstrap payload missing Trust Bundle"
[ "$TRUST_BUNDLE_EPOCH" -ge 1 ] || die "bootstrap payload has an invalid Trust Bundle epoch"
case "$TRUST_BUNDLE_SHA256" in ''|*[!a-f0-9]*) die "bootstrap Trust Bundle digest is invalid" ;; esac
[ "${#TRUST_BUNDLE_SHA256}" -eq 64 ] || die "bootstrap Trust Bundle digest is invalid"
[ -n "$ENROLLMENT_TOKEN" ] || die "bootstrap payload missing enrollment token"
https_url "$ENROLLMENT_ENDPOINT" || die "enrollment endpoint must use HTTPS"
https_url "$HTTP_ENDPOINT" || die "ingest HTTP endpoint must use HTTPS"
https_url "$ART_URI" || die "artifact URL must use HTTPS"
case "$GRPC_ENDPOINT" in grpcs://*) ;; *) die "ingest gRPC endpoint must use grpcs" ;; esac

TMP=$(mktemp -d "${TMPDIR:-/tmp}/argus-host-install.XXXXXX")
trap 'rm -rf "$TMP"' EXIT HUP INT TERM
umask 077
printf '%s' "$CA_B64" | base64 -d > "$TMP/server-ca.pem" || die "Trust Bundle encoding rejected"
openssl crl2pkcs7 -nocrl -certfile "$TMP/server-ca.pem" >/dev/null 2>&1 || die "bootstrap Trust Bundle is invalid"
printf '%s  %s\n' "$TRUST_BUNDLE_SHA256" "$TMP/server-ca.pem" | sha256sum -c - >/dev/null 2>&1 || die "bootstrap Trust Bundle digest mismatch"

log "downloading Collector artifact with the current Trust Bundle"
ART_PATH="$TMP/artifact.tar.gz"
curl -fsSL --proto '=https' --tlsv1.2 --cacert "$TMP/server-ca.pem" --max-time 300 -o "$ART_PATH" "$ART_URI" || die "artifact download failed"
printf '%s  %s\n' "$ART_SHA" "$ART_PATH" | sha256sum -c - >/dev/null 2>&1 || die "artifact sha256 mismatch"
[ "$(wc -c < "$ART_PATH" | tr -d ' ')" = "$ART_SIZE" ] || die "artifact size mismatch"
[ -n "$ART_PUB" ] && [ -n "$ART_SIG" ] && [ -n "$ART_KEY_ID" ] || die "bootstrap payload missing artifact signature material"
decode_raw_b64 "$ART_PUB" > "$TMP/pub.raw" || die "signing public key encoding rejected"
[ "$(wc -c < "$TMP/pub.raw" | tr -d ' ')" = 32 ] || die "signing public key size rejected"
{ printf '\x30\x2a\x30\x05\x06\x03\x2b\x65\x70\x03\x21\x00'; cat "$TMP/pub.raw"; } > "$TMP/pub.der"
openssl pkey -pubin -inform DER -in "$TMP/pub.der" -out "$TMP/pub.pem" >/dev/null 2>&1 || die "signing public key rejected"
decode_raw_b64 "$ART_SIG" > "$TMP/art.sig" || die "artifact signature encoding rejected"
[ "$(wc -c < "$TMP/art.sig" | tr -d ' ')" = 64 ] || die "artifact signature size rejected"
openssl dgst -sha256 -binary "$ART_PATH" > "$TMP/art.hash"
openssl pkeyutl -verify -pubin -inkey "$TMP/pub.pem" -rawin -in "$TMP/art.hash" -sigfile "$TMP/art.sig" >/dev/null 2>&1 || \
  die "artifact ed25519 signature verification failed"

install -d -m 0700 "$ROOT" "$ROOT/release" "$ETC"
install -d -m 0755 "$(dirname "$BIN")" "$(dirname "$UNIT")"
printf '%s' "$CONFIG_B64" | base64 -d > "$ETC/config.yaml"
install -m 0600 "$TMP/server-ca.pem" "$ETC/server-ca.pem"
printf '%s' "$ENROLLMENT_TOKEN" > "$ETC/enrollment-token"
chmod 0600 "$ETC/config.yaml" "$ETC/enrollment-token"
find "$ROOT/release" -mindepth 1 -maxdepth 1 -delete
tar -xzf "$ART_PATH" -C "$ROOT/release"
[ -x "$ROOT/release/argus-otelcol" ] || die "artifact missing argus-otelcol binary"
install -m 0755 "$ROOT/release/argus-otelcol" "$BIN.tmp"
mv -f "$BIN.tmp" "$BIN"

printf '%s\n' \
  '[Unit]' 'Description=Argus managed OpenTelemetry Collector' 'After=network-online.target' 'Wants=network-online.target' '' \
  '[Service]' 'Type=simple' "Environment=ARGUS_TELEMETRY_ENROLLMENT_TOKEN_FILE=$ETC/enrollment-token" \
  "Environment=ARGUS_TELEMETRY_ENROLLMENT_ENDPOINT=$ENROLLMENT_ENDPOINT" \
  "Environment=ARGUS_TELEMETRY_INGEST_GRPC_ENDPOINT=$GRPC_ENDPOINT" \
  "Environment=ARGUS_TELEMETRY_INGEST_HTTP_ENDPOINT=$HTTP_ENDPOINT" \
  "ExecStart=$BIN --config=$ETC/config.yaml" 'Restart=always' 'RestartSec=5' 'NoNewPrivileges=true' 'PrivateTmp=true' \
  'ProtectSystem=strict' 'ProtectKernelTunables=true' 'ProtectKernelModules=true' 'ProtectControlGroups=true' 'RestrictSUIDSGID=true' '' \
  '[Install]' "WantedBy=$WANTED_BY" > "$UNIT"
printf '{"schema_version":"argus.collector_state/v3","effective_revision":%s,"transport":"bootstrap","trust_bundle_epoch":%s,"trust_bundle_sha256":"%s","updated_at":"%s"}\n' \
	"$DESIRED" "$TRUST_BUNDLE_EPOCH" "$TRUST_BUNDLE_SHA256" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$STATE"

if [ "$FOREGROUND" -eq 1 ]; then
  ARGUS_TELEMETRY_ENROLLMENT_TOKEN_FILE="$ETC/enrollment-token" \
  ARGUS_TELEMETRY_ENROLLMENT_ENDPOINT="$ENROLLMENT_ENDPOINT" \
  ARGUS_TELEMETRY_INGEST_GRPC_ENDPOINT="$GRPC_ENDPOINT" \
  ARGUS_TELEMETRY_INGEST_HTTP_ENDPOINT="$HTTP_ENDPOINT" \
  exec "$BIN" --config="$ETC/config.yaml"
fi
$SYSTEMCTL daemon-reload
$SYSTEMCTL enable --now argus-otelcol.service
sleep 2
$SYSTEMCTL is-active --quiet argus-otelcol.service || die "Collector service failed to start"
if [ "$SCOPE" = "linux-user" ] && ! loginctl show-user "$(id -un)" -p Linger 2>/dev/null | grep -q '=yes$'; then
  log "warning: systemd linger is disabled; the Collector stops after the last login session ends"
fi
log "Collector installed and running (revision $DESIRED)"
