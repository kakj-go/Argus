// Package installinstruction builds self-contained, fail-closed installation
// instructions. The generated bootstrap never changes the operating-system
// trust store and never executes bytes received directly from a pipe.
package installinstruction

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Scope string

type DownloadTLSMode string

const (
	ScopeLinuxSystem Scope = "linux-system"
	ScopeLinuxUser   Scope = "linux-user"
	ScopeKubernetes  Scope = "kubernetes"

	DownloadTLSStrict             DownloadTLSMode = "strict"
	DownloadTLSInsecureFirstFetch DownloadTLSMode = "insecure-first-fetch"
)

type Set struct {
	Scope              Scope     `json:"scope"`
	Command            string    `json:"command"`
	DownloadTLSMode    string    `json:"download_tls_mode,omitempty"`
	ExpiresAt          time.Time `json:"expires_at"`
	TrustBundleEpoch   int64     `json:"trust_bundle_epoch"`
	TrustBundleSHA256  string    `json:"trust_bundle_sha256"`
	InstallerSHA256    string    `json:"installer_sha256"`
	CapabilityWarnings []string  `json:"capability_warnings"`

	// BootstrapScript is generated only while serving an authenticated dynamic
	// bootstrap request. It is never serialized into one-time result payloads.
	BootstrapScript string `json:"-"`
}

type POSIXOptions struct {
	Scope              Scope
	InstallerURL       string
	BootstrapScriptURL string
	DownloadTLSMode    DownloadTLSMode
	InstallerSHA256    string
	TrustBundlePEM     []byte
	TrustBundleEpoch   int64
	Token              string
	ExpiresAt          time.Time
	InstallerArguments []string
	CapabilityWarnings []string
}

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// BuildPOSIX creates the single user-facing install command and the strict
// bootstrap script served behind it. Arguments must not contain the enrollment
// token; the script passes it through a mode-0600 temporary file.
func BuildPOSIX(options POSIXOptions) (Set, error) {
	if options.Scope != ScopeLinuxSystem && options.Scope != ScopeLinuxUser && options.Scope != ScopeKubernetes {
		return Set{}, errors.New("installation scope is invalid")
	}
	parsed, err := url.Parse(strings.TrimSpace(options.InstallerURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return Set{}, errors.New("installer URL must be an absolute HTTPS URL without user info")
	}
	var command string
	downloadTLSMode := options.DownloadTLSMode
	if strings.TrimSpace(options.BootstrapScriptURL) != "" {
		bootstrapURL, bootstrapErr := url.Parse(strings.TrimSpace(options.BootstrapScriptURL))
		if bootstrapErr != nil || bootstrapURL.Scheme != "https" || bootstrapURL.Host == "" || bootstrapURL.User != nil {
			return Set{}, errors.New("bootstrap script URL must be an absolute HTTPS URL without user info")
		}
		if downloadTLSMode == "" {
			downloadTLSMode = DownloadTLSStrict
		}
		if downloadTLSMode != DownloadTLSStrict && downloadTLSMode != DownloadTLSInsecureFirstFetch {
			return Set{}, errors.New("bootstrap download TLS mode is invalid")
		}
		query := bootstrapURL.Query()
		query.Set("scope", string(options.Scope))
		bootstrapURL.RawQuery = query.Encode()
		command = bootstrapDownloadCommand(bootstrapURL.String(), options.Token, downloadTLSMode)
	} else if downloadTLSMode != "" {
		return Set{}, errors.New("bootstrap download TLS mode requires a bootstrap script URL")
	}
	digest := strings.ToLower(strings.TrimSpace(options.InstallerSHA256))
	if !sha256Pattern.MatchString(digest) {
		return Set{}, errors.New("installer SHA-256 is invalid")
	}
	canonical, err := canonicalCABundle(options.TrustBundlePEM)
	if err != nil {
		return Set{}, err
	}
	if options.TrustBundleEpoch < 1 || strings.TrimSpace(options.Token) == "" || options.ExpiresAt.IsZero() {
		return Set{}, errors.New("installation token, expiration, and positive Trust Bundle epoch are required")
	}
	for _, argument := range options.InstallerArguments {
		if strings.ContainsRune(argument, '\x00') {
			return Set{}, errors.New("installer argument contains NUL")
		}
	}
	bundleDigest := sha256.Sum256(canonical)
	bundleBase64 := base64.StdEncoding.EncodeToString(canonical)
	base := bootstrapPrefix(parsed.String(), digest, bundleBase64)
	args := append([]string{"--scope", string(options.Scope), "--ca-file", `"$ARGUS_CA_FILE"`, "--token-file", `"$ARGUS_TOKEN_FILE"`}, options.InstallerArguments...)
	commandSuffix := "sh \"$ARGUS_INSTALLER\" " + shellArguments(args)
	bootstrapScript := base + "\nprintf '%s' " + quote(options.Token) + " > \"$ARGUS_TOKEN_FILE\"\n" + commandSuffix
	if command == "" {
		command = inlineScriptCommand(bootstrapScript)
	}
	return Set{
		Scope: options.Scope, Command: command, DownloadTLSMode: string(downloadTLSMode),
		ExpiresAt: options.ExpiresAt.UTC(), TrustBundleEpoch: options.TrustBundleEpoch,
		TrustBundleSHA256: hex.EncodeToString(bundleDigest[:]), InstallerSHA256: digest,
		CapabilityWarnings: append([]string{}, options.CapabilityWarnings...),
		BootstrapScript:    bootstrapScript,
	}, nil
}

// bootstrapDownloadCommand is the short, user-facing entry point. In
// insecure-first-fetch mode only this request skips server certificate
// verification; the downloaded script contains the versioned Trust Bundle and
// performs every subsequent request in strict mode.
func bootstrapDownloadCommand(scriptURL, token string, mode DownloadTLSMode) string {
	curlTLS := ""
	if mode == DownloadTLSInsecureFirstFetch {
		curlTLS = " --insecure" // ARGUS_INSECURE_FIRST_FETCH_ONLY
	}
	return `(set -eu; umask 077; ARGUS_BOOTSTRAP=$(mktemp "${TMPDIR:-/tmp}/argus-bootstrap.XXXXXX"); trap 'rm -f "$ARGUS_BOOTSTRAP"' EXIT HUP INT TERM; curl -fsS --proto '=https' --tlsv1.2` + curlTLS + ` --header ` + quote("X-Argus-Enrollment-Token: "+token) + ` --output "$ARGUS_BOOTSTRAP" ` + quote(scriptURL) + `; chmod 0700 "$ARGUS_BOOTSTRAP"; sh "$ARGUS_BOOTSTRAP")`
}

// inlineScriptCommand keeps Kubernetes enrollment on the same single-command
// contract when no authenticated bootstrap endpoint is available. The script
// is written to a mode-0700 temporary file before execution, never piped to sh.
func inlineScriptCommand(script string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(script + "\n"))
	return `(set -eu; umask 077; ARGUS_BOOTSTRAP=$(mktemp "${TMPDIR:-/tmp}/argus-bootstrap.XXXXXX"); trap 'rm -f "$ARGUS_BOOTSTRAP"' EXIT HUP INT TERM; printf '%s' ` + quote(encoded) + ` | base64 -d > "$ARGUS_BOOTSTRAP"; chmod 0700 "$ARGUS_BOOTSTRAP"; sh "$ARGUS_BOOTSTRAP")`
}

func bootstrapPrefix(installerURL, installerSHA256, bundleBase64 string) string {
	return `set -eu
umask 077
ARGUS_INSTALL_DIR=$(mktemp -d "${TMPDIR:-/tmp}/argus-install.XXXXXX")
ARGUS_CA_FILE="$ARGUS_INSTALL_DIR/argus-ca.pem"
ARGUS_INSTALLER="$ARGUS_INSTALL_DIR/install.sh"
ARGUS_TOKEN_FILE="$ARGUS_INSTALL_DIR/token"
argus_cleanup() { rm -rf "$ARGUS_INSTALL_DIR"; }
trap argus_cleanup EXIT HUP INT TERM
printf '%s' ` + quote(bundleBase64) + ` | base64 -d > "$ARGUS_CA_FILE"
curl -fsSL --proto '=https' --tlsv1.2 --cacert "$ARGUS_CA_FILE" --output "$ARGUS_INSTALLER" ` + quote(installerURL) + `
printf '%s  %s\n' ` + quote(installerSHA256) + ` "$ARGUS_INSTALLER" | sha256sum -c - >/dev/null
chmod 0700 "$ARGUS_INSTALLER"`
}

func shellArguments(arguments []string) string {
	values := make([]string, 0, len(arguments))
	for _, value := range arguments {
		if strings.HasPrefix(value, `"$ARGUS_`) && strings.HasSuffix(value, `"`) {
			values = append(values, value)
		} else {
			values = append(values, quote(value))
		}
	}
	return strings.Join(values, " ")
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func canonicalCABundle(value []byte) ([]byte, error) {
	rest := bytes.TrimSpace(value)
	seen := map[[32]byte]struct{}{}
	var output bytes.Buffer
	count := 0
	now := time.Now().UTC()
	for len(rest) > 0 {
		block, remaining := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, errors.New("Trust Bundle must contain only PEM certificates")
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !certificate.IsCA || !certificate.BasicConstraintsValid || certificate.KeyUsage&x509.KeyUsageCertSign == 0 {
			return nil, errors.New("Trust Bundle contains a certificate that is not a valid CA")
		}
		if now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
			return nil, errors.New("Trust Bundle contains an expired or not-yet-valid CA")
		}
		digest := sha256.Sum256(block.Bytes)
		if _, exists := seen[digest]; exists {
			return nil, fmt.Errorf("Trust Bundle contains duplicate CA %s", hex.EncodeToString(digest[:]))
		}
		seen[digest] = struct{}{}
		_ = pem.Encode(&output, &pem.Block{Type: "CERTIFICATE", Bytes: block.Bytes})
		count++
		rest = bytes.TrimSpace(remaining)
	}
	if count == 0 {
		return nil, errors.New("Trust Bundle contains no CA certificates")
	}
	return output.Bytes(), nil
}
