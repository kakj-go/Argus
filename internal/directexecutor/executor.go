package directexecutor

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/ssh"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

const maxProbeResponseBytes = 64 * 1024

type Executor struct {
	Store       *postgres.Store
	Secrets     secret.Service
	Validator   resource.DirectTargetValidator
	InstanceID  string
	Concurrency int
	Timeout     time.Duration
	slotOnce    sync.Once
	slots       chan struct{}
}

type connectionPlan struct {
	TargetType        string   `json:"target_type"`
	Address           string   `json:"address"`
	Port              int32    `json:"port"`
	Username          string   `json:"username"`
	ConnectionMode    string   `json:"connection_mode"`
	CredentialVersion int64    `json:"credential_version"`
	ResolvedIPs       []string `json:"resolved_ips"`
}

func (executor *Executor) Run(ctx context.Context) error {
	concurrency := executor.concurrency()
	if concurrency < 1 || concurrency > 64 {
		concurrency = 8
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			_, _ = executor.Store.Queries.ExpireQueuedConnectionTests(ctx)
			reserved := executor.reserveAvailable()
			if reserved == 0 {
				continue
			}
			tests, err := executor.Store.Queries.ClaimDirectConnectionTests(ctx, int32(reserved))
			if err != nil {
				executor.release(reserved)
				return err
			}
			executor.release(reserved - len(tests))
			for _, test := range tests {
				go func(value db.ConnectionTest) {
					defer executor.release(1)
					executor.execute(ctx, value)
				}(test)
			}
		}
	}
}

func (executor *Executor) concurrency() int {
	if executor.Concurrency < 1 || executor.Concurrency > 64 {
		return 8
	}
	return executor.Concurrency
}

func (executor *Executor) ensureSlots() chan struct{} {
	executor.slotOnce.Do(func() {
		executor.slots = make(chan struct{}, executor.concurrency())
	})
	return executor.slots
}

func (executor *Executor) reserveAvailable() int {
	slots := executor.ensureSlots()
	reserved := 0
	for reserved < cap(slots) {
		select {
		case slots <- struct{}{}:
			reserved++
		default:
			return reserved
		}
	}
	return reserved
}

func (executor *Executor) reserveOne() bool {
	select {
	case executor.ensureSlots() <- struct{}{}:
		return true
	default:
		return false
	}
}

func (executor *Executor) release(count int) {
	for range count {
		<-executor.ensureSlots()
	}
}

func VerifyEgress(ctx context.Context, verificationURL string, advertised []string) error {
	return verifyEgress(ctx, verificationURL, advertised, &http.Client{Timeout: 10 * time.Second, CheckRedirect: rejectRedirect})
}

func verifyEgress(ctx context.Context, verificationURL string, advertised []string, client *http.Client) error {
	if verificationURL == "" && len(advertised) == 0 {
		return nil
	}
	parsed, err := url.Parse(verificationURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return errors.New("egress verification URL must be HTTPS")
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(body) > 4096 || response.StatusCode != http.StatusOK {
		return errors.New("egress verification endpoint failed")
	}
	observed := strings.TrimSpace(string(body))
	var payload struct {
		IP string `json:"ip"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.IP != "" {
		observed = payload.IP
	}
	address, err := netip.ParseAddr(observed)
	if err != nil {
		return errors.New("egress verification returned an invalid address")
	}
	for _, expected := range advertised {
		if value, parseErr := netip.ParseAddr(expected); parseErr == nil && value.Unmap() == address.Unmap() {
			return nil
		}
	}
	return errors.New("observed egress address is not advertised")
}

func (executor *Executor) execute(parent context.Context, test db.ConnectionTest) {
	timeout := executor.Timeout
	if timeout <= 0 || timeout > 2*time.Minute {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	var plan connectionPlan
	if err := json.Unmarshal(test.RequestPlan, &plan); err != nil || len(plan.ResolvedIPs) == 0 || !test.CredentialID.Valid {
		executor.finish(ctx, test, "failed", resource.ConnectionTestResult{}, "DIRECT_PLAN_INVALID")
		return
	}
	addresses, err := parseAddresses(plan.ResolvedIPs)
	if err != nil {
		executor.finish(ctx, test, "failed", resource.ConnectionTestResult{}, "DIRECT_TARGET_DENIED")
		return
	}
	protocol := "ssh"
	if plan.ConnectionMode == "direct_winrm" {
		protocol = "winrm"
	} else if test.TargetType == "kubernetes_cluster" {
		protocol = "kubernetes"
	}
	lease, err := executor.Secrets.IssueLease(secret.WithActorType(ctx, "direct_executor"), executor.InstanceID, test.EnterpriseID, secret.LeaseRequest{
		CredentialID: test.CredentialID.UUID, OperationRef: test.ID.String(), TargetResourceType: "connection_test", TargetResourceID: test.ID,
		RecipientType: "direct_executor", RecipientID: executor.InstanceID, Protocol: protocol, TTL: time.Minute})
	if err != nil {
		executor.finish(ctx, test, "failed", resource.ConnectionTestResult{}, "CREDENTIAL_UNAVAILABLE")
		return
	}
	defer clear(lease.Value)
	credential, err := executor.Store.Queries.GetCredential(ctx, db.GetCredentialParams{ID: test.CredentialID.UUID, EnterpriseID: test.EnterpriseID})
	if err != nil || credential.Version != plan.CredentialVersion {
		executor.finish(ctx, test, "failed", resource.ConnectionTestResult{}, "CREDENTIAL_VERSION_STALE")
		return
	}
	started := time.Now()
	var result resource.ConnectionTestResult
	switch protocol {
	case "ssh":
		result, err = executor.probeSSH(ctx, plan, addresses, plan.Username, lease.Value)
	case "winrm":
		result, err = executor.probeWinRM(ctx, plan, addresses, plan.Username, lease.Value)
	case "kubernetes":
		result, err = executor.probeKubernetes(ctx, plan, addresses, lease.Value)
	}
	result.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		code := "CONNECTION_TEST_FAILED"
		if errors.Is(err, resource.ErrDirectTargetDenied) {
			code = "DIRECT_TARGET_DENIED"
		}
		executor.finish(ctx, test, "failed", result, code)
		return
	}
	_ = executor.Secrets.ConsumeLease(ctx, test.EnterpriseID, lease.Lease.ID)
	executor.finish(ctx, test, "succeeded", result, "")
}

func (executor *Executor) probeSSH(ctx context.Context, plan connectionPlan, addresses []netip.Addr, username string, credential []byte) (resource.ConnectionTestResult, error) {
	auth, err := sshAuth(credential)
	if err != nil || username == "" {
		return resource.ConnectionTestResult{}, errors.New("invalid SSH credential")
	}
	var fingerprint string
	config := &ssh.ClientConfig{User: username, Auth: []ssh.AuthMethod{auth}, Timeout: executor.Timeout,
		HostKeyCallback: captureHostKey(&fingerprint)}
	connection, err := dialFixed(ctx, addresses[0], plan.Port)
	if err != nil {
		return resource.ConnectionTestResult{}, err
	}
	defer connection.Close()
	if err := executor.Validator.Revalidate(ctx, plan.Address, addresses); err != nil {
		return resource.ConnectionTestResult{}, err
	}
	clientConnection, channels, requests, err := ssh.NewClientConn(connection, net.JoinHostPort(plan.Address, fmt.Sprint(plan.Port)), config)
	if err != nil {
		return resource.ConnectionTestResult{}, err
	}
	client := ssh.NewClient(clientConnection, channels, requests)
	defer client.Close()
	return resource.ConnectionTestResult{ResolvedIPs: addressStrings(addresses), HostKeyFingerprint: fingerprint, RemoteVersion: string(clientConnection.ServerVersion())}, nil
}

func (executor *Executor) probeWinRM(ctx context.Context, plan connectionPlan, addresses []netip.Addr, username string, credential []byte) (resource.ConnectionTestResult, error) {
	scheme := "http"
	if plan.Port == 5986 || plan.Port == 443 {
		scheme = "https"
	}
	target := &url.URL{Scheme: scheme, Host: net.JoinHostPort(plan.Address, fmt.Sprint(plan.Port)), Path: "/wsman"}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: plan.Address},
		DialContext: fixedDialer(addresses[0], plan.Port)}
	client := &http.Client{Transport: transport, Timeout: executor.Timeout, CheckRedirect: rejectRedirect}
	body := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body/></s:Envelope>`
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), strings.NewReader(body))
	request.Header.Set("Content-Type", "application/soap+xml;charset=UTF-8")
	request.SetBasicAuth(username, string(credential))
	response, err := client.Do(request)
	if err != nil {
		return resource.ConnectionTestResult{}, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxProbeResponseBytes))
	if err := executor.Validator.Revalidate(ctx, plan.Address, addresses); err != nil {
		return resource.ConnectionTestResult{}, err
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode >= 500 {
		return resource.ConnectionTestResult{}, errors.New("WinRM authentication failed")
	}
	return resource.ConnectionTestResult{ResolvedIPs: addressStrings(addresses), RemoteVersion: response.Header.Get("Server")}, nil
}

func (executor *Executor) probeKubernetes(ctx context.Context, plan connectionPlan, addresses []netip.Addr, kubeconfig []byte) (resource.ConnectionTestResult, error) {
	config, err := safeKubernetesConfig(kubeconfig)
	if err != nil {
		return resource.ConnectionTestResult{}, err
	}
	target, _, err := executor.Validator.ResolveHTTPS(ctx, plan.Address)
	if err != nil {
		return resource.ConnectionTestResult{}, err
	}
	config.Host = target.Scheme + "://" + target.Host
	if config.ServerName == "" {
		config.ServerName = target.Hostname()
	}
	config.Dial = fixedDialer(addresses[0], int32(portForURL(target)))
	transport, err := rest.TransportFor(config)
	if err != nil {
		return resource.ConnectionTestResult{}, err
	}
	client := &http.Client{Transport: transport, Timeout: executor.Timeout, CheckRedirect: rejectRedirect}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(config.Host, "/")+"/version", nil)
	response, err := client.Do(request)
	if err != nil {
		return resource.ConnectionTestResult{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProbeResponseBytes+1))
	if err != nil || len(body) > maxProbeResponseBytes || response.StatusCode != http.StatusOK {
		return resource.ConnectionTestResult{}, errors.New("Kubernetes version probe failed")
	}
	if err := executor.Validator.Revalidate(ctx, target.Hostname(), addresses); err != nil {
		return resource.ConnectionTestResult{}, err
	}
	var version struct {
		GitVersion string `json:"gitVersion"`
	}
	if json.Unmarshal(body, &version) != nil || version.GitVersion == "" {
		return resource.ConnectionTestResult{}, errors.New("invalid Kubernetes version response")
	}
	return resource.ConnectionTestResult{ResolvedIPs: addressStrings(addresses), RemoteVersion: version.GitVersion}, nil
}

func (executor *Executor) finish(ctx context.Context, test db.ConnectionTest, status string, result resource.ConnectionTestResult, errorCode string) {
	encoded, _ := json.Marshal(result)
	_, _ = executor.Store.Queries.CompleteConnectionTest(ctx, db.CompleteConnectionTestParams{ID: test.ID, EnterpriseID: test.EnterpriseID,
		Status: status, Result: encoded, ErrorCode: optionalError(errorCode)})
}

func sshAuth(value []byte) (ssh.AuthMethod, error) {
	if bytes.Contains(value, []byte("PRIVATE KEY")) {
		signer, err := ssh.ParsePrivateKey(value)
		if err != nil {
			return nil, err
		}
		return ssh.PublicKeys(signer), nil
	}
	return ssh.Password(string(value)), nil
}

func captureHostKey(fingerprint *string) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		*fingerprint = ssh.FingerprintSHA256(key)
		return nil
	}
}

func safeKubernetesConfig(kubeconfig []byte) (*rest.Config, error) {
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil || config.Insecure || config.ExecProvider != nil || config.AuthProvider != nil || config.Proxy != nil || config.WrapTransport != nil {
		return nil, errors.New("unsafe kubeconfig")
	}
	return config, nil
}

func parseAddresses(values []string) ([]netip.Addr, error) {
	result := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		address, err := netip.ParseAddr(value)
		if err != nil {
			return nil, resource.ErrDirectTargetDenied
		}
		result = append(result, address.Unmap())
	}
	return result, nil
}

func dialFixed(ctx context.Context, address netip.Addr, port int32) (net.Conn, error) {
	return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(address.String(), fmt.Sprint(port)))
}

func fixedDialer(address netip.Addr, port int32) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) { return dialFixed(ctx, address, port) }
}

func rejectRedirect(_ *http.Request, _ []*http.Request) error {
	return errors.New("redirects are not allowed")
}

func portForURL(value *url.URL) int {
	if value.Port() != "" {
		var port int
		_, _ = fmt.Sscan(value.Port(), &port)
		return port
	}
	return 443
}

func addressStrings(values []netip.Addr) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = value.String()
	}
	return result
}

func optionalError(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }
