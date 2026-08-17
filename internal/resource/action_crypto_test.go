package resource

import "testing"

func TestActionTokenEncryptionAndHash(t *testing.T) {
	cipher := actionCipher{key: make([]byte, 32)}
	token, hash, err := newActionToken()
	if err != nil {
		t.Fatal(err)
	}
	aad := actionAAD("enterprise", "action")
	nonce, ciphertext, err := cipher.encrypt([]byte(token), aad)
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := cipher.decrypt(nonce, ciphertext, aad)
	if err != nil {
		t.Fatal(err)
	}
	if !verifyActionToken(string(decrypted), hash) {
		t.Fatal("token hash verification failed")
	}
	if _, err := cipher.decrypt(nonce, ciphertext, actionAAD("other", "action")); err == nil {
		t.Fatal("expected AAD mismatch")
	}
}
