package collectormanager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
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
				return errors.New("Collector target Host key changed")
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
	artifact, err := manager.FetchArtifact(ctx, command.GetArtifact())
	if err != nil {
		return Result{}, err
	}
	prepare := "umask 077; install -d -m 0700 " + directory + "/release /etc/argus-otelcol; cat > " + directory + "/artifact.tar.gz"
	if err = runSSH(client, prepare, artifact); err != nil {
		return Result{}, err
	}
	runtimeConfig, err := configbundle.Extract(command.GetRenderedConfig(), "host")
	if err != nil {
		return Result{}, err
	}
	if err = runSSH(client, "umask 077; cat > /etc/argus-otelcol/config.yaml", runtimeConfig); err != nil {
		return Result{}, err
	}
	serverCA, err := configbundle.ServerCA(command.GetRenderedConfig())
	if err != nil {
		return Result{}, err
	}
	if err = runSSH(client, "umask 077; cat > /etc/argus-otelcol/server-ca.pem", serverCA); err != nil {
		return Result{}, err
	}
	if len(command.GetEnrollmentToken()) > 0 {
		if err = runSSH(client, "umask 077; cat > /etc/argus-otelcol/enrollment-token", command.GetEnrollmentToken()); err != nil {
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
		"systemctl is-active --quiet argus-otelcol.service; sleep 2; systemctl is-active --quiet argus-otelcol.service"
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
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	if input != nil {
		session.Stdin = bytes.NewReader(input)
	}
	return session.Run(command)
}

func sshResult(command *connectorv1.CollectorManagementCommand, status string) Result {
	diagnostic := sha256.Sum256([]byte(strings.Join([]string{command.GetCollectorId(), status, command.GetConfigSha256()}, "\x00")))
	return Result{CollectorID: command.GetCollectorId(), EffectiveRevision: command.GetDesiredRevision(), ConfigSHA256: strings.ToLower(command.GetConfigSha256()),
		Status: status, DiagnosticHash: hex.EncodeToString(diagnostic[:])}
}
