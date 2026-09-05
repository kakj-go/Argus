package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
)

const (
	kubernetesIdentitySecret = "argus-connector-identity"
	kubernetesTrustConfigMap = "argus-trust-bundle"
)

type kubernetesStateMirror struct {
	client    kubernetes.Interface
	namespace string
}

func newKubernetesStateMirror(namespace string) (kubernetesStateMirror, error) {
	if issues := validation.IsDNS1123Label(namespace); len(issues) != 0 {
		return kubernetesStateMirror{}, errors.New("ARGUS_CONNECTOR_KUBERNETES_NAMESPACE is invalid")
	}
	configuration, err := rest.InClusterConfig()
	if err != nil {
		return kubernetesStateMirror{}, fmt.Errorf("load in-cluster configuration: %w", err)
	}
	client, err := kubernetes.NewForConfig(configuration)
	if err != nil {
		return kubernetesStateMirror{}, err
	}
	return kubernetesStateMirror{client: client, namespace: namespace}, nil
}

func (mirror kubernetesStateMirror) sync(store localStore) error {
	if mirror.client == nil || mirror.namespace == "" {
		return errors.New("Kubernetes Connector state mirror is not configured")
	}
	files := map[string][]byte{}
	for _, name := range []string{identityFile, keyFile, certFile, caFile} {
		value, err := os.ReadFile(filepath.Join(store.directory, name))
		if err != nil || len(value) == 0 {
			return fmt.Errorf("read Connector identity %s: %w", name, err)
		}
		files[name] = value
	}
	var state identityState
	if err := json.Unmarshal(files[identityFile], &state); err != nil || state.TrustBundleEpoch < 1 || len(state.TrustBundleSHA256) != 64 {
		return errors.New("Connector identity state cannot be mirrored")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	annotations := map[string]string{"argus.io/trust-bundle-epoch": fmt.Sprint(state.TrustBundleEpoch),
		"argus.io/trust-bundle-sha256": state.TrustBundleSHA256}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		configMap, err := mirror.client.CoreV1().ConfigMaps(mirror.namespace).Get(ctx, kubernetesTrustConfigMap, metav1.GetOptions{})
		if err != nil {
			return err
		}
		configMap.Data = map[string]string{"ca.crt": string(files[caFile])}
		configMap.Annotations = annotations
		_, err = mirror.client.CoreV1().ConfigMaps(mirror.namespace).Update(ctx, configMap, metav1.UpdateOptions{})
		return err
	}); err != nil {
		return err
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		secret, err := mirror.client.CoreV1().Secrets(mirror.namespace).Get(ctx, kubernetesIdentitySecret, metav1.GetOptions{})
		if err != nil {
			return err
		}
		secret.Data = files
		secret.Annotations = annotations
		_, err = mirror.client.CoreV1().Secrets(mirror.namespace).Update(ctx, secret, metav1.UpdateOptions{})
		return err
	})
}
