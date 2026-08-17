package resource

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
)

type actionCipher struct{ key []byte }

func (value actionCipher) encrypt(plaintext, aad []byte) ([]byte, []byte, error) {
	aead, err := value.aead()
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return nonce, aead.Seal(nil, nonce, plaintext, aad), nil
}

func (value actionCipher) decrypt(nonce, ciphertext, aad []byte) ([]byte, error) {
	aead, err := value.aead()
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, errors.New("invalid pending action nonce")
	}
	return aead.Open(nil, nonce, ciphertext, aad)
}

func (value actionCipher) aead() (cipher.AEAD, error) {
	if len(value.key) != 32 {
		return nil, errors.New("pending action encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(value.key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func newActionToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	clear(raw)
	return token, hash[:], nil
}

func verifyActionToken(token string, expected []byte) bool {
	hash := sha256.Sum256([]byte(token))
	return len(expected) == sha256.Size && subtle.ConstantTimeCompare(hash[:], expected) == 1
}

func actionAAD(enterpriseID, actionID string) []byte {
	return []byte(fmt.Sprintf("argus.pending_action_token/v1\x00%s\x00%s", enterpriseID, actionID))
}
