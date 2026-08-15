package worker

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestRunRejectsUnsupportedPool(t *testing.T) {
	t.Parallel()

	err := Run(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), "unknown")
	if err == nil {
		t.Fatal("Run returned nil for an unsupported pool")
	}
}
