package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/telemetry"
)

func main() {
	ctx := context.Background()
	databaseURL := os.Getenv("ARGUS_DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "ARGUS_DATABASE_URL is required")
		os.Exit(1)
	}
	store, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer store.Close()

	sync := telemetry.CatalogSync{
		Service:                  telemetry.Service{Store: store},
		Version:                  os.Getenv("ARGUS_OTELCOL_VERSION"),
		LinuxArtifactURI:         os.Getenv("ARGUS_OTELCOL_LINUX_ARM64_URI"),
		LinuxArtifactSHA256:      os.Getenv("ARGUS_OTELCOL_LINUX_ARM64_SHA256"),
		LinuxArtifactSignature:   os.Getenv("ARGUS_OTELCOL_LINUX_ARM64_SIGNATURE"),
		LinuxArtifactByteSize:    envUint64("ARGUS_OTELCOL_LINUX_ARM64_BYTE_SIZE"),
		WindowsArtifactURI:       os.Getenv("ARGUS_OTELCOL_WINDOWS_AMD64_URI"),
		WindowsArtifactSHA256:    os.Getenv("ARGUS_OTELCOL_WINDOWS_AMD64_SHA256"),
		WindowsArtifactSignature: os.Getenv("ARGUS_OTELCOL_WINDOWS_AMD64_SIGNATURE"),
		WindowsArtifactByteSize:  envUint64("ARGUS_OTELCOL_WINDOWS_AMD64_BYTE_SIZE"),
		SigningKeyID:             os.Getenv("ARGUS_OTELCOL_SIGNING_KEY_ID"),
		SigningPublicKey:         os.Getenv("ARGUS_OTELCOL_SIGNING_PUBLIC_KEY"),
	}
	if err := sync.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "telemetry Collector catalog synchronized")
}

func envUint64(name string) uint64 {
	value, _ := strconv.ParseUint(os.Getenv(name), 10, 64)
	return value
}
