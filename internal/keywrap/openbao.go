package keywrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type OpenBao struct {
	Address string
	Token   string
	KeyID   string
	Client  *http.Client
}

func (provider OpenBao) Encrypt(ctx context.Context, plaintext []byte) (Ciphertext, error) {
	var response struct {
		Data struct {
			Ciphertext string `json:"ciphertext"`
		} `json:"data"`
	}
	if err := provider.call(ctx, "encrypt", map[string]string{"plaintext": Encode(plaintext)}, &response); err != nil {
		return Ciphertext{}, err
	}
	parts := strings.Split(response.Data.Ciphertext, ":")
	if len(parts) != 3 || parts[0] != "vault" || !strings.HasPrefix(parts[1], "v") {
		return Ciphertext{}, errors.New("OpenBao returned an invalid ciphertext reference")
	}
	version, err := strconv.ParseInt(strings.TrimPrefix(parts[1], "v"), 10, 32)
	if err != nil {
		return Ciphertext{}, errors.New("OpenBao returned an invalid key version")
	}
	return Ciphertext{Provider: "openbao_transit", KeyID: provider.KeyID, KeyVersion: int32(version), Value: []byte(response.Data.Ciphertext)}, nil
}

func (provider OpenBao) Decrypt(ctx context.Context, ciphertext Ciphertext) ([]byte, error) {
	if ciphertext.Provider != "openbao_transit" || ciphertext.KeyID != provider.KeyID {
		return nil, errors.New("ciphertext key reference mismatch")
	}
	var response struct {
		Data struct {
			Plaintext string `json:"plaintext"`
		} `json:"data"`
	}
	if err := provider.call(ctx, "decrypt", map[string]string{"ciphertext": string(ciphertext.Value)}, &response); err != nil {
		return nil, err
	}
	return Decode(response.Data.Plaintext)
}

func (provider OpenBao) call(ctx context.Context, operation string, body any, target any) error {
	base, err := url.Parse(provider.Address)
	if err != nil || base.Scheme != "http" && base.Scheme != "https" || base.Host == "" || provider.Token == "" || provider.KeyID == "" {
		return ErrUnavailable
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/v1/transit/" + operation + "/" + url.PathEscape(provider.KeyID)
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Vault-Token", provider.Token)
	client := provider.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		return fmt.Errorf("%w: OpenBao returned HTTP %d", ErrUnavailable, response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(target); err != nil {
		return fmt.Errorf("decode OpenBao response: %w", err)
	}
	return nil
}
