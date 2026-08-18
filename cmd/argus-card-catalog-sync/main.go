package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kakj-go/Argus/internal/agent"
	cardservice "github.com/kakj-go/Argus/internal/card"
	"github.com/kakj-go/Argus/internal/mcp"
	"github.com/kakj-go/Argus/internal/storage/postgres"
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
	registry := mcp.NewRegistry()
	if err := (agent.ResourceTools{Store: store}).Register(registry); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	service := cardservice.Service{Store: store, Tools: registry}
	if err := service.SyncSystemCatalog(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "system Card catalog synchronized")
}
