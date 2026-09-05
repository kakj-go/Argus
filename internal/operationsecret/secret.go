package operationsecret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
)

const KeyVersion int32 = 1

type Material struct {
	EnrollmentToken string `json:"enrollment_token"`
}

func Encrypt(key []byte, enterpriseID, operationID uuid.UUID, material Material) ([]byte, []byte, error) {
	if material.EnrollmentToken == "" {
		return nil, nil, errors.New("operation secret enrollment token is empty")
	}
	plaintext, err := json.Marshal(material)
	if err != nil {
		return nil, nil, err
	}
	defer clear(plaintext)
	aead, err := newAEAD(key)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return nonce, aead.Seal(nil, nonce, plaintext, aad(enterpriseID, operationID)), nil
}

func Decrypt(key, nonce, ciphertext []byte, enterpriseID, operationID uuid.UUID) (Material, error) {
	aead, err := newAEAD(key)
	if err != nil || len(nonce) != aead.NonceSize() {
		return Material{}, errors.New("operation secret envelope is invalid")
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad(enterpriseID, operationID))
	if err != nil {
		return Material{}, errors.New("operation secret envelope is invalid")
	}
	defer clear(plaintext)
	var material Material
	if json.Unmarshal(plaintext, &material) != nil || material.EnrollmentToken == "" {
		return Material{}, errors.New("operation secret payload is invalid")
	}
	return material, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("operation secret key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func aad(enterpriseID, operationID uuid.UUID) []byte {
	return []byte(fmt.Sprintf("argus.connector_install_operation_secret/v1\x00%s\x00%s", enterpriseID, operationID))
}
