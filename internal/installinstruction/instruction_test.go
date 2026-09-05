package installinstruction

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBuildPOSIXSerializesEmptyWarningsAsArray(t *testing.T) {
	result, err := BuildPOSIX(POSIXOptions{Scope: ScopeLinuxSystem,
		InstallerURL: "https://artifacts.example.com/install.sh", InstallerSHA256: strings.Repeat("a", 64),
		BootstrapScriptURL: "https://argus.example.com/api/v1/connectors/bootstrap-script", DownloadTLSMode: DownloadTLSStrict,
		TrustBundlePEM: testBundle(t), TrustBundleEpoch: 1, Token: "token", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"capability_warnings":[]`) {
		t.Fatalf("empty warnings must be a JSON array: %s", encoded)
	}
	if result.DownloadTLSMode != string(DownloadTLSStrict) || strings.Contains(result.Command, "--insecure") {
		t.Fatalf("strict download command relaxed TLS: %#v", result)
	}
}

func TestBuildPOSIXCreatesOneCommandAndStrictBootstrap(t *testing.T) {
	token := "secret-token-value"
	result, err := BuildPOSIX(POSIXOptions{Scope: ScopeLinuxSystem,
		InstallerURL: "https://artifacts.example.com/install.sh", InstallerSHA256: strings.Repeat("a", 64),
		BootstrapScriptURL: "https://argus.example.com/api/v1/connectors/bootstrap-script", DownloadTLSMode: DownloadTLSInsecureFirstFetch,
		TrustBundlePEM: testBundle(t), TrustBundleEpoch: 3, Token: token, ExpiresAt: time.Now().Add(time.Hour),
		InstallerArguments: []string{"--server", "https://argus.example.com", "--role", "bastion"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"--cacert \"$ARGUS_CA_FILE\"", "sha256sum -c -", "'--token-file' \"$ARGUS_TOKEN_FILE\"", "'--scope' 'linux-system'"} {
		if !strings.Contains(result.BootstrapScript, expected) {
			t.Fatalf("bootstrap script does not contain %q", expected)
		}
	}
	if !strings.Contains(result.BootstrapScript, token) {
		t.Fatal("bootstrap script does not contain the token")
	}
	if !strings.Contains(result.Command, "--insecure") || !strings.Contains(result.Command, "X-Argus-Enrollment-Token: "+token) ||
		!strings.Contains(result.Command, "scope=linux-system") || strings.Contains(result.Command, "\n") ||
		strings.Contains(result.Command, "curl -fsSL") {
		t.Fatalf("insecure first-fetch command is not a single authenticated download: %q", result.Command)
	}
	if result.DownloadTLSMode != string(DownloadTLSInsecureFirstFetch) {
		t.Fatalf("download TLS mode = %q", result.DownloadTLSMode)
	}
	if strings.Contains(result.BootstrapScript, "--insecure") || strings.Contains(result.BootstrapScript, "curl -k") || strings.Contains(result.BootstrapScript, "| sudo") || strings.Contains(result.BootstrapScript, " | bash ") || strings.Contains(result.BootstrapScript, " | sh ") {
		t.Fatalf("unsafe bootstrap generated:\n%s", result.BootstrapScript)
	}
}

func TestGeneratedPOSIXCommandsParse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell is not required on Windows")
	}
	result, err := BuildPOSIX(POSIXOptions{Scope: ScopeLinuxUser,
		InstallerURL: "https://artifacts.example.com/install.sh", InstallerSHA256: strings.Repeat("a", 64),
		BootstrapScriptURL: "https://argus.example.com/api/v1/connectors/bootstrap-script",
		TrustBundlePEM:     testBundle(t), TrustBundleEpoch: 1, Token: "token", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	for name, command := range map[string]string{"command": result.Command, "bootstrap": result.BootstrapScript} {
		check := exec.Command("sh", "-n")
		check.Stdin = strings.NewReader(command)
		if output, err := check.CombinedOutput(); err != nil {
			t.Fatalf("%s command does not parse: %v: %s", name, err, output)
		}
	}
}

func TestBuildPOSIXCreatesOneKubernetesCommandWithoutTLSRelaxation(t *testing.T) {
	result, err := BuildPOSIX(POSIXOptions{Scope: ScopeKubernetes,
		InstallerURL: "https://artifacts.example.com/install.sh", InstallerSHA256: strings.Repeat("a", 64),
		TrustBundlePEM: testBundle(t), TrustBundleEpoch: 1, Token: "token", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Command == "" || strings.Contains(result.Command, "\n") || result.DownloadTLSMode != "" {
		t.Fatalf("Kubernetes did not receive one safe command: %#v", result)
	}
	if strings.Contains(result.Command, "--insecure") || strings.Contains(result.Command, " | sh") {
		t.Fatalf("Kubernetes command relaxed TLS or piped into sh: %q", result.Command)
	}
}

func TestInsecureFirstFetchCommandDownloadsThenExecutes(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is required to execute the generated bootstrap command")
	}
	requestSeen := make(chan bool, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestSeen <- request.Header.Get("X-Argus-Enrollment-Token") == "one-time-token" &&
			request.URL.Query().Get("scope") == "linux-system"
		_, _ = writer.Write([]byte("printf 'bootstrap-executed'\n"))
	}))
	defer server.Close()

	command := bootstrapDownloadCommand(server.URL+"/bootstrap?scope=linux-system", "one-time-token", DownloadTLSInsecureFirstFetch)
	process := exec.Command("sh", "-c", command)
	output, err := process.CombinedOutput()
	if err != nil {
		t.Fatalf("execute generated one-line command: %v: %s", err, output)
	}
	if string(output) != "bootstrap-executed" || !<-requestSeen {
		t.Fatalf("generated command output/header mismatch: output=%q", output)
	}
}

func TestBuildPOSIXRejectsInvalidTrustAndHTTP(t *testing.T) {
	base := POSIXOptions{Scope: ScopeLinuxUser, InstallerURL: "http://example.com/install.sh", BootstrapScriptURL: "https://example.com/bootstrap-script", InstallerSHA256: strings.Repeat("0", 64),
		TrustBundlePEM: testBundle(t), TrustBundleEpoch: 1, Token: "token", ExpiresAt: time.Now().Add(time.Hour)}
	if _, err := BuildPOSIX(base); err == nil {
		t.Fatal("HTTP installer was accepted")
	}
	base.InstallerURL = "https://example.com/install.sh"
	base.TrustBundlePEM = []byte("not pem")
	if _, err := BuildPOSIX(base); err == nil {
		t.Fatal("invalid Trust Bundle was accepted")
	}
}

func testBundle(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Argus Test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})
}
