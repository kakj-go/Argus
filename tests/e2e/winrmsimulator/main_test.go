package main

import (
	"context"
	"encoding/pem"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kakj-go/Argus/internal/remoteaccess/winrsline"
)

func TestTLSWinRSLineProtocol(t *testing.T) {
	t.Parallel()
	handler := &simulator{username: "argus", password: "test-password"}
	server := httptest.NewTLSServer(handler)
	defer server.Close()

	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := winrsline.Open(winrsline.Options{
		Host: "example.com", Port: 5986, Username: "argus", Password: "test-password", CACert: certificate,
		Dial: func(_, _ string) (net.Conn, error) {
			return net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
		},
	})
	if err != nil {
		t.Fatalf("open TLS WinRS simulator: %v", err)
	}
	defer client.Close()

	result, err := client.ExecuteLine(context.Background(), "whoami")
	if err != nil {
		t.Fatalf("execute whoami: %v", err)
	}
	if string(result.Stdout) != "argus\\m6-e2e\r\n" || len(result.Stderr) != 0 || result.ExitCode != 0 {
		t.Fatalf("unexpected success result: %#v", result)
	}

	result, err = client.ExecuteLine(context.Background(), "Write-Error m6-fail")
	if err != nil {
		t.Fatalf("execute failing PowerShell line: %v", err)
	}
	if !strings.Contains(string(result.Stderr), "m6-winrs-command-failed") || result.ExitCode != 1 {
		t.Fatalf("unexpected failure result: %#v", result)
	}
	if !strings.Contains(handler.lastCommand(), "powershell.exe") || !strings.Contains(handler.lastCommand(), "Write-Error m6-fail") {
		t.Fatalf("simulator did not receive the PowerShell line: %s", handler.lastCommand())
	}
}

func TestWinRSRejectsPlaintextAndUntrustedTLS(t *testing.T) {
	t.Parallel()
	if _, err := winrsline.Open(winrsline.Options{Host: "example.com", Port: 5985, Username: "argus", Password: "test"}); err != winrsline.ErrTLSRequired {
		t.Fatalf("expected WINRM_TLS_REQUIRED, got %v", err)
	}

	server := httptest.NewTLSServer(&simulator{username: "argus", password: "test"})
	defer server.Close()
	_, err := winrsline.Open(winrsline.Options{
		Host: "example.com", Port: 5986, Username: "argus", Password: "test",
		Dial: func(_, _ string) (net.Conn, error) { return net.Dial("tcp", server.Listener.Addr().String()) },
	})
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("expected an untrusted TLS certificate error, got %v", err)
	}
}

func (server *simulator) lastCommand() string {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.last
}
