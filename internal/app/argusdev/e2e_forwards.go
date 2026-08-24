package argusdev

import (
	"context"
	"fmt"
	"time"
)

const m6BastionSSHForwardPort = 4222

func (a *App) configureE2ERuntime(ctx context.Context, env *E2EEnvironment) error {
	if !suiteHas(env.Options.Suite, "m6") {
		return nil
	}
	for _, workload := range []string{"argus-server", "argus-connector-gateway"} {
		patch := map[string]any{
			"spec": map[string]any{
				"template": map[string]any{
					"spec": map[string]any{
						"containers": []any{
							map[string]any{"name": workload, "env": []any{map[string]any{"name": "ARGUS_REMOTE_ORIGIN", "value": "http://127.0.0.1:4195"}}},
						},
					},
				},
			},
		}
		if err := env.Kube.PatchDeployment(ctx, env.SystemNS, workload, patch); err != nil {
			return err
		}
		if err := env.Kube.WaitDeployment(ctx, env.SystemNS, workload, 5*time.Minute); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) startE2EForwards(ctx context.Context, env *E2EEnvironment) error {
	type forward struct {
		name, namespace, service string
		ports                    []string
		readyURL                 string
	}
	forwards := []forward{
		{"api", env.SystemNS, "argus-server", []string{"4180:8080"}, "http://127.0.0.1:4180/readyz"},
		{"web", env.SystemNS, "argus-web", []string{"4173:8080", "4174:8081", "4176:8083"}, "http://127.0.0.1:4173/healthz"},
		{"remote", env.SystemNS, "argus-connector-gateway", []string{"4195:9445"}, ""},
		{"connector", env.SystemNS, "argus-connector-gateway", []string{"4193:9443"}, ""},
	}
	if suiteHas(env.Options.Suite, "m6") {
		forwards = append(forwards, forward{
			"bastion-ssh", env.SystemNS, "argus-e2e-ssh-target",
			[]string{fmt.Sprintf("%d:2222", m6BastionSSHForwardPort)}, "",
		})
	}
	for _, item := range forwards {
		log, err := openArtifact(env.Options.Artifacts + "/port-forward-" + item.name + ".log")
		if err != nil {
			return err
		}
		forward, err := env.Kube.PortForwardService(ctx, item.namespace, item.service, item.ports, log)
		if err != nil {
			_ = log.Close()
			return err
		}
		env.Forwards = append(env.Forwards, forward)
		if item.readyURL != "" {
			if err := waitHTTP(ctx, item.readyURL, "", 2*time.Minute); err != nil {
				return fmt.Errorf("%s port-forward: %w", item.name, err)
			}
		}
	}
	return nil
}
