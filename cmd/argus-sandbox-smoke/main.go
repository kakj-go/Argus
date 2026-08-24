package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kakj-go/Argus/internal/app/sandboxsmoke"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := sandboxsmoke.Run(ctx, sandboxsmoke.Options{
		BaseURL:      os.Getenv("OPENSANDBOX_BASE_URL"),
		APIKey:       os.Getenv("OPENSANDBOX_API_KEY"),
		SandboxImage: os.Getenv("OPENSANDBOX_SMOKE_IMAGE"),
	}); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "argus-sandbox-smoke: %v\n", err)
		os.Exit(1)
	}
	_, _ = fmt.Fprintln(os.Stdout, "OpenSandbox lifecycle verification passed")
}
