package collectormanager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/otelcol/configbundle"
)

type SSHOptions struct {
	Credential []byte
	Dial       func(context.Context) (net.Conn, error)
	Revalidate func(context.Context) error
}

func (manager Manager) ApplySSH(ctx context.Context, command *connectorv1.CollectorManagementCommand, options SSHOptions) (Result, error) {
	if err := Validate(command); err != nil {
		return Result{}, err
	}
	if command.GetResourceType() != "host" || command.GetTargetUsername() == "" || command.GetTargetPort() == 0 ||
		command.GetPinnedHostKey() == "" || len(options.Credential) == 0 || options.Dial == nil || options.Revalidate == nil {
		return Result{}, ErrInvalidCommand
	}
	auth, err := sshAuthentication(options.Credential)
	if err != nil {
		return Result{}, err
	}
	configuration := &ssh.ClientConfig{User: command.GetTargetUsername(), Auth: []ssh.AuthMethod{auth}, Timeout: 15 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if ssh.FingerprintSHA256(key) != command.GetPinnedHostKey() {
				return ErrTargetHostKeyChanged
			}
			return nil
		}}
	connection, err := options.Dial(ctx)
	if err != nil {
		return Result{}, err
	}
	defer connection.Close()
	if err = options.Revalidate(ctx); err != nil {
		return Result{}, err
	}
	transport, channels, requests, err := ssh.NewClientConn(connection,
		net.JoinHostPort(command.GetTargetAddress(), fmt.Sprint(command.GetTargetPort())), configuration)
	if err != nil {
		if strings.Contains(err.Error(), "unable to authenticate") {
			return Result{}, fmt.Errorf("%w: %w", ErrTargetAuthFailed, err)
		}
		return Result{}, err
	}
	client := ssh.NewClient(transport, channels, requests)
	defer client.Close()
	directory := "/var/lib/argus-otelcol/" + command.GetCollectorId()
	if command.GetOperation() == "uninstall" {
		fixed := "systemctl disable --now argus-otelcol.service >/dev/null 2>&1 || true; rm -rf /etc/argus-otelcol " + directory + " /etc/systemd/system/argus-otelcol.service; systemctl daemon-reload"
		if err = runSSH(client, fixed, nil); err != nil {
			return Result{}, err
		}
		return sshResult(command, "uninstalled"), nil
	}
	artifactFile, err := os.CreateTemp("", ".argus-collector-artifact-*")
	if err != nil {
		return Result{}, err
	}
	artifactPath := artifactFile.Name()
	defer func() {
		_ = artifactFile.Close()
		_ = os.Remove(artifactPath)
	}()
	if err = artifactFile.Chmod(0o600); err != nil {
		return Result{}, err
	}
	if err = manager.FetchArtifactTo(ctx, command.GetArtifact(), artifactFile); err != nil {
		return Result{}, err
	}
	if _, err = artifactFile.Seek(0, io.SeekStart); err != nil {
		return Result{}, err
	}
	prepare := "umask 077; install -d -m 0700 " + directory + "/release /etc/argus-otelcol; cat > " + directory + "/artifact.tar.gz"
	if err = runSSHReader(client, prepare, artifactFile); err != nil {
		return Result{}, err
	}
	runtimeConfig, err := configbundle.Extract(command.GetRenderedConfig(), "host")
	if err != nil {
		return Result{}, err
	}
	if err = runSSH(client, "umask 077; cat > /etc/argus-otelcol/config.yaml", runtimeConfig); err != nil {
		return Result{}, err
	}
	trust, err := commandTrustBundle(command)
	if err != nil {
		return Result{}, err
	}
	if err = runSSH(client, "umask 077; cat > /etc/argus-otelcol/server-ca.pem", trust.PEM); err != nil {
		return Result{}, err
	}
	prepareIdentity := "active=''; [ -f /var/lib/argus-otelcol/.active-collector-id ] && active=$(cat /var/lib/argus-otelcol/.active-collector-id); " +
		"if [ \"$active\" != '" + command.GetCollectorId() + "' ]; then systemctl disable --now argus-otelcol.service >/dev/null 2>&1 || true; rm -rf /var/lib/argus-otelcol/identity; fi; " +
		"install -d -m 0700 /var/lib/argus-otelcol/identity; printf '%s' '" + command.GetCollectorId() + "' > /var/lib/argus-otelcol/.active-collector-id"
	if err = runSSH(client, prepareIdentity, nil); err != nil {
		return Result{}, err
	}
	if len(command.GetEnrollmentToken()) > 0 {
		writeToken := "umask 077; if [ -s /var/lib/argus-otelcol/identity/client.pem ]; then cat >/dev/null; rm -f /etc/argus-otelcol/enrollment-token; else cat > /etc/argus-otelcol/enrollment-token; fi"
		if err = runSSH(client, writeToken, command.GetEnrollmentToken()); err != nil {
			return Result{}, err
		}
	}
	environment := "Environment=ARGUS_TELEMETRY_ENROLLMENT_TOKEN_FILE=/etc/argus-otelcol/enrollment-token\n" +
		"Environment=ARGUS_TELEMETRY_ENROLLMENT_ENDPOINT=" + command.GetEnrollmentEndpoint() + "\n" +
		"Environment=ARGUS_TELEMETRY_INGEST_GRPC_ENDPOINT=" + command.GetIngestGrpcEndpoint() + "\n" +
		"Environment=ARGUS_TELEMETRY_INGEST_HTTP_ENDPOINT=" + command.GetIngestHttpEndpoint() + "\n"
	unit := []byte("[Unit]\nDescription=Argus managed OpenTelemetry Collector\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\n" + environment + "ExecStart=/usr/local/bin/argus-otelcol --config=/etc/argus-otelcol/config.yaml\nRestart=always\nRestartSec=5\nNoNewPrivileges=true\nProtectSystem=strict\nProtectHome=true\nReadWritePaths=/var/lib/argus-otelcol /etc/argus-otelcol\n\n[Install]\nWantedBy=multi-user.target\n")
	if err = runSSH(client, "umask 077; cat > /etc/systemd/system/argus-otelcol.service", unit); err != nil {
		return Result{}, err
	}
	activate := sshActivateCommand(directory)
	if err = runSSH(client, activate, nil); err != nil {
		return Result{}, err
	}
	return sshResult(command, "converged"), nil
}

func sshActivateCommand(directory string) string {
	return "rm -rf " + directory + "/release/*; tar -xzf " + directory + "/artifact.tar.gz -C " + directory +
		"/release; test -x " + directory + "/release/argus-otelcol; install -m 0755 " + directory +
		"/release/argus-otelcol /usr/local/bin/argus-otelcol; systemctl daemon-reload; systemctl enable --now argus-otelcol.service; " +
		"i=0; while [ \"$i\" -lt 90 ]; do if systemctl is-active --quiet argus-otelcol.service && " +
		"test -s /var/lib/argus-otelcol/identity/client.pem && test -s /var/lib/argus-otelcol/identity/client-key.pem && " +
		"test -s /var/lib/argus-otelcol/identity/ca.pem && test ! -e /etc/argus-otelcol/enrollment-token; then exit 0; fi; " +
		"i=$((i+1)); sleep 1; done; exit 1"
}

func sshAuthentication(value []byte) (ssh.AuthMethod, error) {
	if bytes.Contains(value, []byte("PRIVATE KEY")) {
		signer, err := ssh.ParsePrivateKey(value)
		if err != nil {
			return nil, err
		}
		return ssh.PublicKeys(signer), nil
	}
	return ssh.Password(string(value)), nil
}

func runSSH(client *ssh.Client, command string, input []byte) error {
	if input == nil {
		return runSSHReader(client, command, nil)
	}
	return runSSHReader(client, command, bytes.NewReader(input))
}

func runSSHReader(client *ssh.Client, command string, input io.Reader) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	if input != nil {
		session.Stdin = input
	}
	// 远端 stderr 必须进入错误信息:目标缺命令(exit 127)、权限不足等
	// 失败原因只有远端输出能说明,仅退出码无法诊断。
	var stderr bytes.Buffer
	session.Stderr = &stderr
	if err = session.Run(command); err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	return nil
}

func sshResult(command *connectorv1.CollectorManagementCommand, status string) Result {
	diagnostic := sha256.Sum256([]byte(strings.Join([]string{command.GetCollectorId(), status, command.GetConfigSha256()}, "\x00")))
	return Result{CollectorID: command.GetCollectorId(), EffectiveRevision: command.GetDesiredRevision(), ConfigSHA256: strings.ToLower(command.GetConfigSha256()),
		Status: status, DiagnosticHash: hex.EncodeToString(diagnostic[:])}
}
