package argusctl

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func (a *App) runTelemetryDLQReplay(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("telemetry dlq replay", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	configPath := flags.String("config", "deploy/profiles/evaluation.yaml", "ArgusInstallConfig file")
	recordID := flags.String("record-id", "", "telemetry DLQ record UUID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	id, err := uuid.Parse(*recordID)
	if err != nil {
		return fmt.Errorf("record-id must be a UUID")
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	jobName := kubernetesName("argus-telemetry-dlq-replay-" + strings.ReplaceAll(id.String(), "-", "")[:12])
	manifest := telemetryDLQReplayJob(jobName, id, cfg)
	defer func() {
		_, _ = a.runner.quiet(context.Background(), "kubectl", "--context", cfg.Spec.KubeContext, "--namespace", cfg.Spec.Namespaces.Observability,
			"delete", "job", jobName, "--ignore-not-found=true", "--wait=true")
	}()
	if _, err = a.runner.run(ctx, strings.NewReader(manifest), "kubectl", "--context", cfg.Spec.KubeContext, "apply", "--filename", "-"); err != nil {
		return err
	}
	if _, err = a.runner.run(ctx, nil, "kubectl", "--context", cfg.Spec.KubeContext, "--namespace", cfg.Spec.Namespaces.Observability,
		"wait", "--for=condition=complete", "job/"+jobName, "--timeout=2m"); err != nil {
		_, _ = a.runner.run(context.Background(), nil, "kubectl", "--context", cfg.Spec.KubeContext, "--namespace", cfg.Spec.Namespaces.Observability,
			"logs", "job/"+jobName, "--all-containers=true")
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "Telemetry DLQ record %s replayed\n", id)
	return nil
}

func telemetryDLQReplayJob(name string, recordID uuid.UUID, cfg *InstallConfig) string {
	return fmt.Sprintf(`apiVersion: batch/v1
kind: Job
metadata:
  name: %s
  namespace: %s
  labels:
    app.kubernetes.io/name: argus-telemetry-writer
    app.kubernetes.io/part-of: argus
    argus.io/release-id: %s
spec:
  backoffLimit: 1
  ttlSecondsAfterFinished: 300
  template:
    metadata:
      labels:
        app.kubernetes.io/name: argus-telemetry-writer
        app.kubernetes.io/part-of: argus
    spec:
      restartPolicy: Never
      serviceAccountName: argus-telemetry-writer
      securityContext: {runAsNonRoot: true, seccompProfile: {type: RuntimeDefault}}
      containers:
        - name: replay
          image: %s
          imagePullPolicy: %s
          command: [/usr/local/bin/argus-telemetry-dlq-replay]
          args: [--record-id=%s]
          env:
            - name: ARGUS_DATABASE_URL
              valueFrom: {secretKeyRef: {name: argus-telemetry-runtime, key: database-url}}
            - {name: ARGUS_KAFKA_BROKERS, value: "argus-kafka-kafka-bootstrap:9093"}
            - {name: ARGUS_KAFKA_USERNAME, value: argus-telemetry}
            - name: ARGUS_KAFKA_PASSWORD
              valueFrom: {secretKeyRef: {name: argus-telemetry, key: password}}
          resources: {requests: {cpu: 25m, memory: 64Mi}, limits: {cpu: 500m, memory: 256Mi}}
`, name, cfg.Spec.Namespaces.Observability, cfg.Spec.ReleaseID, cfg.Image("argus-backend"), cfg.Spec.Images.PullPolicy, recordID)
}
