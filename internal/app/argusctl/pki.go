package argusctl

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/trustbundle"
)

var (
	pkiCertificateGVR = schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificates"}
	pkiIssuerGVR      = schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "clusterissuers"}
)

const (
	pkiRoleLabel         = "argus.io/pki-role"
	pkiEpochAnnotation   = "argus.io/pki-epoch"
	pkiSourceCertificate = "argus.io/pki-source-certificate"
	pkiSourceSecret      = "argus.io/pki-source-secret"
	pkiTargetIssuer      = "argus.io/pki-target-issuer"
	pkiFormerIssuer      = "argus.io/pki-former-issuer"
	pkiStagedServerRole  = "staged-server"
	pkiFormerIssuerRole  = "former-issuer"
)

type pkiSession struct {
	clients *kubeClients
	store   *postgres.Store
	forward *kubePortForward
}

func (session *pkiSession) Close() {
	if session == nil {
		return
	}
	if session.store != nil {
		session.store.Close()
	}
	if session.forward != nil {
		_ = session.forward.Stop()
	}
}

type pkiBundleStatus struct {
	Epoch                 int64     `json:"epoch"`
	State                 string    `json:"state"`
	Direction             string    `json:"direction"`
	SHA256                string    `json:"sha256"`
	CurrentCAFingerprints []string  `json:"currentCaFingerprints"`
	NextCAFingerprints    []string  `json:"nextCaFingerprints"`
	StartedAt             time.Time `json:"startedAt"`
	RetireAt              time.Time `json:"retireAt,omitempty"`
	LastError             string    `json:"lastError,omitempty"`
}

type pkiNodeStatus struct {
	Kind           string    `json:"kind"`
	ID             string    `json:"id"`
	Status         string    `json:"status"`
	Epoch          int64     `json:"epoch"`
	BlocksCutover  bool      `json:"blocksCutover"`
	AcknowledgedAt time.Time `json:"acknowledgedAt,omitempty"`
	UpdatedAt      time.Time `json:"updatedAt"`
	Error          string    `json:"error,omitempty"`
}

type pkiStatusReport struct {
	Mode              PKIMode           `json:"mode"`
	IssuerName        string            `json:"issuerName"`
	TrustSource       string            `json:"trustSource"`
	TrustSourceSHA256 string            `json:"trustSourceSha256"`
	Bundles           []pkiBundleStatus `json:"bundles"`
	Nodes             []pkiNodeStatus   `json:"nodes"`
}

func (a *App) runPKI(ctx context.Context, operation string, args []string) error {
	flags := flag.NewFlagSet("pki "+operation, flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	configPath := flags.String("config", "deploy/profiles/evaluation.yaml", "ArgusInstallConfig file")
	output := flags.String("output", "text", "text or json")
	duration := flags.String("duration", "", "positive extension duration")
	nodeKind := flags.String("node-kind", "", "connector, collector, or kubernetes_connector")
	nodeID := flags.String("node-id", "", "node identifier")
	scope := flags.String("scope", "linux-system", "repair target: linux-system, linux-user, or kubernetes")
	targetNamespace := flags.String("target-namespace", "argus-system", "target namespace for a Kubernetes Connector repair")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *output != "text" && *output != "json" {
		return fmt.Errorf("unsupported output %q", *output)
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	switch operation {
	case "status":
		return a.pkiStatus(ctx, cfg, *output)
	case "rotate":
		return a.pkiRotate(ctx, cfg)
	case "extend":
		extension, parseErr := time.ParseDuration(*duration)
		if parseErr != nil || extension <= 0 {
			return errors.New("pki extend requires --duration with a positive Go duration")
		}
		return a.pkiExtend(ctx, cfg, extension)
	case "abort":
		return a.pkiAbort(ctx, cfg)
	case "repair-command":
		return a.pkiRepairCommand(ctx, cfg, *nodeKind, *nodeID, *scope, *targetNamespace, *output)
	default:
		return fmt.Errorf("unsupported pki operation %q", operation)
	}
}

func (a *App) openPKISession(ctx context.Context, cfg *InstallConfig) (*pkiSession, error) {
	clients, err := clientsFor(cfg.Spec.KubeContext)
	if err != nil {
		return nil, err
	}
	credentials, err := clients.typed.CoreV1().Secrets(cfg.Spec.Namespaces.System).Get(ctx, cfg.Spec.ReleaseID+"-generated-credentials", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read installation database credential: %w", err)
	}
	password := string(credentials.Data["postgresql-password"])
	if password == "" {
		return nil, errors.New("installation database credential is missing postgresql-password")
	}
	forward, err := portForwardService(ctx, clients, cfg.Spec.Namespaces.System, "argus-postgresql", 5432)
	if err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL for PKI operation: %w", err)
	}
	databaseURL := &url.URL{Scheme: "postgres", User: url.UserPassword("argus", password), Host: fmt.Sprintf("127.0.0.1:%d", forward.localPort), Path: "/argus", RawQuery: "sslmode=disable"}
	store, err := postgres.Open(ctx, databaseURL.String())
	if err != nil {
		_ = forward.Stop()
		return nil, err
	}
	return &pkiSession{clients: clients, store: store, forward: forward}, nil
}

func (a *App) pkiStatus(ctx context.Context, cfg *InstallConfig, output string) error {
	session, err := a.openPKISession(ctx, cfg)
	if err != nil {
		return err
	}
	defer session.Close()
	records, err := session.store.Queries.ListTrustBundles(ctx, 10)
	if err != nil {
		return err
	}
	report := pkiStatusReport{Mode: cfg.Spec.PKI.Mode, IssuerName: cfg.globalIssuerName(), TrustSource: "cert-manager/" + cfg.trustSourceName()}
	for _, record := range records {
		item := pkiBundleStatus{Epoch: record.Epoch, State: record.State, Direction: record.Direction, SHA256: record.BundleSha256,
			CurrentCAFingerprints: record.CurrentCaFingerprints, NextCAFingerprints: record.NextCaFingerprints,
			StartedAt: record.StartedAt.Time.UTC(), LastError: record.LastError}
		if record.RetireAt.Valid {
			item.RetireAt = record.RetireAt.Time.UTC()
		}
		report.Bundles = append(report.Bundles, item)
	}
	if len(records) > 0 {
		acks, listErr := session.store.Queries.ListNodeTrustAcks(ctx, records[0].Epoch)
		if listErr != nil {
			return listErr
		}
		for _, ack := range acks {
			item := pkiNodeStatus{Kind: ack.NodeKind, ID: ack.NodeID, Status: ack.Status, Epoch: ack.Epoch,
				BlocksCutover: ack.RequiredForCutover, UpdatedAt: ack.UpdatedAt.Time.UTC(), Error: ack.Error}
			if ack.AcknowledgedAt.Valid {
				item.AcknowledgedAt = ack.AcknowledgedAt.Time.UTC()
			}
			report.Nodes = append(report.Nodes, item)
		}
	}
	if source, getErr := session.clients.typed.CoreV1().ConfigMaps("cert-manager").Get(ctx, cfg.trustSourceName(), metav1.GetOptions{}); getErr == nil {
		report.TrustSourceSHA256 = source.Annotations["argus.io/trust-bundle-sha256"]
	}
	return writeOutput(a.stdout, output, report, func(writer io.Writer) {
		_, _ = fmt.Fprintf(writer, "Argus PKI mode=%s issuer=%s trust-source=%s sha256=%s\n", report.Mode, report.IssuerName, report.TrustSource, report.TrustSourceSHA256)
		for _, bundle := range report.Bundles {
			retirement := ""
			if !bundle.RetireAt.IsZero() {
				retirement = " retire-at=" + bundle.RetireAt.Format(time.RFC3339)
			}
			_, _ = fmt.Fprintf(writer, "epoch=%d state=%s direction=%s sha256=%s%s current=%s next=%s\n", bundle.Epoch, bundle.State, bundle.Direction, bundle.SHA256, retirement,
				strings.Join(bundle.CurrentCAFingerprints, ","), strings.Join(bundle.NextCAFingerprints, ","))
		}
		for _, node := range report.Nodes {
			_, _ = fmt.Fprintf(writer, "node=%s/%s epoch=%d status=%s blocks-cutover=%t updated=%s error=%s\n", node.Kind, node.ID, node.Epoch, node.Status,
				node.BlocksCutover, node.UpdatedAt.Format(time.RFC3339), node.Error)
		}
	})
}

func (a *App) pkiRotate(ctx context.Context, cfg *InstallConfig) error {
	session, err := a.openPKISession(ctx, cfg)
	if err != nil {
		return err
	}
	defer session.Close()
	bundles := trustbundle.Service{Store: session.store}
	current, err := bundles.Current(ctx)
	if err != nil {
		return err
	}
	if current.State == trustbundle.StateOverlapping {
		return fmt.Errorf("PKI rotation epoch %d is already overlapping until %s", current.Epoch, current.RetireAt.Format(time.RFC3339))
	}
	if current.State != trustbundle.StateStable && current.State != trustbundle.StatePreparing {
		return fmt.Errorf("PKI rotation cannot start from state %s", current.State)
	}
	rotationEpoch := current.Epoch
	var next trustbundle.Material
	if current.State == trustbundle.StateStable {
		activeControlPlaneIDs, listErr := activeControlPlaneNodeIDs(ctx, session.clients, cfg)
		if listErr != nil {
			return listErr
		}
		if err = requireRecentControlPlaneAcknowledgement(ctx, session.store, current.Epoch, activeControlPlaneIDs); err != nil {
			return err
		}
		rotationEpoch = current.Epoch + 1
		next, err = a.prepareNextIssuer(ctx, session.clients, cfg, rotationEpoch)
		if err != nil {
			return err
		}
		prepared, prepareErr := bundles.PrepareRotation(ctx, next.PEM, time.Now().UTC().Add(-10*time.Minute), activeControlPlaneIDs...)
		if prepareErr != nil {
			_ = a.cleanupRotationResources(ctx, session.clients, cfg, rotationEpoch, false)
			return prepareErr
		}
		current = prepared
		if err = persistPKITrustSource(ctx, session.clients, cfg, prepared.Material.PEM, prepared.Epoch); err != nil {
			_ = bundles.FailRotation(ctx, prepared.Epoch, err.Error())
			return err
		}
		_, _ = fmt.Fprintf(a.stdout, "Trust Bundle epoch %d published with old and new CA certificates; waiting for online-node ACKs\n", prepared.Epoch)
	} else {
		next, err = current.Material.Select(current.NextCAFingerprints)
		if err != nil {
			return err
		}
		if err = a.ensureRotationIssuer(ctx, session.clients, cfg, rotationEpoch, next); err != nil {
			return err
		}
		if err = persistPKITrustSource(ctx, session.clients, cfg, current.Material.PEM, current.Epoch); err != nil {
			return err
		}
	}
	if err = waitForTrustAcknowledgements(ctx, session.store, current.Epoch, 5*time.Minute); err != nil {
		return fmt.Errorf("rotation remains in preparing state and is safe to resume with the same command: %w", err)
	}
	certificates, err := listArgusCertificates(ctx, session.clients, cfg.Spec.ReleaseID)
	if err != nil {
		return err
	}
	servers, clients := partitionCertificates(certificates)
	if len(servers) == 0 || len(clients) == 0 {
		return errors.New("Argus rotation requires both server-only and client-only Certificate resources")
	}
	if cfg.Spec.PKI.Mode == PKIModeExistingClusterIssuer {
		if err = requireDistinctExistingRotationIssuer(servers, cfg.globalIssuerName()); err != nil {
			return err
		}
	}
	rotationIssuer := rotationIssuerName(cfg, rotationEpoch)
	stagedServers, err := stageServerCertificates(ctx, session.clients, cfg, servers, rotationIssuer, cfg.globalIssuerName(), "forward", rotationEpoch, next, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("pre-issue next server leaves: %w", err)
	}
	if err = switchCertificates(ctx, session.clients, clients, rotationIssuer, next, 5*time.Minute); err != nil {
		return fmt.Errorf("move control-plane client leaves to next issuer: %w", err)
	}
	if cfg.Spec.PKI.Mode == PKIModeManaged {
		former, selectErr := current.Material.Select(current.CurrentCAFingerprints)
		if selectErr != nil {
			return selectErr
		}
		if err = a.ensureManagedFormerIssuer(ctx, session.clients, cfg, rotationEpoch, former); err != nil {
			return err
		}
		if err = switchCertificates(ctx, session.clients, servers, formerIssuerName(cfg, rotationEpoch), former, 5*time.Minute); err != nil {
			return fmt.Errorf("pin serving leaves to the former issuer for overlap: %w", err)
		}
		if err = replaceManagedSteadyRoot(ctx, session.clients, cfg, rotationEpoch); err != nil {
			return err
		}
	}
	if err = probeClusterIssuer(ctx, session.clients, cfg, cfg.globalIssuerName(), rotationEpoch, next); err != nil {
		return fmt.Errorf("steady next ClusterIssuer probe failed: %w", err)
	}
	if err = switchCertificates(ctx, session.clients, append(clients, stagedServers...), cfg.globalIssuerName(), next, 5*time.Minute); err != nil {
		return fmt.Errorf("move next client and staged server leaves to the steady ClusterIssuer: %w", err)
	}
	if err = ensureTargetIssuerReaderRBAC(ctx, session.clients, cfg, cfg.globalIssuerName()); err != nil {
		return err
	}
	if err = switchRuntimeIssuers(ctx, session.clients, cfg, cfg.globalIssuerName(), rotationEpoch, 5*time.Minute); err != nil {
		return fmt.Errorf("switch runtime identity issuance to the next ClusterIssuer: %w", err)
	}
	// Runtime issuer changes restart the affected control-plane processes. Their
	// replacement pods must acknowledge the dual Bundle before the retirement
	// clock can start.
	if err = waitForTrustAcknowledgements(ctx, session.store, current.Epoch, 5*time.Minute); err != nil {
		return fmt.Errorf("rotation remains in preparing state after runtime issuer cutover and is safe to resume: %w", err)
	}
	overlap, _ := time.ParseDuration(cfg.Spec.PKI.Rotation.Overlap)
	promoted, err := bundles.PromoteOverlap(ctx, current.Epoch, overlap)
	if err != nil {
		return err
	}
	if err = a.cleanupNextIssuerResources(ctx, session.clients, cfg, rotationEpoch); err != nil {
		return fmt.Errorf("rotation entered overlap but temporary next-issuer cleanup failed: %w", err)
	}
	_, _ = fmt.Fprintf(a.stdout, "PKI rotation epoch %d entered overlap; serving leaves remain on the former CA until %s\n", promoted.Epoch, promoted.RetireAt.Format(time.RFC3339))
	return nil
}

func requireRecentControlPlaneAcknowledgement(ctx context.Context, store *postgres.Store, epoch int64, activeControlPlaneIDs []string) error {
	acks, err := store.Queries.ListNodeTrustAcks(ctx, epoch)
	if err != nil {
		return err
	}
	active := make(map[string]struct{}, len(activeControlPlaneIDs))
	for _, id := range activeControlPlaneIDs {
		active[id] = struct{}{}
	}
	cutoff := time.Now().UTC().Add(-10 * time.Minute)
	for _, ack := range acks {
		_, isActive := active[ack.NodeID]
		if ack.NodeKind == "control_plane" && isActive && ack.Status == "acked" && ack.UpdatedAt.Valid && ack.UpdatedAt.Time.After(cutoff) {
			return nil
		}
	}
	return errors.New("no currently Ready control-plane process acknowledged the current Bundle in the last 10 minutes")
}

func activeControlPlaneNodeIDs(ctx context.Context, clients *kubeClients, cfg *InstallConfig) ([]string, error) {
	namespaces := []string{cfg.Spec.Namespaces.System, cfg.Spec.Namespaces.Observability}
	ids := make([]string, 0, 8)
	for _, namespace := range namespaces {
		pods, err := clients.typed.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list Ready control-plane Pods in %s: %w", namespace, err)
		}
		for index := range pods.Items {
			if id, ok := controlPlaneNodeIDForPod(&pods.Items[index]); ok {
				ids = append(ids, id)
			}
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) == 0 {
		return nil, errors.New("no Ready Argus control-plane Pods were found")
	}
	return ids, nil
}

func controlPlaneNodeIDForPod(pod *corev1.Pod) (string, bool) {
	if pod == nil || pod.Name == "" || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning || !podReady(pod) {
		return "", false
	}
	appName := pod.Labels["app.kubernetes.io/name"]
	component := ""
	switch appName {
	case "argus-server":
		component = "server"
	case "argus-connector-gateway":
		component = "connector-gateway"
	case "argus-direct-executor":
		component = "direct-executor"
	case "argus-telemetry-ingest":
		component = "telemetry-ingest"
	case "argus-telemetry-query":
		component = "telemetry-query"
	default:
		if strings.HasPrefix(appName, "argus-worker") {
			component = "worker-" + workerPoolFromPod(pod)
		}
	}
	if component == "" {
		return "", false
	}
	return component + "/" + pod.Name, true
}

func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func workerPoolFromPod(pod *corev1.Pod) string {
	for _, container := range pod.Spec.Containers {
		for index, argument := range container.Args {
			if strings.HasPrefix(argument, "--pool=") {
				if pool := strings.TrimSpace(strings.TrimPrefix(argument, "--pool=")); pool != "" {
					return pool
				}
			}
			if argument == "--pool" && index+1 < len(container.Args) {
				if pool := strings.TrimSpace(container.Args[index+1]); pool != "" {
					return pool
				}
			}
		}
	}
	return "default"
}

func waitForTrustAcknowledgements(ctx context.Context, store *postgres.Store, epoch int64, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		count, err := store.Queries.CountUnacknowledgedTrustNodes(ctx, epoch)
		return count == 0, err
	})
}

func (a *App) prepareNextIssuer(ctx context.Context, clients *kubeClients, cfg *InstallConfig, epoch int64) (trustbundle.Material, error) {
	var material trustbundle.Material
	var err error
	if cfg.Spec.PKI.Mode == PKIModeManaged {
		name := rotationRootSecretName(cfg, epoch)
		secrets := clients.typed.CoreV1().Secrets("cert-manager")
		secret, getErr := secrets.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) {
			secret, err = newManagedRootCASecret(cfg.Spec.ReleaseID, name, epoch, time.Now().UTC())
			if err == nil {
				secret, err = secrets.Create(ctx, secret, metav1.CreateOptions{})
			}
		} else {
			err = getErr
		}
		if err != nil {
			return material, fmt.Errorf("create next managed root CA: %w", err)
		}
		if err = validateManagedRootCA(secret, cfg.Spec.ReleaseID); err != nil {
			return material, err
		}
		material, err = trustbundle.Parse(secret.Data[corev1.TLSCertKey], time.Now().UTC())
	} else {
		var value []byte
		value, err = cfg.CABundlePEM()
		if err == nil {
			material, err = trustbundle.Parse(value, time.Now().UTC())
		}
	}
	if err != nil {
		return material, err
	}
	if err = a.ensureRotationIssuer(ctx, clients, cfg, epoch, material); err != nil {
		return material, err
	}
	if err = probeClusterIssuer(ctx, clients, cfg, rotationIssuerName(cfg, epoch), epoch, material); err != nil {
		return material, err
	}
	return material, nil
}

func configuredIssuerMaterial(ctx context.Context, clients *kubeClients, cfg *InstallConfig) (trustbundle.Material, error) {
	if cfg.Spec.PKI.Mode == PKIModeExistingClusterIssuer {
		value, err := cfg.CABundlePEM()
		if err != nil {
			return trustbundle.Material{}, err
		}
		return trustbundle.Parse(value, time.Now().UTC())
	}
	secret, err := clients.typed.CoreV1().Secrets("cert-manager").Get(ctx, cfg.Spec.ReleaseID+"-root-ca", metav1.GetOptions{})
	if err != nil {
		return trustbundle.Material{}, err
	}
	if err = validateManagedRootCA(secret, cfg.Spec.ReleaseID); err != nil {
		return trustbundle.Material{}, err
	}
	return trustbundle.Parse(secret.Data[corev1.TLSCertKey], time.Now().UTC())
}

func (a *App) ensureRotationIssuer(ctx context.Context, clients *kubeClients, cfg *InstallConfig, epoch int64, material trustbundle.Material) error {
	name := rotationIssuerName(cfg, epoch)
	resource := clients.dynamic.Resource(pkiIssuerGVR)
	existing, err := resource.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if existing.GetLabels()["argus.io/release-id"] != cfg.Spec.ReleaseID || existing.GetAnnotations()["argus.io/pki-epoch"] != fmt.Sprintf("%d", epoch) {
			return fmt.Errorf("rotation ClusterIssuer %s is not owned by this PKI epoch", name)
		}
		return waitForIssuerReady(ctx, clients, name, 2*time.Minute)
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	var spec map[string]any
	if cfg.Spec.PKI.Mode == PKIModeManaged {
		spec = map[string]any{"ca": map[string]any{"secretName": rotationRootSecretName(cfg, epoch)}}
	} else {
		steady, getErr := resource.Get(ctx, cfg.globalIssuerName(), metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("read customer ClusterIssuer %s: %w", cfg.globalIssuerName(), getErr)
		}
		if !issuerReady(steady) {
			return fmt.Errorf("customer ClusterIssuer %s is not Ready", cfg.globalIssuerName())
		}
		value, found, nestedErr := unstructured.NestedMap(steady.Object, "spec")
		if nestedErr != nil || !found {
			return fmt.Errorf("customer ClusterIssuer %s has no valid spec", cfg.globalIssuerName())
		}
		spec = value
	}
	issuer := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cert-manager.io/v1", "kind": "ClusterIssuer",
		"metadata": map[string]any{"name": name,
			"labels":      map[string]any{"app.kubernetes.io/part-of": "argus", "argus.io/release-id": cfg.Spec.ReleaseID, "argus.io/pki-role": "rotation-issuer"},
			"annotations": map[string]any{"argus.io/pki-epoch": fmt.Sprintf("%d", epoch), "argus.io/trust-bundle-sha256": material.SHA256}},
		"spec": spec,
	}}
	if _, err = resource.Create(ctx, issuer, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create rotation ClusterIssuer: %w", err)
	}
	return waitForIssuerReady(ctx, clients, name, 2*time.Minute)
}

func issuerReady(issuer *unstructured.Unstructured) bool {
	conditions, found, _ := unstructured.NestedSlice(issuer.Object, "status", "conditions")
	if !found {
		return false
	}
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if ok && condition["type"] == "Ready" && condition["status"] == "True" {
			return true
		}
	}
	return false
}

func waitForIssuerReady(ctx context.Context, clients *kubeClients, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		issuer, err := clients.dynamic.Resource(pkiIssuerGVR).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return issuerReady(issuer), nil
	})
}

func probeClusterIssuer(ctx context.Context, clients *kubeClients, cfg *InstallConfig, issuer string, epoch int64, material trustbundle.Material) error {
	namespace := cfg.Spec.Namespaces.System
	prefix := fmt.Sprintf("argus-pki-probe-%d", epoch)
	types := []struct {
		name, usage, dnsName, uri string
	}{
		{name: prefix + "-server", usage: "server auth", dnsName: prefix + "." + namespace + ".svc"},
		{name: prefix + "-client", usage: "client auth", uri: fmt.Sprintf("spiffe://argus.io/pki/probes/%d/client", epoch)},
	}
	for _, item := range types {
		certificates := clients.dynamic.Resource(pkiCertificateGVR).Namespace(namespace)
		_ = certificates.Delete(ctx, item.name, metav1.DeleteOptions{})
		_ = clients.typed.CoreV1().Secrets(namespace).Delete(ctx, item.name+"-tls", metav1.DeleteOptions{})
		spec := map[string]any{
			"secretName": item.name + "-tls", "duration": "1h", "renewBefore": "15m", "usages": []any{item.usage},
			"privateKey": map[string]any{"algorithm": "ECDSA", "size": int64(256), "rotationPolicy": "Always"},
			"issuerRef":  map[string]any{"name": issuer, "kind": "ClusterIssuer", "group": "cert-manager.io"},
		}
		if item.dnsName != "" {
			spec["dnsNames"] = []any{item.dnsName}
		}
		if item.uri != "" {
			spec["uris"] = []any{item.uri}
		}
		certificate := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "cert-manager.io/v1", "kind": "Certificate",
			"metadata": map[string]any{"name": item.name, "namespace": namespace,
				"labels": map[string]any{"app.kubernetes.io/part-of": "argus", "argus.io/release-id": cfg.Spec.ReleaseID, "argus.io/pki-role": "issuer-probe"}},
			"spec": spec,
		}}
		if _, err := certificates.Create(ctx, certificate, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create %s issuer probe: %w", item.usage, err)
		}
		defer func(name string) {
			_ = certificates.Delete(context.Background(), name, metav1.DeleteOptions{})
			_ = clients.typed.CoreV1().Secrets(namespace).Delete(context.Background(), name+"-tls", metav1.DeleteOptions{})
		}(item.name)
		if err := waitForIssuedSecret(ctx, clients, namespace, item.name+"-tls", material, item.usage, item.dnsName, item.uri, 2*time.Minute); err != nil {
			return fmt.Errorf("%s ClusterIssuer probe failed: %w", item.usage, err)
		}
	}
	return nil
}

func waitForIssuedSecret(ctx context.Context, clients *kubeClients, namespace, name string, material trustbundle.Material, usage, dnsName, uri string, timeout time.Duration) error {
	var lastError error
	err := wait.PollUntilContextTimeout(ctx, time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		secret, err := clients.typed.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		lastError = verifyIssuedLeaf(secret, material, usage, dnsName, uri)
		return lastError == nil, nil
	})
	if err != nil && lastError != nil {
		return lastError
	}
	return err
}

func verifyIssuedLeaf(secret *corev1.Secret, material trustbundle.Material, usage, dnsName, uri string) error {
	pair, err := tls.X509KeyPair(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
	if err != nil || len(pair.Certificate) == 0 {
		return errors.New("issued Secret does not contain a matching certificate and private key")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return err
	}
	var expected x509.ExtKeyUsage
	switch usage {
	case "server auth":
		expected = x509.ExtKeyUsageServerAuth
	case "client auth":
		expected = x509.ExtKeyUsageClientAuth
	default:
		return fmt.Errorf("unsupported certificate usage %q", usage)
	}
	if len(leaf.ExtKeyUsage) != 1 || leaf.ExtKeyUsage[0] != expected || len(leaf.UnknownExtKeyUsage) != 0 {
		return fmt.Errorf("issued leaf does not have exact %s EKU", usage)
	}
	roots, intermediates := x509.NewCertPool(), x509.NewCertPool()
	for _, certificate := range material.Certificates {
		roots.AddCert(certificate)
		intermediates.AddCert(certificate)
	}
	for _, raw := range pair.Certificate[1:] {
		certificate, parseErr := x509.ParseCertificate(raw)
		if parseErr != nil {
			return parseErr
		}
		intermediates.AddCert(certificate)
	}
	options := x509.VerifyOptions{Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{expected}, DNSName: dnsName, CurrentTime: time.Now().UTC()}
	if _, err = leaf.Verify(options); err != nil {
		return fmt.Errorf("issued leaf does not chain to the supplied Bundle: %w", err)
	}
	if uri != "" && (len(leaf.URIs) != 1 || leaf.URIs[0].String() != uri) {
		return fmt.Errorf("issued leaf URI SAN does not equal %s", uri)
	}
	return nil
}

type argusCertificate struct {
	Namespace  string
	Name       string
	SecretName string
	Usage      string
	IssuerName string
}

func listArgusCertificates(ctx context.Context, clients *kubeClients, releaseID string) ([]argusCertificate, error) {
	list, err := clients.dynamic.Resource(pkiCertificateGVR).List(ctx, metav1.ListOptions{LabelSelector: "argus.io/release-id=" + releaseID})
	if err != nil {
		return nil, fmt.Errorf("list Argus Certificates: %w", err)
	}
	result := make([]argusCertificate, 0, len(list.Items))
	for _, item := range list.Items {
		if item.GetLabels()[pkiRoleLabel] == pkiStagedServerRole || item.GetLabels()[pkiRoleLabel] == "issuer-probe" {
			continue
		}
		secretName, found, _ := unstructured.NestedString(item.Object, "spec", "secretName")
		usages, _, _ := unstructured.NestedStringSlice(item.Object, "spec", "usages")
		issuerName, issuerFound, _ := unstructured.NestedString(item.Object, "spec", "issuerRef", "name")
		if !found || !issuerFound || len(usages) != 1 || (usages[0] != "server auth" && usages[0] != "client auth") {
			return nil, fmt.Errorf("Certificate %s/%s must have one explicit server auth or client auth usage", item.GetNamespace(), item.GetName())
		}
		result = append(result, argusCertificate{Namespace: item.GetNamespace(), Name: item.GetName(), SecretName: secretName, Usage: usages[0], IssuerName: issuerName})
	}
	if len(result) == 0 {
		return nil, errors.New("no Argus Certificate resources were found")
	}
	slices.SortFunc(result, func(left, right argusCertificate) int {
		return strings.Compare(left.Namespace+"/"+left.Name, right.Namespace+"/"+right.Name)
	})
	return result, nil
}

func partitionCertificates(certificates []argusCertificate) (servers, clients []argusCertificate) {
	for _, certificate := range certificates {
		if certificate.Usage == "server auth" {
			servers = append(servers, certificate)
		} else {
			clients = append(clients, certificate)
		}
	}
	return servers, clients
}

func requireDistinctExistingRotationIssuer(servers []argusCertificate, nextIssuer string) error {
	for _, certificate := range servers {
		if certificate.IssuerName == nextIssuer {
			return fmt.Errorf("existing-cluster-issuer rotation requires a distinct next ClusterIssuer; serving Certificate %s/%s already references %s", certificate.Namespace, certificate.Name, nextIssuer)
		}
	}
	return nil
}

func stageServerCertificates(ctx context.Context, clients *kubeClients, cfg *InstallConfig, servers []argusCertificate, issuer, targetIssuer, direction string,
	epoch int64, material trustbundle.Material, timeout time.Duration) ([]argusCertificate, error) {
	if targetIssuer == "" || (direction != "forward" && direction != "rollback") {
		return nil, errors.New("staged server cutover target is invalid")
	}
	staged := make([]argusCertificate, 0, len(servers))
	for _, server := range servers {
		resource := clients.dynamic.Resource(pkiCertificateGVR).Namespace(server.Namespace)
		source, err := resource.Get(ctx, server.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		spec, found, err := unstructured.NestedMap(source.Object, "spec")
		if err != nil || !found {
			return nil, fmt.Errorf("Certificate %s/%s has no valid spec", server.Namespace, server.Name)
		}
		name := stagedServerCertificateName(server.Name, epoch)
		secretName := name
		spec["secretName"] = secretName
		spec["issuerRef"] = map[string]any{"name": issuer, "kind": "ClusterIssuer", "group": "cert-manager.io"}
		labels := map[string]any{"app.kubernetes.io/part-of": "argus", "argus.io/release-id": cfg.Spec.ReleaseID, pkiRoleLabel: pkiStagedServerRole}
		annotations := map[string]any{pkiEpochAnnotation: fmt.Sprint(epoch), pkiSourceCertificate: server.Name,
			pkiSourceSecret: server.SecretName, pkiTargetIssuer: targetIssuer, pkiFormerIssuer: server.IssuerName,
			"argus.io/pki-direction": direction}
		candidate := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "cert-manager.io/v1", "kind": "Certificate",
			"metadata": map[string]any{"name": name, "namespace": server.Namespace, "labels": labels, "annotations": annotations},
			"spec":     spec,
		}}
		existing, getErr := resource.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) {
			if _, err = resource.Create(ctx, candidate, metav1.CreateOptions{}); err != nil {
				return nil, fmt.Errorf("create staged Certificate %s/%s: %w", server.Namespace, name, err)
			}
		} else if getErr != nil {
			return nil, getErr
		} else {
			if existing.GetLabels()["argus.io/release-id"] != cfg.Spec.ReleaseID || existing.GetAnnotations()[pkiEpochAnnotation] != fmt.Sprint(epoch) ||
				existing.GetAnnotations()[pkiSourceCertificate] != server.Name || existing.GetAnnotations()[pkiSourceSecret] != server.SecretName ||
				existing.GetAnnotations()[pkiTargetIssuer] != targetIssuer || existing.GetAnnotations()[pkiFormerIssuer] != server.IssuerName ||
				existing.GetAnnotations()["argus.io/pki-direction"] != direction {
				return nil, fmt.Errorf("staged Certificate %s/%s is not owned by this rotation", server.Namespace, name)
			}
			candidate.SetResourceVersion(existing.GetResourceVersion())
			if _, err = resource.Update(ctx, candidate, metav1.UpdateOptions{}); err != nil {
				return nil, fmt.Errorf("update staged Certificate %s/%s: %w", server.Namespace, name, err)
			}
		}
		staged = append(staged, argusCertificate{Namespace: server.Namespace, Name: name, SecretName: secretName, Usage: "server auth", IssuerName: issuer})
	}
	deadline := time.Now().Add(timeout)
	for _, certificate := range staged {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, errors.New("timed out waiting for staged server certificates")
		}
		if err := waitForIssuedSecret(ctx, clients, certificate.Namespace, certificate.SecretName, material, certificate.Usage, "", "", remaining); err != nil {
			return nil, fmt.Errorf("wait for staged Certificate %s/%s: %w", certificate.Namespace, certificate.Name, err)
		}
	}
	return staged, nil
}

func stagedServerCertificateName(source string, epoch int64) string {
	suffix := fmt.Sprintf("-next-%d", epoch)
	limit := 253 - len(suffix)
	if len(source) > limit {
		source = strings.TrimRight(source[:limit], "-.")
	}
	return source + suffix
}

func switchCertificates(ctx context.Context, clients *kubeClients, certificates []argusCertificate, issuer string, material trustbundle.Material, timeout time.Duration) error {
	for _, certificate := range certificates {
		resource := clients.dynamic.Resource(pkiCertificateGVR).Namespace(certificate.Namespace)
		object, err := resource.Get(ctx, certificate.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if err = unstructured.SetNestedField(object.Object, issuer, "spec", "issuerRef", "name"); err != nil {
			return err
		}
		if err = unstructured.SetNestedField(object.Object, "ClusterIssuer", "spec", "issuerRef", "kind"); err != nil {
			return err
		}
		if err = unstructured.SetNestedField(object.Object, "cert-manager.io", "spec", "issuerRef", "group"); err != nil {
			return err
		}
		if _, err = resource.Update(ctx, object, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update Certificate %s/%s issuer: %w", certificate.Namespace, certificate.Name, err)
		}
	}
	deadline := time.Now().Add(timeout)
	for _, certificate := range certificates {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errors.New("timed out waiting for Argus leaf certificate rotation")
		}
		if err := waitForIssuedSecret(ctx, clients, certificate.Namespace, certificate.SecretName, material, certificate.Usage, "", "", remaining); err != nil {
			return fmt.Errorf("wait for Certificate %s/%s: %w", certificate.Namespace, certificate.Name, err)
		}
	}
	return nil
}

func (a *App) ensureManagedFormerIssuer(ctx context.Context, clients *kubeClients, cfg *InstallConfig, epoch int64, former trustbundle.Material) error {
	secrets := clients.typed.CoreV1().Secrets("cert-manager")
	baseName := cfg.Spec.ReleaseID + "-root-ca"
	archiveName := archivedRootSecretName(cfg, epoch)
	archive, err := secrets.Get(ctx, archiveName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		current, getErr := secrets.Get(ctx, baseName, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		if getErr = validateManagedRootCA(current, cfg.Spec.ReleaseID); getErr != nil {
			return getErr
		}
		archive = current.DeepCopy()
		archive.ObjectMeta = metav1.ObjectMeta{Name: archiveName, Labels: copyStringMap(current.Labels), Annotations: copyStringMap(current.Annotations)}
		archive.Labels["argus.io/pki-phase"] = "retired-candidate"
		archive.Annotations[pkiEpochAnnotation] = fmt.Sprint(epoch)
		archive, err = secrets.Create(ctx, archive, metav1.CreateOptions{})
	}
	if err != nil {
		return fmt.Errorf("archive former managed root: %w", err)
	}
	if err = validateManagedRootCA(archive, cfg.Spec.ReleaseID); err != nil {
		return err
	}
	archivedMaterial, err := trustbundle.Parse(archive.Data[corev1.TLSCertKey], time.Now().UTC())
	if err != nil || archivedMaterial.SHA256 != former.SHA256 {
		return errors.New("archived managed root does not match the former Trust Bundle")
	}

	name := formerIssuerName(cfg, epoch)
	resource := clients.dynamic.Resource(pkiIssuerGVR)
	issuer, getErr := resource.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(getErr) {
		issuer = &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "cert-manager.io/v1", "kind": "ClusterIssuer",
			"metadata": map[string]any{"name": name,
				"labels":      map[string]any{"app.kubernetes.io/part-of": "argus", "argus.io/release-id": cfg.Spec.ReleaseID, pkiRoleLabel: pkiFormerIssuerRole},
				"annotations": map[string]any{pkiEpochAnnotation: fmt.Sprint(epoch), "argus.io/trust-bundle-sha256": former.SHA256}},
			"spec": map[string]any{"ca": map[string]any{"secretName": archiveName}},
		}}
		if _, err = resource.Create(ctx, issuer, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create former managed ClusterIssuer: %w", err)
		}
	} else if getErr != nil {
		return getErr
	} else if issuer.GetLabels()["argus.io/release-id"] != cfg.Spec.ReleaseID || issuer.GetAnnotations()[pkiEpochAnnotation] != fmt.Sprint(epoch) {
		return fmt.Errorf("former ClusterIssuer %s is not owned by this rotation", name)
	}
	if err = waitForIssuerReady(ctx, clients, name, 2*time.Minute); err != nil {
		return err
	}
	if err = probeClusterIssuer(ctx, clients, cfg, name, epoch, former); err != nil {
		return fmt.Errorf("former managed ClusterIssuer probe failed: %w", err)
	}
	if err = ensureManagedRootCleanupRBAC(ctx, clients, cfg, archiveName); err != nil {
		return err
	}
	return ensureFormerIssuerCleanupRBAC(ctx, clients, cfg, name)
}

func ensureFormerIssuerCleanupRBAC(ctx context.Context, clients *kubeClients, cfg *InstallConfig, issuerName string) error {
	name := cfg.Spec.ReleaseID + "-pki-former-issuer-cleanup"
	roles := clients.typed.RbacV1().ClusterRoles()
	role, err := roles.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		role = &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
			"app.kubernetes.io/part-of": "argus", "argus.io/release-id": cfg.Spec.ReleaseID,
		}}}
	} else if err != nil {
		return err
	} else if role.Labels["argus.io/release-id"] != cfg.Spec.ReleaseID {
		return errors.New("refusing to update former-issuer cleanup ClusterRole owned by another release")
	}
	role.Rules = []rbacv1.PolicyRule{{APIGroups: []string{"cert-manager.io"}, Resources: []string{"clusterissuers"},
		ResourceNames: []string{issuerName}, Verbs: []string{"get", "delete"}}}
	if role.ResourceVersion == "" {
		_, err = roles.Create(ctx, role, metav1.CreateOptions{})
	} else {
		_, err = roles.Update(ctx, role, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("grant exact former-issuer cleanup permission: %w", err)
	}

	bindings := clients.typed.RbacV1().ClusterRoleBindings()
	binding, err := bindings.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		binding = &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
			"app.kubernetes.io/part-of": "argus", "argus.io/release-id": cfg.Spec.ReleaseID,
		}}}
	} else if err != nil {
		return err
	} else if binding.Labels["argus.io/release-id"] != cfg.Spec.ReleaseID {
		return errors.New("refusing to update former-issuer cleanup ClusterRoleBinding owned by another release")
	}
	binding.Subjects = []rbacv1.Subject{{Kind: "ServiceAccount", Name: "argus-pki-controller", Namespace: cfg.Spec.Namespaces.System}}
	binding.RoleRef = rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: name}
	if binding.ResourceVersion == "" {
		_, err = bindings.Create(ctx, binding, metav1.CreateOptions{})
	} else {
		_, err = bindings.Update(ctx, binding, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("bind exact former-issuer cleanup permission: %w", err)
	}
	return nil
}

func ensureTargetIssuerReaderRBAC(ctx context.Context, clients *kubeClients, cfg *InstallConfig, issuerName string) error {
	if strings.TrimSpace(issuerName) == "" {
		return errors.New("target ClusterIssuer name is empty")
	}
	name := cfg.Spec.ReleaseID + "-pki-target-issuer-reader"
	roles := clients.typed.RbacV1().ClusterRoles()
	role, err := roles.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		role = &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
			"app.kubernetes.io/part-of": "argus", "argus.io/release-id": cfg.Spec.ReleaseID,
		}}}
	} else if err != nil {
		return err
	} else if role.Labels["argus.io/release-id"] != cfg.Spec.ReleaseID {
		return errors.New("refusing to update target-issuer reader ClusterRole owned by another release")
	}
	role.Rules = []rbacv1.PolicyRule{{APIGroups: []string{"cert-manager.io"}, Resources: []string{"clusterissuers"},
		ResourceNames: []string{issuerName}, Verbs: []string{"get"}}}
	if role.ResourceVersion == "" {
		_, err = roles.Create(ctx, role, metav1.CreateOptions{})
	} else {
		_, err = roles.Update(ctx, role, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("grant exact target-issuer read permission: %w", err)
	}

	bindings := clients.typed.RbacV1().ClusterRoleBindings()
	binding, err := bindings.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		binding = &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
			"app.kubernetes.io/part-of": "argus", "argus.io/release-id": cfg.Spec.ReleaseID,
		}}}
	} else if err != nil {
		return err
	} else if binding.Labels["argus.io/release-id"] != cfg.Spec.ReleaseID {
		return errors.New("refusing to update target-issuer reader ClusterRoleBinding owned by another release")
	}
	binding.Subjects = []rbacv1.Subject{{Kind: "ServiceAccount", Name: "argus-pki-controller", Namespace: cfg.Spec.Namespaces.System}}
	binding.RoleRef = rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: name}
	if binding.ResourceVersion == "" {
		_, err = bindings.Create(ctx, binding, metav1.CreateOptions{})
	} else {
		_, err = bindings.Update(ctx, binding, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("bind exact target-issuer read permission: %w", err)
	}
	return nil
}

func switchRuntimeIssuers(ctx context.Context, clients *kubeClients, cfg *InstallConfig, issuerName string, generation int64, timeout time.Duration) error {
	if issuerName == "" || generation < 1 {
		return errors.New("runtime issuer target is invalid")
	}
	configMaps := clients.typed.CoreV1().ConfigMaps(cfg.Spec.Namespaces.System)
	runtimeConfig, err := configMaps.Get(ctx, "argus-runtime-config", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if runtimeConfig.Labels["argus.io/release-id"] != cfg.Spec.ReleaseID {
		return errors.New("refusing to update runtime issuer configuration not owned by this release")
	}
	copy := runtimeConfig.DeepCopy()
	if copy.Data == nil {
		copy.Data = map[string]string{}
	}
	copy.Data["ARGUS_CONNECTOR_ISSUER_NAME"] = issuerName
	copy.Data["ARGUS_CONNECTOR_ISSUER_GENERATION"] = fmt.Sprint(generation)
	copy.Data["ARGUS_TELEMETRY_ISSUER_NAME"] = issuerName
	copy.Data["ARGUS_TELEMETRY_ISSUER_GENERATION"] = fmt.Sprint(generation)
	if _, err = configMaps.Update(ctx, copy, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update runtime issuer configuration: %w", err)
	}

	rolloutValue := fmt.Sprintf("%d:%s", generation, issuerName)
	changed := make([][2]string, 0)
	deployments := clients.typed.AppsV1().Deployments(cfg.Spec.Namespaces.System)
	list, err := deployments.List(ctx, metav1.ListOptions{LabelSelector: "argus.io/release-id=" + cfg.Spec.ReleaseID})
	if err != nil {
		return err
	}
	for index := range list.Items {
		deployment := &list.Items[index]
		if !runtimeIssuerWorkload(deployment.Name) || !deploymentReferencesConfigMap(deployment, "argus-runtime-config") {
			continue
		}
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = map[string]string{}
		}
		deployment.Spec.Template.Annotations["argus.io/pki-runtime-issuer"] = rolloutValue
		if _, err = deployments.Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("restart %s/%s for runtime issuer change: %w", deployment.Namespace, deployment.Name, err)
		}
		changed = append(changed, [2]string{deployment.Namespace, deployment.Name})
	}

	telemetryDeployments := clients.typed.AppsV1().Deployments(cfg.Spec.Namespaces.Observability)
	ingest, err := telemetryDeployments.Get(ctx, "argus-telemetry-ingest", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read telemetry identity issuer workload: %w", err)
	}
	if ingest.Labels["argus.io/release-id"] != cfg.Spec.ReleaseID || len(ingest.Spec.Template.Spec.Containers) != 1 {
		return errors.New("telemetry identity issuer workload is not owned by this release")
	}
	setContainerEnv(&ingest.Spec.Template.Spec.Containers[0], "ARGUS_TELEMETRY_ISSUER_NAME", issuerName)
	setContainerEnv(&ingest.Spec.Template.Spec.Containers[0], "ARGUS_TELEMETRY_ISSUER_GENERATION", fmt.Sprint(generation))
	if ingest.Spec.Template.Annotations == nil {
		ingest.Spec.Template.Annotations = map[string]string{}
	}
	ingest.Spec.Template.Annotations["argus.io/pki-runtime-issuer"] = rolloutValue
	if _, err = telemetryDeployments.Update(ctx, ingest, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("restart telemetry identity issuer workload: %w", err)
	}
	changed = append(changed, [2]string{ingest.Namespace, ingest.Name})
	return waitForDeploymentRollouts(ctx, clients, changed, timeout)
}

func runtimeIssuerWorkload(name string) bool {
	return name == "argus-server" || name == "argus-connector-gateway" || name == "argus-worker" || strings.HasPrefix(name, "argus-worker-")
}

func deploymentReferencesConfigMap(deployment *appsv1.Deployment, name string) bool {
	for _, container := range deployment.Spec.Template.Spec.Containers {
		for _, source := range container.EnvFrom {
			if source.ConfigMapRef != nil && source.ConfigMapRef.Name == name {
				return true
			}
		}
	}
	return false
}

func setContainerEnv(container *corev1.Container, name, value string) {
	for index := range container.Env {
		if container.Env[index].Name == name {
			container.Env[index].Value = value
			container.Env[index].ValueFrom = nil
			return
		}
	}
	container.Env = append(container.Env, corev1.EnvVar{Name: name, Value: value})
}

func waitForDeploymentRollouts(ctx context.Context, clients *kubeClients, deployments [][2]string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for _, item := range deployments {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errors.New("timed out waiting for runtime issuer rollout")
		}
		err := wait.PollUntilContextTimeout(ctx, 2*time.Second, remaining, true, func(ctx context.Context) (bool, error) {
			deployment, err := clients.typed.AppsV1().Deployments(item[0]).Get(ctx, item[1], metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			desired := int32(1)
			if deployment.Spec.Replicas != nil {
				desired = *deployment.Spec.Replicas
			}
			return deployment.Status.ObservedGeneration >= deployment.Generation && deployment.Status.UpdatedReplicas == desired &&
				deployment.Status.AvailableReplicas == desired && deployment.Status.UnavailableReplicas == 0, nil
		})
		if err != nil {
			return fmt.Errorf("wait for runtime issuer rollout %s/%s: %w", item[0], item[1], err)
		}
	}
	return nil
}

func replaceManagedSteadyRoot(ctx context.Context, clients *kubeClients, cfg *InstallConfig, epoch int64) error {
	secrets := clients.typed.CoreV1().Secrets("cert-manager")
	baseName := cfg.Spec.ReleaseID + "-root-ca"
	archiveName := archivedRootSecretName(cfg, epoch)
	current, err := secrets.Get(ctx, baseName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err = validateManagedRootCA(current, cfg.Spec.ReleaseID); err != nil {
		return err
	}
	next, err := secrets.Get(ctx, rotationRootSecretName(cfg, epoch), metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err = validateManagedRootCA(next, cfg.Spec.ReleaseID); err != nil {
		return err
	}
	if _, getErr := secrets.Get(ctx, archiveName, metav1.GetOptions{}); apierrors.IsNotFound(getErr) {
		archive := current.DeepCopy()
		archive.ObjectMeta = metav1.ObjectMeta{Name: archiveName, Labels: copyStringMap(current.Labels), Annotations: copyStringMap(current.Annotations)}
		archive.Labels["argus.io/pki-phase"] = "retired-candidate"
		if _, err = secrets.Create(ctx, archive, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("archive current managed root: %w", err)
		}
	} else if getErr != nil {
		return getErr
	}
	if err = ensureManagedRootCleanupRBAC(ctx, clients, cfg, archiveName); err != nil {
		return err
	}
	replacement := next.DeepCopy()
	replacement.ObjectMeta = metav1.ObjectMeta{Name: baseName, Labels: copyStringMap(next.Labels), Annotations: copyStringMap(next.Annotations)}
	replacement.Labels["argus.io/pki-phase"] = "steady"
	if err = secrets.Delete(ctx, baseName, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("remove former steady managed root: %w", err)
	}
	if _, err = secrets.Create(ctx, replacement, metav1.CreateOptions{}); err != nil {
		restore := current.DeepCopy()
		restore.ObjectMeta = metav1.ObjectMeta{Name: baseName, Labels: copyStringMap(current.Labels), Annotations: copyStringMap(current.Annotations)}
		_, restoreErr := secrets.Create(ctx, restore, metav1.CreateOptions{})
		return fmt.Errorf("install next steady managed root: %w (restore result: %v)", err, restoreErr)
	}
	return waitForIssuerReady(ctx, clients, cfg.globalIssuerName(), 2*time.Minute)
}

func ensureManagedRootCleanupRBAC(ctx context.Context, clients *kubeClients, cfg *InstallConfig, archiveName string) error {
	const namespace = "cert-manager"
	name := cfg.Spec.ReleaseID + "-pki-root-cleanup"
	roles := clients.typed.RbacV1().Roles(namespace)
	role, err := roles.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		role = &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
			"app.kubernetes.io/part-of": "argus", "argus.io/release-id": cfg.Spec.ReleaseID,
		}}}
	} else if err != nil {
		return err
	} else if role.Labels["argus.io/release-id"] != cfg.Spec.ReleaseID {
		return fmt.Errorf("refusing to update root-cleanup Role owned by another release")
	}
	role.Rules = []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"secrets"}, ResourceNames: []string{archiveName}, Verbs: []string{"get", "delete"}}}
	if role.ResourceVersion == "" {
		_, err = roles.Create(ctx, role, metav1.CreateOptions{})
	} else {
		_, err = roles.Update(ctx, role, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("grant exact retired-root cleanup permission: %w", err)
	}
	bindings := clients.typed.RbacV1().RoleBindings(namespace)
	binding, err := bindings.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		binding = &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
			"app.kubernetes.io/part-of": "argus", "argus.io/release-id": cfg.Spec.ReleaseID,
		}}}
	} else if err != nil {
		return err
	} else if binding.Labels["argus.io/release-id"] != cfg.Spec.ReleaseID {
		return fmt.Errorf("refusing to update root-cleanup RoleBinding owned by another release")
	}
	binding.Subjects = []rbacv1.Subject{{Kind: "ServiceAccount", Name: "argus-pki-controller", Namespace: cfg.Spec.Namespaces.System}}
	binding.RoleRef = rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: name}
	if binding.ResourceVersion == "" {
		_, err = bindings.Create(ctx, binding, metav1.CreateOptions{})
	} else {
		_, err = bindings.Update(ctx, binding, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("bind exact retired-root cleanup permission: %w", err)
	}
	return nil
}

func copyStringMap(value map[string]string) map[string]string {
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func rotationIssuerName(cfg *InstallConfig, epoch int64) string {
	return fmt.Sprintf("%s-ca-next-%d", cfg.Spec.ReleaseID, epoch)
}

func rotationRootSecretName(cfg *InstallConfig, epoch int64) string {
	return fmt.Sprintf("%s-root-ca-next-%d", cfg.Spec.ReleaseID, epoch)
}

func formerIssuerName(cfg *InstallConfig, epoch int64) string {
	return fmt.Sprintf("%s-ca-former-%d", cfg.Spec.ReleaseID, epoch)
}

func archivedRootSecretName(cfg *InstallConfig, epoch int64) string {
	return fmt.Sprintf("%s-root-ca-previous-%d", cfg.Spec.ReleaseID, epoch)
}

func (a *App) cleanupNextIssuerResources(ctx context.Context, clients *kubeClients, cfg *InstallConfig, epoch int64) error {
	if err := clients.dynamic.Resource(pkiIssuerGVR).Delete(ctx, rotationIssuerName(cfg, epoch), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if cfg.Spec.PKI.Mode == PKIModeManaged {
		if err := clients.typed.CoreV1().Secrets("cert-manager").Delete(ctx, rotationRootSecretName(cfg, epoch), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func cleanupStagedServerCertificates(ctx context.Context, clients *kubeClients, cfg *InstallConfig, epoch int64) error {
	selector := fmt.Sprintf("argus.io/release-id=%s,%s=%s", cfg.Spec.ReleaseID, pkiRoleLabel, pkiStagedServerRole)
	items, err := clients.dynamic.Resource(pkiCertificateGVR).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	for _, item := range items.Items {
		if item.GetAnnotations()[pkiEpochAnnotation] != fmt.Sprint(epoch) {
			continue
		}
		secretName, _, _ := unstructured.NestedString(item.Object, "spec", "secretName")
		if err = clients.dynamic.Resource(pkiCertificateGVR).Namespace(item.GetNamespace()).Delete(ctx, item.GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		if secretName != "" && secretName == item.GetName() {
			if err = clients.typed.CoreV1().Secrets(item.GetNamespace()).Delete(ctx, secretName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

func (a *App) cleanupRotationResources(ctx context.Context, clients *kubeClients, cfg *InstallConfig, epoch int64, keepArchive bool) error {
	if err := a.cleanupNextIssuerResources(ctx, clients, cfg, epoch); err != nil {
		return err
	}
	if err := cleanupStagedServerCertificates(ctx, clients, cfg, epoch); err != nil {
		return err
	}
	if cfg.Spec.PKI.Mode == PKIModeManaged {
		if err := clients.dynamic.Resource(pkiIssuerGVR).Delete(ctx, formerIssuerName(cfg, epoch), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		if !keepArchive {
			if err := clients.typed.CoreV1().Secrets("cert-manager").Delete(ctx, archivedRootSecretName(cfg, epoch), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

func (a *App) pkiExtend(ctx context.Context, cfg *InstallConfig, extension time.Duration) error {
	session, err := a.openPKISession(ctx, cfg)
	if err != nil {
		return err
	}
	defer session.Close()
	service := trustbundle.Service{Store: session.store}
	current, err := service.Current(ctx)
	if err != nil {
		return err
	}
	extended, err := service.Extend(ctx, current.Epoch, extension)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "PKI overlap for epoch %d extended to %s\n", extended.Epoch, extended.RetireAt.Format(time.RFC3339))
	return nil
}

func (a *App) pkiAbort(ctx context.Context, cfg *InstallConfig) error {
	session, err := a.openPKISession(ctx, cfg)
	if err != nil {
		return err
	}
	defer session.Close()
	service := trustbundle.Service{Store: session.store}
	current, err := service.Current(ctx)
	if err != nil {
		return err
	}
	if current.State != trustbundle.StatePreparing && current.State != trustbundle.StateOverlapping {
		return fmt.Errorf("PKI epoch %d is %s and has no active rotation to abort", current.Epoch, current.State)
	}
	if current.Direction == trustbundle.DirectionForward && current.State == trustbundle.StateOverlapping &&
		!current.RetireAt.IsZero() && time.Until(current.RetireAt) < 15*time.Minute {
		return errors.New("less than 15 minutes remain before CA retirement; run pki extend before pki abort")
	}
	formerFingerprints := current.CurrentCAFingerprints
	if current.Direction == trustbundle.DirectionRollback {
		formerFingerprints = current.NextCAFingerprints
	}
	former, err := current.Material.Select(formerFingerprints)
	if err != nil {
		return err
	}
	certificates, err := listArgusCertificates(ctx, session.clients, cfg.Spec.ReleaseID)
	if err != nil {
		return err
	}
	servers, _ := partitionCertificates(certificates)
	if len(servers) == 0 {
		return errors.New("PKI rollback requires server-only Certificate resources")
	}
	rollbackIssuer := cfg.globalIssuerName()
	if cfg.Spec.PKI.Mode == PKIModeExistingClusterIssuer {
		rollbackIssuer, err = formerIssuerForRotation(ctx, session.clients, cfg, current.Epoch, servers)
		if err != nil {
			return err
		}
		if err = waitForIssuerReady(ctx, session.clients, rollbackIssuer, 2*time.Minute); err != nil {
			return fmt.Errorf("former customer ClusterIssuer %s is not Ready: %w", rollbackIssuer, err)
		}
		if err = probeClusterIssuer(ctx, session.clients, cfg, rollbackIssuer, current.Epoch, former); err != nil {
			return fmt.Errorf("former customer ClusterIssuer %s does not issue from the rollback Bundle: %w", rollbackIssuer, err)
		}
	} else if _, getErr := session.clients.typed.CoreV1().Secrets("cert-manager").Get(ctx, archivedRootSecretName(cfg, current.Epoch), metav1.GetOptions{}); getErr != nil && !apierrors.IsNotFound(getErr) {
		return getErr
	} else if apierrors.IsNotFound(getErr) {
		if err = probeClusterIssuer(ctx, session.clients, cfg, rollbackIssuer, current.Epoch, former); err != nil {
			return fmt.Errorf("managed steady ClusterIssuer does not match the former Bundle: %w", err)
		}
	}

	overlap, _ := time.ParseDuration(cfg.Spec.PKI.Rotation.Overlap)
	if current.Direction != trustbundle.DirectionRollback {
		current, err = service.ReverseOverlap(ctx, current.Epoch, overlap)
		if err != nil {
			return err
		}
	}

	if cfg.Spec.PKI.Mode == PKIModeManaged {
		if _, getErr := session.clients.typed.CoreV1().Secrets("cert-manager").Get(ctx, archivedRootSecretName(cfg, current.Epoch), metav1.GetOptions{}); getErr == nil {
			if err = restoreManagedSteadyRoot(ctx, session.clients, cfg, current.Epoch, former); err != nil {
				return err
			}
		} else if !apierrors.IsNotFound(getErr) {
			return getErr
		} else if err = switchCertificates(ctx, session.clients, certificates, rollbackIssuer, former, 5*time.Minute); err != nil {
			return fmt.Errorf("restore managed leaves before rollback: %w", err)
		}
	} else if err = switchCertificates(ctx, session.clients, certificates, rollbackIssuer, former, 5*time.Minute); err != nil {
		return fmt.Errorf("restore customer-issued leaves before rollback: %w", err)
	}
	if err = cleanupStagedServerCertificates(ctx, session.clients, cfg, current.Epoch); err != nil {
		return err
	}
	certificates, err = listArgusCertificates(ctx, session.clients, cfg.Spec.ReleaseID)
	if err != nil {
		return err
	}
	servers, _ = partitionCertificates(certificates)
	if _, err = stageServerCertificates(ctx, session.clients, cfg, servers, rollbackIssuer, rollbackIssuer, "rollback", current.Epoch, former, 5*time.Minute); err != nil {
		return fmt.Errorf("pre-issue rollback server leaves: %w", err)
	}
	if err = ensureTargetIssuerReaderRBAC(ctx, session.clients, cfg, rollbackIssuer); err != nil {
		return err
	}
	previousGeneration := current.Epoch - 1
	if previousGeneration < 1 {
		previousGeneration = 1
	}
	if err = switchRuntimeIssuers(ctx, session.clients, cfg, rollbackIssuer, previousGeneration, 5*time.Minute); err != nil {
		return fmt.Errorf("restore runtime identity issuance for safe rollback: %w", err)
	}
	if err = a.cleanupNextIssuerResources(ctx, session.clients, cfg, current.Epoch); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "PKI rotation epoch %d reversed safely; former CA will become stable after rollback overlap at %s\n", current.Epoch, current.RetireAt.Format(time.RFC3339))
	return nil
}

func formerIssuerForRotation(ctx context.Context, clients *kubeClients, cfg *InstallConfig, epoch int64, servers []argusCertificate) (string, error) {
	candidates := map[string]struct{}{}
	selector := fmt.Sprintf("argus.io/release-id=%s,%s=%s", cfg.Spec.ReleaseID, pkiRoleLabel, pkiStagedServerRole)
	staged, err := clients.dynamic.Resource(pkiCertificateGVR).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return "", err
	}
	for _, certificate := range staged.Items {
		if certificate.GetAnnotations()[pkiEpochAnnotation] == fmt.Sprint(epoch) {
			if name := certificate.GetAnnotations()[pkiFormerIssuer]; name != "" {
				candidates[name] = struct{}{}
			}
		}
	}
	if len(candidates) == 0 {
		for _, certificate := range servers {
			if certificate.IssuerName != "" {
				candidates[certificate.IssuerName] = struct{}{}
			}
		}
	}
	if len(candidates) != 1 {
		return "", fmt.Errorf("cannot determine one former customer ClusterIssuer for epoch %d: %v", epoch, candidates)
	}
	for name := range candidates {
		return name, nil
	}
	return "", errors.New("former customer ClusterIssuer is unavailable")
}

func restoreManagedSteadyRoot(ctx context.Context, clients *kubeClients, cfg *InstallConfig, epoch int64, former trustbundle.Material) error {
	secrets := clients.typed.CoreV1().Secrets("cert-manager")
	archive, err := secrets.Get(ctx, archivedRootSecretName(cfg, epoch), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read archived managed root for rollback: %w", err)
	}
	if err = validateManagedRootCA(archive, cfg.Spec.ReleaseID); err != nil {
		return err
	}
	rollbackIssuer := formerIssuerName(cfg, epoch)
	_ = clients.dynamic.Resource(pkiIssuerGVR).Delete(ctx, rollbackIssuer, metav1.DeleteOptions{})
	issuer := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cert-manager.io/v1", "kind": "ClusterIssuer",
		"metadata": map[string]any{"name": rollbackIssuer,
			"labels":      map[string]any{"app.kubernetes.io/part-of": "argus", "argus.io/release-id": cfg.Spec.ReleaseID, "argus.io/pki-role": "rotation-issuer"},
			"annotations": map[string]any{"argus.io/pki-epoch": fmt.Sprintf("%d", epoch)}},
		"spec": map[string]any{"ca": map[string]any{"secretName": archive.Name}},
	}}
	if _, err = clients.dynamic.Resource(pkiIssuerGVR).Create(ctx, issuer, metav1.CreateOptions{}); err != nil {
		return err
	}
	if err = waitForIssuerReady(ctx, clients, rollbackIssuer, 2*time.Minute); err != nil {
		return err
	}
	if err = probeClusterIssuer(ctx, clients, cfg, rollbackIssuer, epoch, former); err != nil {
		return err
	}
	certificates, err := listArgusCertificates(ctx, clients, cfg.Spec.ReleaseID)
	if err != nil {
		return err
	}
	if err = switchCertificates(ctx, clients, certificates, rollbackIssuer, former, 5*time.Minute); err != nil {
		return err
	}
	baseName := cfg.Spec.ReleaseID + "-root-ca"
	current, err := secrets.Get(ctx, baseName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	replacement := archive.DeepCopy()
	replacement.ObjectMeta = metav1.ObjectMeta{Name: baseName, Labels: copyStringMap(archive.Labels), Annotations: copyStringMap(archive.Annotations)}
	replacement.Labels["argus.io/pki-phase"] = "steady"
	if err = secrets.Delete(ctx, baseName, metav1.DeleteOptions{}); err != nil {
		return err
	}
	if _, err = secrets.Create(ctx, replacement, metav1.CreateOptions{}); err != nil {
		restore := current.DeepCopy()
		restore.ObjectMeta = metav1.ObjectMeta{Name: baseName, Labels: copyStringMap(current.Labels), Annotations: copyStringMap(current.Annotations)}
		_, restoreErr := secrets.Create(ctx, restore, metav1.CreateOptions{})
		return fmt.Errorf("restore former managed root: %w (new-root restore result: %v)", err, restoreErr)
	}
	if err = waitForIssuerReady(ctx, clients, cfg.globalIssuerName(), 2*time.Minute); err != nil {
		return err
	}
	return switchCertificates(ctx, clients, certificates, cfg.globalIssuerName(), former, 5*time.Minute)
}

// Keep generated DB types referenced in this file's status contract explicit;
// this catches sqlc shape changes during compilation.
var _ = db.PkiTrustBundle{}
