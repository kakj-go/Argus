// Package connector contains the enterprise-side Connector runtime.
package connector

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

const softwareVersion = "0.1.0-m3"

func Run(ctx context.Context, logger *slog.Logger) error {
	command := "run"
	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "run" || args[0] == "enroll") {
		command, args = args[0], args[1:]
	}
	switch command {
	case "enroll":
		return runEnroll(ctx, logger, args)
	case "run":
		return runConnector(ctx, logger, args)
	default:
		return fmt.Errorf("unknown connector command %q", command)
	}
}

func runEnroll(ctx context.Context, logger *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("argus-connector enroll", flag.ContinueOnError)
	server := flags.String("server", os.Getenv("ARGUS_CONNECTOR_ENROLLMENT_URL"), "Argus enrollment API URL")
	connectorID := flags.String("connector-id", "", "preallocated Connector ID")
	token := flags.String("token", os.Getenv("ARGUS_CONNECTOR_ENROLLMENT_TOKEN"), "one-time enrollment token")
	role := flags.String("role", "bastion", "Connector role: bastion or kubernetes")
	name := flags.String("name", hostname(), "Connector display name")
	dataDirectory := flags.String("data-dir", defaultDataDirectory(), "Connector identity directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *server == "" || *connectorID == "" || *token == "" || (*role != "bastion" && *role != "kubernetes") {
		return errors.New("--server, --connector-id, --token, and a valid --role are required")
	}
	result, err := enroll(ctx, enrollOptions{Server: *server, ConnectorID: *connectorID, Token: *token, Role: *role,
		Name: *name, DataDirectory: *dataDirectory})
	if err != nil {
		return err
	}
	logger.Info("Connector enrolled", "connector_id", result.ConnectorID, "role", result.Role, "gateway", result.GatewayEndpoint)
	return nil
}

func runConnector(ctx context.Context, logger *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("argus-connector run", flag.ContinueOnError)
	dataDirectory := flags.String("data-dir", defaultDataDirectory(), "Connector identity directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return (connectorClient{store: localStore{directory: *dataDirectory}, logger: logger}).run(ctx)
}

func defaultDataDirectory() string {
	if value := os.Getenv("ARGUS_CONNECTOR_DATA_DIR"); value != "" {
		return value
	}
	return "/var/lib/argus-connector"
}

func hostname() string {
	value, err := os.Hostname()
	if err != nil || value == "" {
		return "argus-connector"
	}
	return filepath.Base(value)
}
