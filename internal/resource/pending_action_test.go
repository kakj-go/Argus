package resource

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func TestCanonicalJSONProducesStablePlanHash(t *testing.T) {
	left, err := canonicalJSON([]byte(`{"operation":"create","input":{"labels":{"team":"m3","route":"bastion"},"name":"m3-bastion"},"version":1}`))
	if err != nil {
		t.Fatal(err)
	}
	right, err := canonicalJSON([]byte(`{"version":1,"input":{"name":"m3-bastion","labels":{"route":"bastion","team":"m3"}},"operation":"create"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left, right) {
		t.Fatalf("canonical JSON differs:\n%s\n%s", left, right)
	}
	if sha256.Sum256(left) != sha256.Sum256(right) {
		t.Fatal("equivalent immutable plans produced different hashes")
	}
}

func TestCanonicalJSONPreservesIntegerPrecision(t *testing.T) {
	value, err := canonicalJSON([]byte(`{"expected_version":9223372036854775807}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != `{"expected_version":9223372036854775807}` {
		t.Fatalf("integer precision changed: %s", value)
	}
}
