package secret

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/kakj-go/Argus/internal/keywrap"
)

const (
	localProvider = "local"
	localKeyID    = "argus-secret-kek"
	keySize       = 32
)

var ErrUnknownKeyVersion = errors.New("unknown secret KEK version")

type keyringFile struct {
	CurrentVersion int               `json:"current_version"`
	Keys           map[string]string `json:"keys"`
}

type Keyring struct {
	currentVersion int
	keys           map[int][]byte
	provider       keywrap.Provider
}

type Envelope struct {
	Provider   string
	KeyID      string
	KeyVersion int
	WrappedDEK []byte
	WrapNonce  []byte
	Nonce      []byte
	Ciphertext []byte
	ValueHash  []byte
}

func LoadKeyring(path string) (Keyring, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Keyring{}, fmt.Errorf("read secret KEK keyring: %w", err)
	}
	var file keyringFile
	if err := json.Unmarshal(data, &file); err != nil {
		return Keyring{}, fmt.Errorf("decode secret KEK keyring: %w", err)
	}
	if file.CurrentVersion < 1 || len(file.Keys) == 0 {
		return Keyring{}, errors.New("secret KEK keyring has no current key")
	}
	keys := make(map[int][]byte, len(file.Keys))
	for rawVersion, encoded := range file.Keys {
		version, err := strconv.Atoi(rawVersion)
		if err != nil || version < 1 {
			return Keyring{}, fmt.Errorf("invalid secret KEK version %q", rawVersion)
		}
		key, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil || len(key) != keySize {
			return Keyring{}, fmt.Errorf("secret KEK version %d must be 32 bytes in base64url form", version)
		}
		keys[version] = key
	}
	if _, ok := keys[file.CurrentVersion]; !ok {
		return Keyring{}, errors.New("secret KEK current_version is not present in keys")
	}
	return Keyring{currentVersion: file.CurrentVersion, keys: keys}, nil
}

// NewProviderKeyring delegates DEK wrapping to a versioned external provider.
// Payload encryption remains local AES-256-GCM with resource-bound AAD.
func NewProviderKeyring(provider keywrap.Provider) (Keyring, error) {
	if provider == nil {
		return Keyring{}, keywrap.ErrUnavailable
	}
	return Keyring{provider: provider}, nil
}

// LoadConfiguredKeyring selects exactly one wrapping authority. External mode
// never falls back to the local key file when OpenBao is unavailable.
func LoadConfiguredKeyring(path, mode, address, token, keyID string) (Keyring, error) {
	switch mode {
	case "local_test":
		return LoadKeyring(path)
	case "openbao_transit":
		return NewProviderKeyring(keywrap.OpenBao{Address: address, Token: token, KeyID: keyID})
	default:
		return Keyring{}, fmt.Errorf("unsupported key wrapping mode %q", mode)
	}
}

func (keyring Keyring) Encrypt(plaintext, aad []byte) (Envelope, error) {
	return keyring.EncryptContext(context.Background(), plaintext, aad)
}

func (keyring Keyring) EncryptContext(ctx context.Context, plaintext, aad []byte) (Envelope, error) {
	dek := make([]byte, keySize)
	if _, err := rand.Read(dek); err != nil {
		return Envelope{}, fmt.Errorf("generate secret DEK: %w", err)
	}
	nonce, ciphertext, err := seal(dek, plaintext, aad)
	if err != nil {
		return Envelope{}, err
	}
	if keyring.provider != nil {
		wrapped, wrapErr := keyring.provider.Encrypt(ctx, dek)
		clear(dek)
		if wrapErr != nil {
			return Envelope{}, wrapErr
		}
		hash := sha256.Sum256(plaintext)
		return Envelope{Provider: wrapped.Provider, KeyID: wrapped.KeyID, KeyVersion: int(wrapped.KeyVersion), WrappedDEK: wrapped.Value,
			Nonce: nonce, Ciphertext: ciphertext, ValueHash: hash[:]}, nil
	}
	wrapAAD := append([]byte("argus.secret_dek/v1\x00"), aad...)
	wrapNonce, wrappedDEK, err := seal(keyring.keys[keyring.currentVersion], dek, wrapAAD)
	clear(dek)
	if err != nil {
		return Envelope{}, err
	}
	hash := sha256.Sum256(plaintext)
	return Envelope{Provider: localProvider, KeyID: localKeyID, KeyVersion: keyring.currentVersion,
		WrappedDEK: wrappedDEK, WrapNonce: wrapNonce, Nonce: nonce, Ciphertext: ciphertext, ValueHash: hash[:]}, nil
}

func (keyring Keyring) Decrypt(envelope Envelope, aad []byte) ([]byte, error) {
	return keyring.DecryptContext(context.Background(), envelope, aad)
}

func (keyring Keyring) DecryptContext(ctx context.Context, envelope Envelope, aad []byte) ([]byte, error) {
	var dek []byte
	var err error
	if keyring.provider != nil {
		dek, err = keyring.provider.Decrypt(ctx, keywrap.Ciphertext{Provider: envelope.Provider, KeyID: envelope.KeyID,
			KeyVersion: int32(envelope.KeyVersion), Value: envelope.WrappedDEK})
	} else {
		if envelope.Provider != localProvider || envelope.KeyID != localKeyID {
			return nil, errors.New("unsupported secret envelope provider")
		}
		kek, ok := keyring.keys[envelope.KeyVersion]
		if !ok {
			return nil, ErrUnknownKeyVersion
		}
		wrapAAD := append([]byte("argus.secret_dek/v1\x00"), aad...)
		dek, err = open(kek, envelope.WrapNonce, envelope.WrappedDEK, wrapAAD)
	}
	if err != nil {
		return nil, fmt.Errorf("unwrap secret DEK: %w", err)
	}
	defer clear(dek)
	plaintext, err := open(dek, envelope.Nonce, envelope.Ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("decrypt secret value: %w", err)
	}
	hash := sha256.Sum256(plaintext)
	if !equalBytes(hash[:], envelope.ValueHash) {
		clear(plaintext)
		return nil, errors.New("secret value hash mismatch")
	}
	return plaintext, nil
}

func (keyring Keyring) Rewrap(envelope Envelope, aad []byte) (Envelope, error) {
	return keyring.RewrapContext(context.Background(), envelope, aad)
}

func (keyring Keyring) RewrapContext(ctx context.Context, envelope Envelope, aad []byte) (Envelope, error) {
	if keyring.provider != nil {
		dek, err := keyring.provider.Decrypt(ctx, keywrap.Ciphertext{Provider: envelope.Provider, KeyID: envelope.KeyID,
			KeyVersion: int32(envelope.KeyVersion), Value: envelope.WrappedDEK})
		if err != nil {
			return Envelope{}, err
		}
		defer clear(dek)
		wrapped, err := keyring.provider.Encrypt(ctx, dek)
		if err != nil {
			return Envelope{}, err
		}
		envelope.Provider, envelope.KeyID, envelope.KeyVersion = wrapped.Provider, wrapped.KeyID, int(wrapped.KeyVersion)
		envelope.WrappedDEK, envelope.WrapNonce = wrapped.Value, nil
		return envelope, nil
	}
	if envelope.KeyVersion == keyring.currentVersion {
		return envelope, nil
	}
	old, ok := keyring.keys[envelope.KeyVersion]
	if !ok {
		return Envelope{}, ErrUnknownKeyVersion
	}
	wrapAAD := append([]byte("argus.secret_dek/v1\x00"), aad...)
	dek, err := open(old, envelope.WrapNonce, envelope.WrappedDEK, wrapAAD)
	if err != nil {
		return Envelope{}, fmt.Errorf("unwrap secret DEK: %w", err)
	}
	defer clear(dek)
	wrapNonce, wrappedDEK, err := seal(keyring.keys[keyring.currentVersion], dek, wrapAAD)
	if err != nil {
		return Envelope{}, err
	}
	envelope.KeyVersion = keyring.currentVersion
	envelope.WrapNonce = wrapNonce
	envelope.WrappedDEK = wrappedDEK
	return envelope, nil
}

func seal(key, plaintext, aad []byte) ([]byte, []byte, error) {
	aead, err := newGCM(key)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("generate encryption nonce: %w", err)
	}
	return nonce, aead.Seal(nil, nonce, plaintext, aad), nil
}

func open(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	aead, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, errors.New("invalid encryption nonce")
	}
	return aead.Open(nil, nonce, ciphertext, aad)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != keySize {
		return nil, errors.New("encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := range a {
		result |= a[i] ^ b[i]
	}
	return result == 0
}
