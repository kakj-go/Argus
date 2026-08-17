package secret

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvelopeRoundTripAndAAD(t *testing.T) {
	keyring := testKeyring(t, 1)
	aad := []byte("enterprise/secret/version/type")
	envelope, err := keyring.Encrypt([]byte("private-value"), aad)
	if err != nil {
		t.Fatal(err)
	}
	value, err := keyring.Decrypt(envelope, aad)
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "private-value" {
		t.Fatalf("unexpected plaintext %q", value)
	}
	if _, err := keyring.Decrypt(envelope, []byte("another-resource")); err == nil {
		t.Fatal("expected AAD mismatch to fail")
	}
}

func TestEnvelopeRewrapDoesNotChangeCiphertext(t *testing.T) {
	old := testKeyring(t, 1)
	aad := []byte("tenant/secret/1/password")
	envelope, err := old.Encrypt([]byte("value"), aad)
	if err != nil {
		t.Fatal(err)
	}
	combined := Keyring{currentVersion: 2, keys: map[int][]byte{1: old.keys[1], 2: make([]byte, keySize)}}
	rewrapped, err := combined.Rewrap(envelope, aad)
	if err != nil {
		t.Fatal(err)
	}
	if rewrapped.KeyVersion != 2 || string(rewrapped.Ciphertext) != string(envelope.Ciphertext) || string(rewrapped.Nonce) != string(envelope.Nonce) {
		t.Fatal("rewrap changed the encrypted secret payload")
	}
	if _, err := combined.Decrypt(rewrapped, aad); err != nil {
		t.Fatal(err)
	}
}

func testKeyring(t *testing.T, current int) Keyring {
	t.Helper()
	key := make([]byte, keySize)
	for i := range key {
		key[i] = byte(i + current)
	}
	path := filepath.Join(t.TempDir(), "keyring.json")
	data := fmt.Sprintf(`{"current_version":%d,"keys":{"%d":"%s"}}`, current, current, base64.RawURLEncoding.EncodeToString(key))
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	keyring, err := LoadKeyring(path)
	if err != nil {
		t.Fatal(err)
	}
	return keyring
}
