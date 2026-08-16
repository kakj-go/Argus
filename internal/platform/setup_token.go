package platform

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

var (
	ErrSetupTokenInvalid     = errors.New("setup token is invalid")
	ErrSetupTokenExpired     = errors.New("setup token has expired")
	ErrSetupTokenUnavailable = errors.New("setup token is unavailable")
)

type SetupTokenProvider struct {
	TokenPath   string
	ExpiresPath string
	Now         func() time.Time
}

func (provider SetupTokenProvider) Verify(candidate string) error {
	tokenBytes, err := os.ReadFile(provider.TokenPath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSetupTokenUnavailable, err)
	}
	expiresBytes, err := os.ReadFile(provider.ExpiresPath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSetupTokenUnavailable, err)
	}
	expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(string(expiresBytes)))
	if err != nil {
		return fmt.Errorf("%w: invalid expiry", ErrSetupTokenUnavailable)
	}
	now := time.Now
	if provider.Now != nil {
		now = provider.Now
	}
	if !now().Before(expiresAt) {
		return ErrSetupTokenExpired
	}
	expected := strings.TrimSpace(string(tokenBytes))
	if len(candidate) != len(expected) || subtle.ConstantTimeCompare([]byte(candidate), []byte(expected)) != 1 {
		return ErrSetupTokenInvalid
	}
	return nil
}
