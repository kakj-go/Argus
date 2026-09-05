package connector

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kakj-go/Argus/internal/resource"
)

func TestValidateDirectInstallInputRejectsMissingMode(t *testing.T) {
	_, _, err := validateDirectInstallInput(context.Background(), nil, uuid.Must(uuid.NewV7()), BastionInput{})
	if !errors.Is(err, resource.ErrInvalidConnectionMode) {
		t.Fatalf("missing install mode error = %v, want %v", err, resource.ErrInvalidConnectionMode)
	}
}

func TestOnlyModeAExposesFrozenReleaseToEnrollmentCommand(t *testing.T) {
	releaseID := uuid.NullUUID{UUID: uuid.Must(uuid.NewV7()), Valid: true}
	if got := manualConnectorReleaseID(bastionPlan{Input: BastionInput{InstallMode: "command"}, ReleaseVersionID: releaseID}); got != releaseID {
		t.Fatalf("Mode A release = %v", got)
	}
	if got := manualConnectorReleaseID(bastionPlan{Input: BastionInput{InstallMode: "direct_install"}, ReleaseVersionID: releaseID}); got.Valid {
		t.Fatalf("direct install leaked browser-command release input: %v", got)
	}
}
