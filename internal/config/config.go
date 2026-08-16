// Package config loads process configuration from the environment.
package config

import (
	"encoding/base64"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultHTTPAddress = ":8080"
const defaultHealthAddress = ":8081"

type Server struct {
	Address                  string
	DatabaseURL              string
	RedisURL                 string
	SetupTokenPath           string
	SetupTokenExpiresPath    string
	AllowedOrigins           []string
	SecureCookies            bool
	IdempotencyEncryptionKey []byte
	CursorSigningKey         []byte
	SessionIdleTTL           time.Duration
	SessionAbsoluteTTL       time.Duration
}

func LoadServer() Server {
	address := os.Getenv("ARGUS_HTTP_ADDRESS")
	if address == "" {
		address = defaultHTTPAddress
	}

	secureCookies, _ := strconv.ParseBool(os.Getenv("ARGUS_SECURE_COOKIES"))
	key, _ := base64.RawURLEncoding.DecodeString(os.Getenv("ARGUS_IDEMPOTENCY_ENCRYPTION_KEY"))
	cursorKey, _ := base64.RawURLEncoding.DecodeString(os.Getenv("ARGUS_CURSOR_SIGNING_KEY"))
	return Server{
		Address:                  address,
		DatabaseURL:              os.Getenv("ARGUS_DATABASE_URL"),
		RedisURL:                 os.Getenv("ARGUS_REDIS_URL"),
		SetupTokenPath:           valueOrDefault("ARGUS_SETUP_TOKEN_PATH", "/var/run/secrets/argus/setup/token"),
		SetupTokenExpiresPath:    valueOrDefault("ARGUS_SETUP_TOKEN_EXPIRES_PATH", "/var/run/secrets/argus/setup/expires-at"),
		AllowedOrigins:           splitList(os.Getenv("ARGUS_ALLOWED_ORIGINS")),
		SecureCookies:            secureCookies,
		IdempotencyEncryptionKey: key,
		CursorSigningKey:         cursorKey,
		SessionIdleTTL:           30 * time.Minute,
		SessionAbsoluteTTL:       12 * time.Hour,
	}
}

func (cfg Server) Validate() error {
	if cfg.DatabaseURL == "" {
		return errors.New("ARGUS_DATABASE_URL is required")
	}
	if cfg.RedisURL == "" {
		return errors.New("ARGUS_REDIS_URL is required")
	}
	if len(cfg.AllowedOrigins) == 0 {
		return errors.New("ARGUS_ALLOWED_ORIGINS is required")
	}
	if len(cfg.IdempotencyEncryptionKey) != 32 {
		return errors.New("ARGUS_IDEMPOTENCY_ENCRYPTION_KEY must be 32 bytes in base64url form")
	}
	if len(cfg.CursorSigningKey) != 32 {
		return errors.New("ARGUS_CURSOR_SIGNING_KEY must be 32 bytes in base64url form")
	}
	return nil
}

func valueOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func splitList(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func LoadHealthAddress() string {
	address := os.Getenv("ARGUS_HEALTH_ADDRESS")
	if address == "" {
		address = defaultHealthAddress
	}
	return address
}
