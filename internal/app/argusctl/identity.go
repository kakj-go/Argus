package argusctl

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (a *App) runSetupTokenRotate(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("setup-token rotate", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	configPath := flags.String("config", "deploy/profiles/evaluation.yaml", "ArgusInstallConfig file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	clients, err := clientsFor(cfg.Spec.KubeContext)
	if err != nil {
		return err
	}
	raw, err := clients.typed.CoreV1().RESTClient().Get().
		Namespace(cfg.Spec.Namespaces.System).
		Resource("services").Name("argus-server:8080").SubResource("proxy").
		Suffix("api", "v1", "setup", "status").Do(ctx).Raw()
	if err != nil {
		return fmt.Errorf("read setup status: %w", err)
	}
	var status struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return fmt.Errorf("decode setup status: %w", err)
	}
	if status.State != "uninitialized" {
		return errors.New("setup token rotation is disabled after initialization")
	}
	name := cfg.Spec.ReleaseID + "-generated-secrets"
	secret, err := clients.typed.CoreV1().Secrets(cfg.Spec.Namespaces.System).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read setup token secret: %w", err)
	}
	token, err := randomSecret(32)
	if err != nil {
		return err
	}
	secret.Data["setup-token"] = []byte(token)
	secret.Data["setup-token-expires-at"] = []byte(time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339))
	if _, err := clients.typed.CoreV1().Secrets(cfg.Spec.Namespaces.System).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("rotate setup token: %w", err)
	}
	_, _ = fmt.Fprintln(a.stdout, token)
	return nil
}

func (a *App) runAdminResetPassword(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("admin reset-password", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	configPath := flags.String("config", "deploy/profiles/evaluation.yaml", "ArgusInstallConfig file")
	userID := flags.String("user-id", "", "enterprise administrator user ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if _, err := uuid.Parse(*userID); err != nil {
		return errors.New("--user-id must be a UUID")
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	_, err = a.runner.run(ctx, nil, "kubectl", "--context", cfg.Spec.KubeContext, "-n", cfg.Spec.Namespaces.System,
		"exec", "deployment/argus-server", "--", "/usr/local/bin/argus-server", "admin-reset-password", "--user-id", strings.TrimSpace(*userID))
	return err
}
