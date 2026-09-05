package argusdev

import "testing"

func TestScanTLSProductionContentRejectsBypasses(t *testing.T) {
	bad := []string{
		"tls.Config{InsecureSkipVerify: true}",
		"insecure_skip_verify: true",
		"curl --insecure https://argus.example/install.sh",
		"curl -fsSLk https://argus.example/install.sh",
		"curl -fsSL https://argus.example/install.sh | sudo bash",
		"artifactTLSMode: insecure",
	}
	for _, value := range bad {
		if err := scanTLSProductionContent("fixture", []byte(value)); err == nil {
			t.Fatalf("TLS bypass was accepted: %s", value)
		}
	}
}

func TestScanTLSProductionContentAllowsStrictBootstrap(t *testing.T) {
	good := `curl -fsSL --proto '=https' --tlsv1.2 --cacert "$ARGUS_CA_FILE" --output "$ARGUS_INSTALLER" https://argus.example/install.sh
insecure_skip_verify: false
sha256sum -c checksums`
	if err := scanTLSProductionContent("fixture", []byte(good)); err != nil {
		t.Fatal(err)
	}
}

func TestScanTLSProductionContentAllowsOnlyScopedInsecureFirstFetch(t *testing.T) {
	approved := `curlTLS = " --insecure" // ARGUS_INSECURE_FIRST_FETCH_ONLY`
	if err := scanTLSProductionContent("internal/installinstruction/instruction.go", []byte(approved)); err != nil {
		t.Fatal(err)
	}
	if err := scanTLSProductionContent("fixture", []byte(approved)); err == nil {
		t.Fatal("scoped marker was accepted outside the reviewed source")
	}
}
