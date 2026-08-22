package keywrap

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalRoundTripAndTamper(t *testing.T) {
	provider := Local{Key: []byte("0123456789abcdef0123456789abcdef"), KeyID: "m8-test"}
	ciphertext, err := provider.Encrypt(context.Background(), []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if ciphertext.Provider != ProviderLocalTest {
		t.Fatalf("local provider = %q", ciphertext.Provider)
	}
	plaintext, err := provider.Decrypt(context.Background(), ciphertext)
	if err != nil || string(plaintext) != "secret" {
		t.Fatalf("round trip failed: %q %v", plaintext, err)
	}
	ciphertext.Value[len(ciphertext.Value)-1] ^= 1
	if _, err := provider.Decrypt(context.Background(), ciphertext); err == nil {
		t.Fatal("tampered ciphertext was accepted")
	}
}

func TestOpenBaoRoundTripAndUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Vault-Token") != "token" {
			writer.WriteHeader(http.StatusForbidden)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/v1/transit/encrypt/argus" {
			_, _ = writer.Write([]byte(`{"data":{"ciphertext":"vault:v3:value"}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"data":{"plaintext":"c2VjcmV0"}}`))
	}))
	defer server.Close()
	provider := OpenBao{Address: server.URL, Token: "token", KeyID: "argus"}
	ciphertext, err := provider.Encrypt(context.Background(), []byte("secret"))
	if err != nil || ciphertext.Provider != ProviderOpenBaoTransit || ciphertext.KeyVersion != 3 {
		t.Fatalf("encrypt: %#v %v", ciphertext, err)
	}
	plaintext, err := provider.Decrypt(context.Background(), ciphertext)
	if err != nil || string(plaintext) != "secret" {
		t.Fatalf("decrypt: %q %v", plaintext, err)
	}
	provider.Address = "::not-a-url"
	if _, err := provider.Encrypt(context.Background(), []byte("secret")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected unavailable, got %v", err)
	}
}
