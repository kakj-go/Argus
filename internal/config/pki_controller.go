package config

import (
	"errors"
	"os"
	"strings"
	"time"
)

type PKIController struct {
	DatabaseURL       string
	ReleaseID         string
	Mode              string
	TrustSourceName   string
	TrustBundleName   string
	ManagedRootPrefix string
	Namespaces        []string
	Interval          time.Duration
}

func LoadPKIController() PKIController {
	releaseID := os.Getenv("ARGUS_RELEASE_ID")
	return PKIController{
		DatabaseURL:       os.Getenv("ARGUS_DATABASE_URL"),
		ReleaseID:         releaseID,
		Mode:              os.Getenv("ARGUS_PKI_MODE"),
		TrustSourceName:   os.Getenv("ARGUS_PKI_TRUST_SOURCE_NAME"),
		TrustBundleName:   os.Getenv("ARGUS_PKI_TRUST_BUNDLE_NAME"),
		ManagedRootPrefix: releaseID + "-root-ca-previous-",
		Namespaces:        splitList(os.Getenv("ARGUS_PKI_TARGET_NAMESPACES")),
		Interval:          durationOrDefault("ARGUS_PKI_RECONCILE_INTERVAL", 30*time.Second),
	}
}

func (cfg PKIController) Validate() error {
	if cfg.DatabaseURL == "" || cfg.ReleaseID == "" || cfg.TrustSourceName == "" || cfg.TrustBundleName == "" || len(cfg.Namespaces) == 0 {
		return errors.New("PKI controller database, release, Trust Bundle, and namespace configuration is required")
	}
	if cfg.Mode != "managed" && cfg.Mode != "existing-cluster-issuer" {
		return errors.New("ARGUS_PKI_MODE must be managed or existing-cluster-issuer")
	}
	if cfg.Interval < time.Second {
		return errors.New("ARGUS_PKI_RECONCILE_INTERVAL must be at least one second")
	}
	for _, namespace := range cfg.Namespaces {
		if strings.TrimSpace(namespace) == "" {
			return errors.New("ARGUS_PKI_TARGET_NAMESPACES contains an empty namespace")
		}
	}
	return nil
}
