// Package pkirotation performs the deadline-driven, fail-closed end of a root
// CA overlap. Root creation and leaf cutover remain explicit argusctl actions.
package pkirotation

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"

	"github.com/jackc/pgx/v5"

	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/trustbundle"
)

var (
	certificateGVR = schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificates"}
	issuerGVR      = schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "clusterissuers"}
)

const (
	pkiRoleLabel         = "argus.io/pki-role"
	pkiEpochAnnotation   = "argus.io/pki-epoch"
	pkiSourceCertificate = "argus.io/pki-source-certificate"
	pkiSourceSecret      = "argus.io/pki-source-secret"
	pkiTargetIssuer      = "argus.io/pki-target-issuer"
	pkiDirection         = "argus.io/pki-direction"
	pkiStagedServerRole  = "staged-server"
)

type Config struct {
	ReleaseID       string
	Mode            string
	TrustSourceName string
	TrustBundleName string
	Namespaces      []string
	Interval        time.Duration
}

type Reconciler struct {
	Store   *postgres.Store
	Typed   kubernetes.Interface
	Dynamic dynamic.Interface
	Config  Config
	Logger  *slog.Logger
}

func (reconciler Reconciler) Run(ctx context.Context) error {
	if reconciler.Store == nil || reconciler.Typed == nil || reconciler.Dynamic == nil || reconciler.Config.ReleaseID == "" {
		return errors.New("PKI rotation reconciler is not configured")
	}
	interval := reconciler.Config.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := reconciler.Reconcile(ctx, time.Now().UTC()); err != nil && !errors.Is(err, context.Canceled) {
			reconciler.logger().Error("PKI rotation reconciliation failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (reconciler Reconciler) Reconcile(ctx context.Context, now time.Time) error {
	service := trustbundle.Service{Store: reconciler.Store}
	current, err := service.Current(ctx)
	if err != nil {
		return err
	}
	if err = reconciler.syncServiceClientIdentities(ctx, current); err != nil {
		return fmt.Errorf("synchronize service client identities: %w", err)
	}
	if current.State != trustbundle.StateOverlapping || current.RetireAt.IsZero() || now.Before(current.RetireAt) {
		return reconciler.cleanupCompletedArchives(ctx)
	}
	next, err := current.Material.Select(current.NextCAFingerprints)
	if err != nil {
		return err
	}
	staged, err := reconciler.loadStagedServers(ctx, current.Epoch, current.Direction, next)
	if err != nil {
		return fmt.Errorf("refuse CA retirement without complete staged server leaves: %w", err)
	}
	if err = reconciler.promoteStagedServers(ctx, staged, next, 2*time.Minute); err != nil {
		return fmt.Errorf("switch serving leaves to the next CA: %w", err)
	}
	if err = reconciler.verifyAllLeaves(ctx, next); err != nil {
		return fmt.Errorf("refuse CA retirement while an Argus leaf does not chain to the next CA: %w", err)
	}
	if err = reconciler.publishTrustSource(ctx, next, current.Epoch+1); err != nil {
		return err
	}
	if err = reconciler.waitForTargets(ctx, next, 2*time.Minute); err != nil {
		return err
	}
	activeControlPlaneIDs, err := reconciler.activeControlPlaneNodeIDs(ctx)
	if err != nil {
		return fmt.Errorf("list active control-plane Bundle acknowledgers: %w", err)
	}
	stable, err := service.CompleteRetirement(ctx, current.Epoch, activeControlPlaneIDs...)
	if err != nil {
		return err
	}
	if err = reconciler.cleanupRotationResources(ctx, current.Epoch); err != nil {
		return err
	}
	reconciler.logger().Info("retired former Argus CA", "overlap_epoch", current.Epoch, "stable_epoch", stable.Epoch, "bundle_sha256", stable.Material.SHA256)
	return nil
}

func (reconciler Reconciler) syncServiceClientIdentities(ctx context.Context, bundle trustbundle.Bundle) error {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(bundle.Material.PEM) {
		return errors.New("current Trust Bundle has no usable CA certificates")
	}
	selector := "argus.io/release-id=" + reconciler.Config.ReleaseID
	for _, namespace := range reconciler.Config.Namespaces {
		certificates, err := reconciler.Dynamic.Resource(certificateGVR).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return err
		}
		for _, resource := range certificates.Items {
			usages, _, _ := unstructured.NestedStringSlice(resource.Object, "spec", "usages")
			if len(usages) != 1 || usages[0] != "client auth" {
				continue
			}
			uris, _, _ := unstructured.NestedStringSlice(resource.Object, "spec", "uris")
			secretName, _, _ := unstructured.NestedString(resource.Object, "spec", "secretName")
			if len(uris) != 1 || !strings.HasPrefix(uris[0], "spiffe://argus.io/services/") || secretName == "" {
				return fmt.Errorf("service client Certificate %s/%s has an invalid URI or Secret", namespace, resource.GetName())
			}
			secret, err := reconciler.Typed.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			block, _ := pem.Decode(secret.Data[corev1.TLSCertKey])
			if block == nil || block.Type != "CERTIFICATE" {
				return fmt.Errorf("service client Secret %s/%s has no certificate", namespace, secretName)
			}
			certificate, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return err
			}
			identity, err := trustbundle.ServiceCertificateIdentity(certificate, uris)
			if err != nil || certificate.URIs[0].String() != uris[0] {
				return fmt.Errorf("service client Certificate %s/%s identity does not match its specification", namespace, resource.GetName())
			}
			if _, err = certificate.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
				return fmt.Errorf("service client Certificate %s/%s does not chain to the current Bundle: %w", namespace, resource.GetName(), err)
			}
			record, getErr := reconciler.Store.Queries.GetActivePKICertificateIdentity(ctx, trustbundle.CertificateSerial(certificate))
			if getErr == nil {
				if err = trustbundle.VerifyCertificateIdentity(record, certificate, identity); err != nil {
					return err
				}
				continue
			}
			if !errors.Is(getErr, pgx.ErrNoRows) {
				return getErr
			}
			if err = reconciler.Store.InTx(ctx, func(queries *db.Queries) error {
				if lockErr := queries.LockPKICertificateSubject(ctx, db.LockPKICertificateSubjectParams{
					SubjectKind: "service", SubjectID: identity.SubjectID,
				}); lockErr != nil {
					return lockErr
				}
				record, lockedGetErr := queries.GetActivePKICertificateIdentity(ctx, trustbundle.CertificateSerial(certificate))
				if lockedGetErr == nil {
					return trustbundle.VerifyCertificateIdentity(record, certificate, identity)
				}
				if !errors.Is(lockedGetErr, pgx.ErrNoRows) {
					return lockedGetErr
				}
				if markErr := queries.MarkPKISubjectCertificatesOverlap(ctx, db.MarkPKISubjectCertificatesOverlapParams{
					SubjectKind: "service", SubjectID: identity.SubjectID,
				}); markErr != nil {
					return markErr
				}
				identity.IssuerGeneration = int32(bundle.Epoch)
				return trustbundle.RegisterCertificateIdentity(ctx, queries, certificate, identity)
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

type stagedServer struct {
	Namespace         string
	CertificateName   string
	SecretName        string
	SourceCertificate string
	SourceSecret      string
	TargetIssuer      string
}

func (reconciler Reconciler) loadStagedServers(ctx context.Context, epoch int64, direction string, material trustbundle.Material) ([]stagedServer, error) {
	result := make([]stagedServer, 0)
	seenSources := map[string]struct{}{}
	targetIssuer := ""
	selector := fmt.Sprintf("argus.io/release-id=%s,%s=%s", reconciler.Config.ReleaseID, pkiRoleLabel, pkiStagedServerRole)
	for _, namespace := range reconciler.Config.Namespaces {
		list, err := reconciler.Dynamic.Resource(certificateGVR).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return nil, err
		}
		for _, item := range list.Items {
			annotations := item.GetAnnotations()
			if annotations[pkiEpochAnnotation] != fmt.Sprint(epoch) {
				continue
			}
			if annotations[pkiDirection] != direction {
				return nil, fmt.Errorf("staged Certificate %s/%s direction %q does not match Bundle direction %q", namespace, item.GetName(), annotations[pkiDirection], direction)
			}
			stage := stagedServer{Namespace: namespace, CertificateName: item.GetName(), SourceCertificate: annotations[pkiSourceCertificate],
				SourceSecret: annotations[pkiSourceSecret], TargetIssuer: annotations[pkiTargetIssuer]}
			stage.SecretName, _, _ = unstructured.NestedString(item.Object, "spec", "secretName")
			if stage.CertificateName == "" || stage.SecretName != stage.CertificateName || stage.SourceCertificate == "" || stage.SourceSecret == "" || stage.TargetIssuer == "" {
				return nil, fmt.Errorf("staged Certificate %s/%s has incomplete cutover metadata", namespace, item.GetName())
			}
			key := namespace + "/" + stage.SourceCertificate
			if _, duplicate := seenSources[key]; duplicate {
				return nil, fmt.Errorf("multiple staged leaves target Certificate %s", key)
			}
			seenSources[key] = struct{}{}
			if targetIssuer == "" {
				targetIssuer = stage.TargetIssuer
			} else if targetIssuer != stage.TargetIssuer {
				return nil, errors.New("staged server leaves disagree on the target ClusterIssuer")
			}
			source, getErr := reconciler.Dynamic.Resource(certificateGVR).Namespace(namespace).Get(ctx, stage.SourceCertificate, metav1.GetOptions{})
			if getErr != nil || source.GetLabels()["argus.io/release-id"] != reconciler.Config.ReleaseID {
				return nil, fmt.Errorf("source Certificate %s is missing or not owned by this release", key)
			}
			usage, _, _ := unstructured.NestedStringSlice(source.Object, "spec", "usages")
			secretName, _, _ := unstructured.NestedString(source.Object, "spec", "secretName")
			if len(usage) != 1 || usage[0] != "server auth" || secretName != stage.SourceSecret {
				return nil, fmt.Errorf("source Certificate %s is not the expected server-only leaf", key)
			}
			secret, getErr := reconciler.Typed.CoreV1().Secrets(namespace).Get(ctx, stage.SecretName, metav1.GetOptions{})
			if getErr != nil {
				return nil, getErr
			}
			if verifyErr := verifyLeaf(secret, material, "server auth"); verifyErr != nil {
				return nil, fmt.Errorf("staged Certificate %s/%s: %w", namespace, stage.CertificateName, verifyErr)
			}
			result = append(result, stage)
		}
	}
	if len(result) == 0 {
		return nil, errors.New("no staged server leaves were found")
	}
	return result, nil
}

func (reconciler Reconciler) promoteStagedServers(ctx context.Context, staged []stagedServer, material trustbundle.Material, timeout time.Duration) error {
	issuer, err := reconciler.Dynamic.Resource(issuerGVR).Get(ctx, staged[0].TargetIssuer, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read target ClusterIssuer %s: %w", staged[0].TargetIssuer, err)
	}
	if !issuerReady(issuer) {
		return fmt.Errorf("target ClusterIssuer %s is not Ready", staged[0].TargetIssuer)
	}
	for _, stage := range staged {
		resource := reconciler.Dynamic.Resource(certificateGVR).Namespace(stage.Namespace)
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			source, getErr := resource.Get(ctx, stage.SourceCertificate, metav1.GetOptions{})
			if getErr != nil {
				return getErr
			}
			if setErr := unstructured.SetNestedField(source.Object, stage.TargetIssuer, "spec", "issuerRef", "name"); setErr != nil {
				return setErr
			}
			if setErr := unstructured.SetNestedField(source.Object, "ClusterIssuer", "spec", "issuerRef", "kind"); setErr != nil {
				return setErr
			}
			if setErr := unstructured.SetNestedField(source.Object, "cert-manager.io", "spec", "issuerRef", "group"); setErr != nil {
				return setErr
			}
			_, updateErr := resource.Update(ctx, source, metav1.UpdateOptions{})
			return updateErr
		}); err != nil {
			return fmt.Errorf("set target issuer on Certificate %s/%s: %w", stage.Namespace, stage.SourceCertificate, err)
		}

		stagedSecret, err := reconciler.Typed.CoreV1().Secrets(stage.Namespace).Get(ctx, stage.SecretName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if len(stagedSecret.Data[corev1.TLSCertKey]) == 0 || len(stagedSecret.Data[corev1.TLSPrivateKeyKey]) == 0 {
			return fmt.Errorf("staged Secret %s/%s lacks a TLS key pair", stage.Namespace, stage.SecretName)
		}
		secrets := reconciler.Typed.CoreV1().Secrets(stage.Namespace)
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			live, getErr := secrets.Get(ctx, stage.SourceSecret, metav1.GetOptions{})
			if getErr != nil {
				return getErr
			}
			copy := live.DeepCopy()
			if copy.Data == nil {
				copy.Data = map[string][]byte{}
			}
			for _, key := range []string{corev1.TLSCertKey, corev1.TLSPrivateKeyKey, "ca.crt"} {
				if value := stagedSecret.Data[key]; len(value) != 0 {
					copy.Data[key] = append([]byte(nil), value...)
				}
			}
			_, updateErr := secrets.Update(ctx, copy, metav1.UpdateOptions{})
			return updateErr
		}); err != nil {
			return fmt.Errorf("atomically publish staged server key pair to %s/%s: %w", stage.Namespace, stage.SourceSecret, err)
		}
	}

	deadline := time.Now().Add(timeout)
	for _, stage := range staged {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errors.New("timed out verifying promoted server leaves")
		}
		if err := wait.PollUntilContextTimeout(ctx, time.Second, remaining, true, func(ctx context.Context) (bool, error) {
			secret, err := reconciler.Typed.CoreV1().Secrets(stage.Namespace).Get(ctx, stage.SourceSecret, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			return verifyLeaf(secret, material, "server auth") == nil, nil
		}); err != nil {
			return fmt.Errorf("verify promoted server Secret %s/%s: %w", stage.Namespace, stage.SourceSecret, err)
		}
	}
	return nil
}

func (reconciler Reconciler) activeControlPlaneNodeIDs(ctx context.Context) ([]string, error) {
	ids := make([]string, 0, 8)
	for _, namespace := range reconciler.Config.Namespaces {
		pods, err := reconciler.Typed.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for index := range pods.Items {
			if id, ok := controlPlaneNodeID(&pods.Items[index]); ok {
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

func controlPlaneNodeID(pod *corev1.Pod) (string, bool) {
	if pod == nil || pod.Name == "" || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
		return "", false
	}
	ready := false
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			ready = condition.Status == corev1.ConditionTrue
			break
		}
	}
	if !ready {
		return "", false
	}
	component := ""
	switch appName := pod.Labels["app.kubernetes.io/name"]; appName {
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
			component = "worker-" + workerPool(pod)
		}
	}
	if component == "" {
		return "", false
	}
	return component + "/" + pod.Name, true
}

func workerPool(pod *corev1.Pod) string {
	for _, container := range pod.Spec.Containers {
		for index, argument := range container.Args {
			if strings.HasPrefix(argument, "--pool=") {
				if value := strings.TrimSpace(strings.TrimPrefix(argument, "--pool=")); value != "" {
					return value
				}
			}
			if argument == "--pool" && index+1 < len(container.Args) {
				if value := strings.TrimSpace(container.Args[index+1]); value != "" {
					return value
				}
			}
		}
	}
	return "default"
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

func (reconciler Reconciler) verifyAllLeaves(ctx context.Context, material trustbundle.Material) error {
	count := 0
	for _, namespace := range reconciler.Config.Namespaces {
		list, err := reconciler.Dynamic.Resource(certificateGVR).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: "argus.io/release-id=" + reconciler.Config.ReleaseID})
		if err != nil {
			return fmt.Errorf("list Certificates in %s: %w", namespace, err)
		}
		for _, certificate := range list.Items {
			if certificate.GetLabels()[pkiRoleLabel] == pkiStagedServerRole || certificate.GetLabels()[pkiRoleLabel] == "issuer-probe" {
				continue
			}
			secretName, found, _ := unstructured.NestedString(certificate.Object, "spec", "secretName")
			usages, _, _ := unstructured.NestedStringSlice(certificate.Object, "spec", "usages")
			if !found || len(usages) != 1 {
				return fmt.Errorf("Certificate %s/%s has no single-use leaf policy", namespace, certificate.GetName())
			}
			secret, getErr := reconciler.Typed.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
			if getErr != nil {
				return getErr
			}
			if verifyErr := verifyLeaf(secret, material, usages[0]); verifyErr != nil {
				return fmt.Errorf("Certificate %s/%s: %w", namespace, certificate.GetName(), verifyErr)
			}
			count++
		}
	}
	if count == 0 {
		return errors.New("no managed Argus leaves were found")
	}
	return nil
}

func verifyLeaf(secret *corev1.Secret, material trustbundle.Material, usage string) error {
	pair, err := tls.X509KeyPair(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
	if err != nil || len(pair.Certificate) == 0 {
		return errors.New("invalid leaf key pair")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return err
	}
	expected := x509.ExtKeyUsageServerAuth
	if usage == "client auth" {
		expected = x509.ExtKeyUsageClientAuth
	} else if usage != "server auth" {
		return fmt.Errorf("unsupported usage %q", usage)
	}
	if len(leaf.ExtKeyUsage) != 1 || leaf.ExtKeyUsage[0] != expected || len(leaf.UnknownExtKeyUsage) != 0 {
		return fmt.Errorf("leaf does not have exact %s EKU", usage)
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
	_, err = leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{expected}, CurrentTime: time.Now().UTC()})
	return err
}

func (reconciler Reconciler) publishTrustSource(ctx context.Context, material trustbundle.Material, epoch int64) error {
	configMaps := reconciler.Typed.CoreV1().ConfigMaps("cert-manager")
	current, err := configMaps.Get(ctx, reconciler.Config.TrustSourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read PKI trust source: %w", err)
	}
	if current.Labels["argus.io/release-id"] != reconciler.Config.ReleaseID {
		return errors.New("refusing to update a Trust source owned by another release")
	}
	if current.Data["ca.crt"] == string(material.PEM) && strings.EqualFold(current.Annotations["argus.io/trust-bundle-sha256"], material.SHA256) &&
		current.Annotations["argus.io/trust-bundle-epoch"] == fmt.Sprint(epoch) {
		return nil
	}
	copy := current.DeepCopy()
	if copy.Annotations == nil {
		copy.Annotations = map[string]string{}
	}
	copy.Annotations["argus.io/trust-bundle-sha256"] = material.SHA256
	copy.Annotations["argus.io/trust-bundle-epoch"] = fmt.Sprint(epoch)
	copy.Data = map[string]string{"ca.crt": string(material.PEM)}
	if _, err = configMaps.Update(ctx, copy, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("publish retired Trust Bundle source: %w", err)
	}
	return nil
}

func (reconciler Reconciler) waitForTargets(ctx context.Context, material trustbundle.Material, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		for _, namespace := range reconciler.Config.Namespaces {
			bundle, err := reconciler.Typed.CoreV1().ConfigMaps(namespace).Get(ctx, reconciler.Config.TrustBundleName, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			parsed, parseErr := trustbundle.Parse([]byte(bundle.Data["ca.crt"]), time.Now().UTC())
			if parseErr != nil || parsed.SHA256 != material.SHA256 {
				return false, nil
			}
		}
		return true, nil
	})
}

func (reconciler Reconciler) cleanupRotationResources(ctx context.Context, epoch int64) error {
	selector := fmt.Sprintf("argus.io/release-id=%s,%s=%s", reconciler.Config.ReleaseID, pkiRoleLabel, pkiStagedServerRole)
	for _, namespace := range reconciler.Config.Namespaces {
		list, err := reconciler.Dynamic.Resource(certificateGVR).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return err
		}
		for _, item := range list.Items {
			if item.GetAnnotations()[pkiEpochAnnotation] != fmt.Sprint(epoch) {
				continue
			}
			secretName, _, _ := unstructured.NestedString(item.Object, "spec", "secretName")
			if err = reconciler.Dynamic.Resource(certificateGVR).Namespace(namespace).Delete(ctx, item.GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			if secretName != "" && secretName == item.GetName() {
				if err = reconciler.Typed.CoreV1().Secrets(namespace).Delete(ctx, secretName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
					return err
				}
			}
		}
	}
	if reconciler.Config.Mode == "managed" {
		issuerName := fmt.Sprintf("%s-ca-former-%d", reconciler.Config.ReleaseID, epoch)
		if err := reconciler.Dynamic.Resource(issuerGVR).Delete(ctx, issuerName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete former managed ClusterIssuer %s: %w", issuerName, err)
		}
		rootName := fmt.Sprintf("%s-root-ca-previous-%d", reconciler.Config.ReleaseID, epoch)
		if err := reconciler.Typed.CoreV1().Secrets("cert-manager").Delete(ctx, rootName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete retired managed root %s: %w", rootName, err)
		}
	}
	return nil
}

func (reconciler Reconciler) cleanupCompletedArchives(ctx context.Context) error {
	records, err := reconciler.Store.Queries.ListTrustBundles(ctx, 4)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.State != trustbundle.StateRetiring {
			continue
		}
		if err = reconciler.cleanupRotationResources(ctx, record.Epoch); err != nil {
			return err
		}
	}
	return nil
}

func (reconciler Reconciler) logger() *slog.Logger {
	if reconciler.Logger == nil {
		return slog.Default()
	}
	return reconciler.Logger
}
