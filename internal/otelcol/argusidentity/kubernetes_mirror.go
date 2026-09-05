package argusidentity

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	kubernetesMirrorEnv      = "ARGUS_TELEMETRY_KUBERNETES_MIRROR"
	kubernetesNamespaceEnv   = "ARGUS_TELEMETRY_KUBERNETES_NAMESPACE"
	kubernetesIdentitySecret = "argus-otelcol-identity"
	kubernetesConfigMap      = "argus-otelcol-config"
	serviceAccountDirectory  = "/var/run/secrets/kubernetes.io/serviceaccount"
)

type kubernetesObject struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   kubernetesMetadata `json:"metadata"`
	Data       map[string]string  `json:"data"`
	Type       string             `json:"type,omitempty"`
}

type kubernetesMetadata struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	ResourceVersion string            `json:"resourceVersion"`
	Labels          map[string]string `json:"labels,omitempty"`
	Annotations     map[string]string `json:"annotations,omitempty"`
}

// mirrorKubernetesIdentity persists the Gateway's last validated rotating
// identity back to the fixed bootstrap Secret. Agent replicas deliberately do
// not mirror, preventing competing writers from causing Secret churn.
func mirrorKubernetesIdentity(ctx context.Context, config Config) error {
	if os.Getenv(kubernetesMirrorEnv) != "1" {
		return nil
	}
	namespace := os.Getenv(kubernetesNamespaceEnv)
	if !validKubernetesName(namespace) {
		return errors.New("telemetry Kubernetes identity namespace is invalid")
	}
	values := make(map[string][]byte, 8)
	for name, path := range map[string]string{
		"client.pem": config.CertificateFile, "client-key.pem": config.PrivateKeyFile,
		"server.pem": config.ServerCertificateFile, "server-key.pem": config.ServerPrivateKeyFile,
		"ca.pem": config.CABundleFile, "trust-bundle.json": config.TrustBundleStateFile,
	} {
		value, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read Collector identity mirror file %s: %w", filepath.Base(path), err)
		}
		if len(value) == 0 {
			return fmt.Errorf("Collector identity mirror file %s is empty", filepath.Base(path))
		}
		values[name] = value
	}
	state, err := loadTrustBundleState(config.TrustBundleStateFile, config.CABundleFile)
	if err != nil {
		return err
	}
	values["trust-bundle-epoch"] = []byte(strconv.FormatInt(state.Epoch, 10))
	values["trust-bundle-sha256"] = []byte(state.BundleSHA256)
	client, token, baseURL, err := inClusterHTTPClient()
	if err != nil {
		return err
	}
	operationCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	secretPath := baseURL + "/api/v1/namespaces/" + namespace + "/secrets/" + kubernetesIdentitySecret
	err = updateKubernetesObject(operationCtx, client, token, secretPath, func(object *kubernetesObject) {
		if object.Data == nil {
			object.Data = map[string]string{}
		}
		for name, value := range values {
			object.Data[name] = base64.StdEncoding.EncodeToString(value)
		}
		delete(object.Data, "enrollment-token")
	})
	if err != nil {
		return fmt.Errorf("mirror Collector identity Secret: %w", err)
	}
	configPath := baseURL + "/api/v1/namespaces/" + namespace + "/configmaps/" + kubernetesConfigMap
	err = updateKubernetesObject(operationCtx, client, token, configPath, func(object *kubernetesObject) {
		if object.Data == nil {
			object.Data = map[string]string{}
		}
		object.Data["server-ca.pem"] = string(values["ca.pem"])
		object.Data["trust-bundle-epoch"] = strconv.FormatInt(state.Epoch, 10)
		object.Data["trust-bundle-sha256"] = state.BundleSHA256
	})
	if err != nil {
		return fmt.Errorf("mirror Collector Trust Bundle ConfigMap: %w", err)
	}
	return nil
}

func inClusterHTTPClient() (*http.Client, string, string, error) {
	token, err := os.ReadFile(filepath.Join(serviceAccountDirectory, "token"))
	if err != nil || strings.TrimSpace(string(token)) == "" {
		return nil, "", "", errors.New("telemetry Kubernetes identity mirror token is unavailable")
	}
	caPEM, err := os.ReadFile(filepath.Join(serviceAccountDirectory, "ca.crt"))
	if err != nil {
		return nil, "", "", errors.New("telemetry Kubernetes API CA is unavailable")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, "", "", errors.New("telemetry Kubernetes API CA is invalid")
	}
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT_HTTPS")
	if port == "" {
		port = "443"
	}
	if host == "" {
		return nil, "", "", errors.New("telemetry Kubernetes API endpoint is unavailable")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}}
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("Kubernetes API redirects are forbidden")
		}}
	return client, strings.TrimSpace(string(token)), "https://" + net.JoinHostPort(host, port), nil
}

func updateKubernetesObject(ctx context.Context, client *http.Client, token, endpoint string, mutate func(*kubernetesObject)) error {
	for attempt := 0; attempt < 5; attempt++ {
		object, err := getKubernetesObject(ctx, client, token, endpoint)
		if err != nil {
			return err
		}
		mutate(&object)
		encoded, err := json.Marshal(object)
		if err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(encoded))
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return nil
		}
		if response.StatusCode != http.StatusConflict {
			return fmt.Errorf("Kubernetes API update returned %d", response.StatusCode)
		}
	}
	return errors.New("Kubernetes API update remained conflicted")
}

func getKubernetesObject(ctx context.Context, client *http.Client, token, endpoint string) (kubernetesObject, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return kubernetesObject{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		return kubernetesObject{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return kubernetesObject{}, fmt.Errorf("Kubernetes API read returned %d", response.StatusCode)
	}
	var object kubernetesObject
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err = decoder.Decode(&object); err != nil || object.Metadata.ResourceVersion == "" {
		return kubernetesObject{}, errors.New("Kubernetes API object is invalid")
	}
	return object, nil
}

func validKubernetesName(value string) bool {
	if len(value) < 1 || len(value) > 63 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	last := value[len(value)-1]
	return (last >= 'a' && last <= 'z') || (last >= '0' && last <= '9')
}
