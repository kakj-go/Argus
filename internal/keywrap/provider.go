// Package keywrap provides versioned encryption for small secret material.
package keywrap

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

var ErrUnavailable = errors.New("key wrapping provider unavailable")

const (
	ProviderLocalTest      = "local_test"
	ProviderOpenBaoTransit = "openbao_transit"
)

type Ciphertext struct {
	Provider   string
	KeyID      string
	KeyVersion int32
	Value      []byte
}

type Provider interface {
	Encrypt(context.Context, []byte) (Ciphertext, error)
	Decrypt(context.Context, Ciphertext) ([]byte, error)
}

// Local is deliberately limited to tests and the Evaluation profile.
type Local struct {
	Key   []byte
	KeyID string
}

func (provider Local) Encrypt(_ context.Context, plaintext []byte) (Ciphertext, error) {
	block, err := aes.NewCipher(provider.Key)
	if err != nil {
		return Ciphertext{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Ciphertext{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Ciphertext{}, err
	}
	value := append(nonce, gcm.Seal(nil, nonce, plaintext, []byte(provider.KeyID))...)
	return Ciphertext{Provider: ProviderLocalTest, KeyID: provider.KeyID, KeyVersion: 1, Value: value}, nil
}

func (provider Local) Decrypt(_ context.Context, ciphertext Ciphertext) ([]byte, error) {
	if ciphertext.Provider != ProviderLocalTest || ciphertext.KeyID != provider.KeyID {
		return nil, errors.New("ciphertext key reference mismatch")
	}
	block, err := aes.NewCipher(provider.Key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext.Value) <= gcm.NonceSize() {
		return nil, errors.New("ciphertext is truncated")
	}
	return gcm.Open(nil, ciphertext.Value[:gcm.NonceSize()], ciphertext.Value[gcm.NonceSize():], []byte(provider.KeyID))
}

func Encode(value []byte) string { return base64.StdEncoding.EncodeToString(value) }

func Decode(value string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode key provider payload: %w", err)
	}
	return decoded, nil
}
