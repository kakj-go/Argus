package argusdev

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// E2EEndpoints captures the domain-based access paths of an installed release.
// Browser traffic goes through the ingress (TLS terminated with the
// cert-manager issued certificate); the connector mTLS channel goes through
// the dedicated LoadBalancer service. Host processes reach both without
// /etc/hosts by overriding dial addresses while keeping TLS ServerName on the
// public hostname.
type E2EEndpoints struct {
	EnterpriseOrigin string
	PlatformOrigin   string
	CardOrigin       string
	RemoteOrigin     string
	APIBase          string
	ConnectorGateway string
	EnrollServer     string
	IngressIP        string
	ConnectorIP      string
	// 子进程拨号覆盖用的地址(优先宿主机 localhost 发布端口)。
	IngressDialAddress   string
	ConnectorDialAddress string
	HostResolver         string
	CAFile               string
	RootCAs              *x509.CertPool
	hostIPs              map[string]string
}

// Transport returns a shared round tripper that pins the public hostnames to
// the load-balancer addresses while TLS ServerName stays on the hostname.
func (e *E2EEndpoints) Transport() *http.Transport {
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: 10 * time.Second}
			if host, port, err := net.SplitHostPort(addr); err == nil {
				if ip, mapped := e.hostIPs[host]; mapped {
					addr = net.JoinHostPort(ip, port)
				}
			}
			return dialer.DialContext(ctx, network, addr)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       &tls.Config{RootCAs: e.RootCAs},
	}
}

func (e *E2EEndpoints) HTTPClient() *http.Client {
	return &http.Client{Transport: e.Transport(), Timeout: 30 * time.Second}
}

func (env *E2EEnvironment) EnterpriseOrigin() string { return env.Endpoints.EnterpriseOrigin }
func (env *E2EEnvironment) PlatformOrigin() string   { return env.Endpoints.PlatformOrigin }
func (env *E2EEnvironment) CardOrigin() string       { return env.Endpoints.CardOrigin }

// resolveE2EAccess waits for the ingress and connector load balancers to be
// allocated, loads the ingress CA, and records the domain endpoints the
// scenarios and Playwright use. It replaces the former port-forward setup.
func (a *App) resolveE2EAccess(ctx context.Context, env *E2EEnvironment) error {
	hosts, err := e2eExposureHosts(env.ConfigPath)
	if err != nil {
		return err
	}
	ingressIP, err := waitForIngressIP(ctx, env, 5*time.Minute)
	if err != nil {
		return err
	}
	connectorIP, err := waitForServiceIP(ctx, env, env.SystemNS, "argus-connector-gateway-public", 5*time.Minute)
	if err != nil {
		return err
	}
	// 与 ingress-certificates.yaml 的多 SAN 证书 secret 名保持一致。
	caPEM, err := env.Kube.SecretValue(ctx, env.SystemNS, "argus-web-tls", "ca.crt")
	if err != nil {
		return fmt.Errorf("read ingress CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return fmt.Errorf("ingress CA from secret argus-web-tls could not be parsed")
	}
	caFile := filepath.Join(env.Options.Artifacts, "argus-e2e-ingress-ca.pem")
	if err := writePrivate(caFile, []byte(caPEM)); err != nil {
		return err
	}

	// Docker Desktop 把 LoadBalancer 端口同时发布到宿主机 localhost;
	// 优先用 127.0.0.1 直连——某些本机代理(TUN)会拦截宿主机到
	// Docker 桥网段 LB IP 的出站流量,导致直拨 LB IP 被重置。
	ingressDialIP, connectorDialIP := ingressIP, connectorIP
	if dialReachable(ctx, "127.0.0.1:443") {
		ingressDialIP = "127.0.0.1"
	}
	if dialReachable(ctx, "127.0.0.1:9443") {
		connectorDialIP = "127.0.0.1"
	}

	endpoints := &E2EEndpoints{
		EnterpriseOrigin: "https://" + hosts["enterprise"],
		PlatformOrigin:   "https://" + hosts["platform"],
		CardOrigin:       "https://" + hosts["cards"],
		RemoteOrigin:     "wss://" + hosts["remote"],
		APIBase:          "https://" + hosts["platform"] + "/api/v1",
		ConnectorGateway: "grpcs://" + hosts["connector"] + ":9443",
		EnrollServer:     "https://" + hosts["enterprise"],
		IngressIP:        ingressIP,
		ConnectorIP:      connectorIP,
		// 子进程(connector 等)经地址覆盖直拨;使用与 hostIPs 相同的回退地址,
		// 避免宿主机代理拦截 Docker 桥网段 LB IP。
		IngressDialAddress:   net.JoinHostPort(ingressDialIP, "443"),
		ConnectorDialAddress: net.JoinHostPort(connectorDialIP, "9443"),
		CAFile:               caFile,
		RootCAs:              pool,
		hostIPs: map[string]string{
			hosts["enterprise"]: ingressDialIP,
			hosts["platform"]:   ingressDialIP,
			hosts["cards"]:      ingressDialIP,
			hosts["remote"]:     ingressDialIP,
			hosts["connector"]:  connectorDialIP,
		},
	}
	var resolver strings.Builder
	for _, host := range []string{hosts["enterprise"], hosts["platform"], hosts["cards"], hosts["remote"]} {
		fmt.Fprintf(&resolver, "MAP %s %s,", host, ingressDialIP)
	}
	fmt.Fprintf(&resolver, "MAP %s %s", hosts["connector"], connectorDialIP)
	endpoints.HostResolver = resolver.String()
	env.Endpoints = endpoints

	httpClient := endpoints.HTTPClient()
	if err := waitHTTPSReady(ctx, httpClient, endpoints.PlatformOrigin+"/readyz", "ready", 2*time.Minute); err != nil {
		return fmt.Errorf("platform portal through ingress: %w", err)
	}
	if err := waitHTTPSReady(ctx, httpClient, endpoints.EnterpriseOrigin+"/healthz", "ok", 2*time.Minute); err != nil {
		return fmt.Errorf("enterprise portal through ingress: %w", err)
	}
	return nil
}

// e2eExposureHosts reads the exposure hosts (including the derived cards and
// remote hosts) from the generated install config so the access layer stays
// profile-agnostic.
func e2eExposureHosts(configPath string) (map[string]string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var document struct {
		Spec struct {
			Exposure struct {
				EnterpriseHost string `json:"enterpriseHost"`
				PlatformHost   string `json:"platformHost"`
				ConnectorHost  string `json:"connectorHost"`
			} `json:"exposure"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	exposure := document.Spec.Exposure
	if exposure.EnterpriseHost == "" || exposure.PlatformHost == "" || exposure.ConnectorHost == "" {
		return nil, fmt.Errorf("install config %s lacks exposure hosts", configPath)
	}
	return map[string]string{
		"enterprise": exposure.EnterpriseHost,
		"platform":   exposure.PlatformHost,
		"connector":  exposure.ConnectorHost,
		"cards":      "cards." + e2eParentDomain(exposure.EnterpriseHost),
		"remote":     "remote." + e2eParentDomain(exposure.PlatformHost),
	}, nil
}

// e2eParentDomain mirrors argusctl's derivation: platform.argus.dev ->
// argus.dev, shorter hosts unchanged.
func e2eParentDomain(host string) string {
	labels := strings.Split(host, ".")
	if len(labels) < 3 {
		return host
	}
	return strings.Join(labels[1:], ".")
}

func waitForIngressIP(ctx context.Context, env *E2EEnvironment, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ingress, err := env.Kube.Client.NetworkingV1().Ingresses(env.SystemNS).Get(ctx, "argus-web", metav1.GetOptions{})
		if err == nil {
			if ip := ingressLoadBalancerIP(ingress.Status.LoadBalancer.Ingress); ip != "" {
				return ip, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return "", fmt.Errorf("Ingress argus-web has no load-balancer IP after %s", timeout)
}

func waitForServiceIP(ctx context.Context, env *E2EEnvironment, namespace, name string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		service, err := env.Kube.Client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			if ip := serviceLoadBalancerIP(service.Status.LoadBalancer.Ingress); ip != "" {
				return ip, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return "", fmt.Errorf("Service %s/%s has no load-balancer IP after %s", namespace, name, timeout)
}

func ingressLoadBalancerIP(entries []networkingv1.IngressLoadBalancerIngress) string {
	for _, entry := range entries {
		if entry.IP != "" {
			return entry.IP
		}
	}
	return ""
}

func serviceLoadBalancerIP(entries []corev1.LoadBalancerIngress) string {
	for _, entry := range entries {
		if entry.IP != "" {
			return entry.IP
		}
	}
	return ""
}

func waitHTTPSReady(ctx context.Context, client *http.Client, url, expected string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if requestErr == nil {
			response, err := client.Do(request)
			if err == nil {
				body, readErr := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if readErr == nil && response.StatusCode < 400 && strings.Contains(string(body), expected) {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("HTTPS endpoint %s did not become ready", url)
}

// dialReachable 探测 TCP 地址是否可建立连接;用于选择宿主机 localhost
// 发布端口或 LoadBalancer IP 作为 E2E 拨号地址。
func dialReachable(ctx context.Context, address string) bool {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}
