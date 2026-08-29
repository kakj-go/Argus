package argusdev

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type m6RemoteCase struct {
	name      string
	hostID    string
	accountID string
	protocol  string
	command   string
	expect    string
}

func (a *App) verifyM6RemoteSession(ctx context.Context, env *E2EEnvironment, test m6RemoteCase) (string, error) {
	client, _ := scenarioHTTP(env)
	request, err := client.JSON(ctx, "m6-"+test.name+"-request", "enterprise", http.MethodPost, "/enterprise/remote-access-requests", http.StatusCreated,
		map[string]any{"host_id": test.hostID, "managed_account_id": test.accountID, "protocol": test.protocol, "action": "terminal", "reason": "M6 E2E " + test.name + " recording"}, enterpriseHeaders(env, "m6-"+test.name+"-request"))
	if err != nil {
		return "", err
	}
	if request["status"] != "authorized" {
		return "", fmt.Errorf("M6 %s request status is %v", test.name, request["status"])
	}
	requestID, err := stringField(request, "id")
	if err != nil {
		return "", err
	}
	leaseID, err := a.findM6Lease(ctx, env, requestID)
	if err != nil {
		return "", err
	}
	session, err := client.JSON(ctx, "m6-"+test.name+"-session", "enterprise", http.MethodPost, "/enterprise/remote-access-sessions", http.StatusCreated,
		map[string]any{"lease_id": leaseID, "terminal_cols": 100, "terminal_rows": 30}, enterpriseHeaders(env, "m6-"+test.name+"-session"))
	if err != nil {
		return "", err
	}
	sessionID, _ := stringField(session, "id")
	recordingID, _ := stringField(session, "recording_id")
	ticketResult, err := client.JSON(ctx, "m6-"+test.name+"-ticket", "enterprise", http.MethodPost, "/enterprise/remote-access-sessions/"+sessionID+"/tickets", http.StatusCreated, nil, enterpriseHeaders(env, "m6-"+test.name+"-ticket"))
	if err != nil {
		return "", err
	}
	ticket, err := validateM6Ticket(ticketResult)
	if err != nil {
		return "", err
	}
	websocketURL, _ := stringField(ticketResult, "websocket_url")
	args := []string{"--url", websocketURL, "--origin", env.EnterpriseOrigin(), "--command", test.command, "--expect", test.expect}
	if err := a.runM6RemoteClient(ctx, env, "m6-"+test.name+"-client.log", ticket, args...); err != nil {
		return "", err
	}
	if err := a.runM6RemoteClient(ctx, env, "m6-"+test.name+"-ticket-replay.log", ticket, args...); err == nil {
		return "", fmt.Errorf("M6 %s ticket replay unexpectedly succeeded", test.name)
	}
	if err := a.waitM6Recording(ctx, env, recordingID); err != nil {
		return "", err
	}
	return leaseID, nil
}

func (a *App) verifyM6TerminatedTicket(ctx context.Context, env *E2EEnvironment, leaseID string) error {
	client, _ := scenarioHTTP(env)
	session, err := client.JSON(ctx, "m6-terminate-session", "enterprise", http.MethodPost, "/enterprise/remote-access-sessions", http.StatusCreated,
		map[string]any{"lease_id": leaseID, "terminal_cols": 100, "terminal_rows": 30}, enterpriseHeaders(env, "m6-terminate-session"))
	if err != nil {
		return err
	}
	sessionID, _ := stringField(session, "id")
	ticketResult, err := client.JSON(ctx, "m6-terminate-ticket", "enterprise", http.MethodPost, "/enterprise/remote-access-sessions/"+sessionID+"/tickets", http.StatusCreated, nil, enterpriseHeaders(env, "m6-terminate-ticket"))
	if err != nil {
		return err
	}
	ticket, err := validateM6Ticket(ticketResult)
	if err != nil {
		return err
	}
	websocketURL, _ := stringField(ticketResult, "websocket_url")
	if _, err := client.JSON(ctx, "m6-terminate", "enterprise", http.MethodPost, "/enterprise/remote-access-sessions/"+sessionID+"/terminate", http.StatusOK,
		map[string]any{"reason": "e2e_terminate"}, enterpriseHeaders(env, "m6-terminate")); err != nil {
		return err
	}
	if err := a.runM6RemoteClient(ctx, env, "m6-terminated-ticket.log", ticket, "--url", websocketURL, "--origin", env.EnterpriseOrigin()); err == nil {
		return fmt.Errorf("M6 terminated Session accepted an unused Ticket")
	}
	return nil
}

func (a *App) verifyM6ObjectStoreFailClosed(ctx context.Context, env *E2EEnvironment, leaseID string) error {
	client, _ := scenarioHTTP(env)
	session, err := client.JSON(ctx, "m6-object-store-session", "enterprise", http.MethodPost, "/enterprise/remote-access-sessions", http.StatusCreated,
		map[string]any{"lease_id": leaseID, "terminal_cols": 100, "terminal_rows": 30}, enterpriseHeaders(env, "m6-object-store-session"))
	if err != nil {
		return err
	}
	sessionID, _ := stringField(session, "id")
	ticketResult, err := client.JSON(ctx, "m6-object-store-ticket", "enterprise", http.MethodPost, "/enterprise/remote-access-sessions/"+sessionID+"/tickets", http.StatusCreated, nil, enterpriseHeaders(env, "m6-object-store-ticket"))
	if err != nil {
		return err
	}
	ticket, err := validateM6Ticket(ticketResult)
	if err != nil {
		return err
	}
	websocketURL, _ := stringField(ticketResult, "websocket_url")
	if err := env.Kube.ScaleStatefulSet(ctx, env.SystemNS, "argus-minio", 0); err != nil {
		return err
	}
	if err := env.Kube.WaitStatefulSet(ctx, env.SystemNS, "argus-minio", 0, 2*time.Minute); err != nil {
		return err
	}
	runErr := a.runM6RemoteClient(ctx, env, "m6-object-store-fail-closed.log", ticket,
		"--url", websocketURL, "--origin", env.EnterpriseOrigin(), "--command", "stream", "--expect-status", "failed", "--expect-reason", "REMOTE_ACCESS_RECORDING_UNAVAILABLE", "--timeout", "90s")
	restoreCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	restoreErr := env.Kube.ScaleStatefulSet(restoreCtx, env.SystemNS, "argus-minio", 1)
	if restoreErr == nil {
		restoreErr = env.Kube.WaitStatefulSet(restoreCtx, env.SystemNS, "argus-minio", 1, 5*time.Minute)
	}
	if runErr != nil {
		return runErr
	}
	return restoreErr
}

func validateM6Ticket(result map[string]any) (string, error) {
	ticket, _ := result["ticket"].(string)
	websocketURL, _ := result["websocket_url"].(string)
	decoded, decodeErr := base64.RawURLEncoding.DecodeString(ticket)
	parsedURL, parseErr := url.Parse(websocketURL)
	hasTicketQuery := false
	if parseErr == nil {
		for key := range parsedURL.Query() {
			hasTicketQuery = hasTicketQuery || strings.EqualFold(key, "ticket")
		}
	}
	if result["protocol_version"] != "argus.remote_access/v1" || decodeErr != nil || len(decoded) != 32 || parseErr != nil ||
		(parsedURL.Scheme != "ws" && parsedURL.Scheme != "wss") || parsedURL.Host == "" || hasTicketQuery {
		return "", fmt.Errorf("M6 remote access ticket does not match the public contract")
	}
	return ticket, nil
}

func (a *App) findM6Lease(ctx context.Context, env *E2EEnvironment, requestID string) (string, error) {
	client, _ := scenarioHTTP(env)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		leases, err := client.JSON(ctx, "m6-leases-"+requestID, "enterprise", http.MethodGet, "/enterprise/remote-access-leases", http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
		if err != nil {
			return "", err
		}
		for _, lease := range objectItems(leases) {
			if lease["request_id"] == requestID && lease["revoked"] == false {
				return stringField(lease, "id")
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return "", fmt.Errorf("M6 lease for request %s did not appear", requestID)
}

func (a *App) runM6RemoteClient(ctx context.Context, env *E2EEnvironment, artifact, ticket string, args ...string) error {
	binary := filepath.Join(env.WorkDir, "argus-e2e-remoteclient")
	if _, err := os.Stat(binary); err != nil {
		if err := a.runner.Run(ctx, nil, "go", "build", "-trimpath", "-o", binary, "./tests/e2e/remoteclient"); err != nil {
			return err
		}
	}
	log, err := openArtifact(filepath.Join(env.Options.Artifacts, artifact))
	if err != nil {
		return err
	}
	defer log.Close()
	variables := map[string]string{
		"ARGUS_E2E_CA_FILE": env.Endpoints.CAFile,
	}
	var hostMap strings.Builder
	for host, ip := range env.Endpoints.hostIPs {
		if hostMap.Len() > 0 {
			hostMap.WriteByte(',')
		}
		hostMap.WriteString(host + "=" + ip)
	}
	variables["ARGUS_E2E_HOST_MAP"] = hostMap.String()
	return a.runner.RunIO(ctx, variables, strings.NewReader(ticket+"\n"), log, log, binary, args...)
}

func (a *App) waitM6Recording(ctx context.Context, env *E2EEnvironment, recordingID string) error {
	client, _ := scenarioHTTP(env)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		recording, err := client.JSON(ctx, "m6-recording-"+recordingID, "enterprise", http.MethodGet, "/enterprise/remote-access-recordings/"+recordingID, http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
		if err != nil {
			return err
		}
		if recording["status"] == "available" {
			chunkCount, _ := numberField(recording, "chunk_count")
			eventCount, _ := numberField(recording, "event_count")
			if chunkCount < 1 || eventCount < 3 {
				return fmt.Errorf("M6 recording %s has incomplete counters", recordingID)
			}
			events, err := client.JSON(ctx, "m6-recording-events-"+recordingID, "enterprise", http.MethodGet, "/enterprise/remote-access-recordings/"+recordingID+"/events", http.StatusOK, nil, map[string]string{"Origin": env.EnterpriseOrigin()})
			if err != nil {
				return err
			}
			raw, _ := events["events"].([]any)
			seen := map[string]bool{}
			for _, value := range raw {
				entry, _ := value.(map[string]any)
				kind, _ := entry["type"].(string)
				seen[kind] = true
			}
			if !seen["i"] || !seen["o"] || !seen["r"] {
				return fmt.Errorf("M6 recording %s is missing input, output, or resize events", recordingID)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("M6 recording %s did not become available", recordingID)
}
