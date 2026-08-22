package argusctl

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
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
	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	secret.Data["setup-token"] = []byte(token)
	secret.Data["setup-token-expires-at"] = []byte(expiresAt.Format(time.RFC3339))
	if _, err := clients.typed.CoreV1().Secrets(cfg.Spec.Namespaces.System).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("rotate setup token: %w", err)
	}
	for _, deployment := range []string{"argus-server", "argus-web"} {
		if err := restartDeployment(ctx, clients, cfg.Spec.Namespaces.System, deployment, "setup-token-rotation"); err != nil {
			return fmt.Errorf("activate rotated setup token in %s: %w", deployment, err)
		}
	}
	printSetupToken(a.stdout, token, expiresAt)
	return nil
}

func restartDeployment(ctx context.Context, clients *kubeClients, namespace, name, reason string) error {
	deployments := clients.typed.AppsV1().Deployments(namespace)
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, err := deployments.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = map[string]string{}
		}
		deployment.Spec.Template.Annotations["argus.io/restarted-at"] = time.Now().UTC().Format(time.RFC3339Nano)
		deployment.Spec.Template.Annotations["argus.io/restart-reason"] = reason
		_, err = deployments.Update(ctx, deployment, metav1.UpdateOptions{})
		return err
	}); err != nil {
		return fmt.Errorf("restart deployment %s/%s: %w", namespace, name, err)
	}
	return waitForDeployment(ctx, clients, namespace, name, 5*time.Minute)
}

func printSetupToken(writer io.Writer, token string, expiresAt time.Time) {
	_, _ = fmt.Fprintln(writer, "Setup Token (copy and store it now; shown only once):")
	_, _ = fmt.Fprintln(writer, token)
	_, _ = fmt.Fprintf(writer, "Expires at: %s\n", expiresAt.UTC().Format(time.RFC3339))
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
