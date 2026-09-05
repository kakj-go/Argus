package connector

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"

	"github.com/kakj-go/Argus/internal/collectormanager"
	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/trustbundle"
)

const (
	collectorRepairNamespace = "argus-telemetry"
	collectorIdentitySecret  = "argus-otelcol-identity"
	collectorConfigMap       = "argus-otelcol-config"
)

// runRepairCollector is intentionally available only from an in-cluster
// Kubernetes Connector. It creates fresh keys locally, performs strict TLS
// enrollment, and writes only the two fixed Argus Collector resources.
func runRepairCollector(ctx context.Context, logger *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("argus-connector repair-collector", flag.ContinueOnError)
	server := flags.String("server", "", "Collector identity enrollment URL")
	collectorID := flags.String("collector-id", "", "Collector identity UUID")
	token := flags.String("token", "", "one-time Collector PKI repair token")
	caFile := flags.String("ca-file", "", "current Argus Trust Bundle file")
	epoch := flags.Uint64("trust-bundle-epoch", 0, "current Trust Bundle epoch")
	namespace := flags.String("namespace", collectorRepairNamespace, "Collector namespace")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *server == "" || *collectorID == "" || *token == "" || *caFile == "" || *epoch < 1 ||
		len(validation.IsDNS1123Label(*namespace)) != 0 {
		return errors.New("--server, --collector-id, --token, --ca-file, a positive --trust-bundle-epoch, and a valid --namespace are required")
	}
	caPEM, err := os.ReadFile(*caFile)
	if err != nil {
		return fmt.Errorf("read repair Trust Bundle: %w", err)
	}
	material, err := trustbundle.Parse(caPEM, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("validate repair Trust Bundle: %w", err)
	}
	command := &connectorv1.CollectorManagementCommand{Operation: "install", CollectorId: *collectorID,
		EnrollmentToken: []byte(*token), EnrollmentEndpoint: *server, TrustBundlePem: material.PEM,
		TrustBundleEpoch: *epoch, TrustBundleSha256: material.SHA256,
		TrustBundleCaFingerprints: append([]string(nil), material.Fingerprints...)}
	identity, err := collectormanager.EnrollIdentity(ctx, command)
	if err != nil {
		return fmt.Errorf("enroll repaired Collector identity: %w", err)
	}
	configuration, err := rest.InClusterConfig()
	if err != nil {
		return errors.New("repair-collector must run in a Kubernetes Connector Pod")
	}
	client, err := kubernetes.NewForConfig(configuration)
	if err != nil {
		return err
	}
	state, _ := json.Marshal(map[string]any{"epoch": identity.TrustBundleEpoch, "bundle_sha256": identity.TrustBundleSHA256,
		"ca_fingerprints": identity.TrustBundleCAFingerprints})
	if err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		secret, getErr := client.CoreV1().Secrets(*namespace).Get(ctx, collectorIdentitySecret, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}
		for key, value := range map[string][]byte{
			"client.pem": identity.ClientCertificatePEM, "client-key.pem": identity.ClientPrivateKeyPEM,
			"server.pem": identity.ServerCertificatePEM, "server-key.pem": identity.ServerPrivateKeyPEM,
			"ca.pem": identity.CABundlePEM, "trust-bundle.json": state,
			"trust-bundle-epoch":  []byte(strconv.FormatUint(identity.TrustBundleEpoch, 10)),
			"trust-bundle-sha256": []byte(identity.TrustBundleSHA256),
		} {
			secret.Data[key] = value
		}
		delete(secret.Data, "enrollment-token")
		_, updateErr := client.CoreV1().Secrets(*namespace).Update(ctx, secret, metav1.UpdateOptions{})
		return updateErr
	}); err != nil {
		return fmt.Errorf("persist repaired Collector identity: %w", err)
	}
	if err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		config, getErr := client.CoreV1().ConfigMaps(*namespace).Get(ctx, collectorConfigMap, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		if config.Data == nil {
			config.Data = map[string]string{}
		}
		config.Data["server-ca.pem"] = string(identity.CABundlePEM)
		config.Data["trust-bundle-epoch"] = strconv.FormatUint(identity.TrustBundleEpoch, 10)
		config.Data["trust-bundle-sha256"] = strings.ToLower(identity.TrustBundleSHA256)
		_, updateErr := client.CoreV1().ConfigMaps(*namespace).Update(ctx, config, metav1.UpdateOptions{})
		return updateErr
	}); err != nil {
		return fmt.Errorf("persist repaired Collector Trust Bundle: %w", err)
	}
	logger.Info("Kubernetes Collector identity repaired", "collector_id", *collectorID, "trust_bundle_epoch", identity.TrustBundleEpoch)
	return nil
}
