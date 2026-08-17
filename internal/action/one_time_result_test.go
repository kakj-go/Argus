package action

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
)

func TestOneTimeResultEncryptionBindsExecutionIdentity(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{0x42}, 32)
	enterpriseID := uuid.New()
	executionID := uuid.New()
	plaintext := []byte(`{"enrollment":{"install_command":"install --token private"}}`)
	aad := oneTimeResultAAD(enterpriseID, executionID)

	nonce, ciphertext, err := encryptOneTimeResult(key, plaintext, aad)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte("private")) || bytes.Contains(ciphertext, []byte("install_command")) {
		t.Fatal("ciphertext contains one-time result plaintext")
	}
	decrypted, err := decryptOneTimeResult(key, nonce, ciphertext, aad)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted payload = %q", decrypted)
	}
	if _, err := decryptOneTimeResult(key, nonce, ciphertext, oneTimeResultAAD(enterpriseID, uuid.New())); err == nil {
		t.Fatal("ciphertext must not decrypt for another execution")
	}
}

func TestOneTimeResultEncryptionRejectsInvalidKey(t *testing.T) {
	t.Parallel()
	if _, _, err := encryptOneTimeResult([]byte("short"), []byte("payload"), []byte("aad")); err == nil {
		t.Fatal("invalid key length must fail closed")
	}
}

func TestOneTimeResultAuthorizationUsesPostCommitVersion(t *testing.T) {
	t.Parallel()

	if !oneTimeResultAuthorizationCurrent("active", 8, 8) {
		t.Fatal("the action's own authorization-version increment must not block claiming its result")
	}
	if oneTimeResultAuthorizationCurrent("active", 9, 8) {
		t.Fatal("authorization changes after result creation must invalidate the result")
	}
	if oneTimeResultAuthorizationCurrent("disabled", 8, 8) {
		t.Fatal("disabled users must not claim one-time results")
	}
}
