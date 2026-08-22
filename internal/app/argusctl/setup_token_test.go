package argusctl

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
)

func TestEnsureSetupTokenCreatesAndOnlyReturnsNewValueOnce(t *testing.T) {
	clients := &kubeClients{typed: fake.NewSimpleClientset()}
	created, err := ensureSetupToken(context.Background(), clients, "argus-system", "generated-secrets")
	if err != nil {
		t.Fatal(err)
	}
	if !created.Created || created.Token == "" || !created.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("created token = %#v", created)
	}

	existing, err := ensureSetupToken(context.Background(), clients, "argus-system", "generated-secrets")
	if err != nil {
		t.Fatal(err)
	}
	if existing.Created || existing.Token != "" || existing.ExpiresAt.Format(time.RFC3339) != created.ExpiresAt.Format(time.RFC3339) {
		t.Fatalf("existing token was exposed again: %#v", existing)
	}
}

func TestPrintSetupTokenRemindsOperatorToCopyIt(t *testing.T) {
	var output bytes.Buffer
	expiresAt := time.Date(2026, time.August, 23, 11, 7, 45, 0, time.UTC)
	printSetupToken(&output, "generated-token", expiresAt)

	for _, expected := range []string{"copy and store it now", "shown only once", "generated-token", expiresAt.Format(time.RFC3339)} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output %q does not contain %q", output.String(), expected)
		}
	}
}
