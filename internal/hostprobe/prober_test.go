package hostprobe

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestScaleConcurrency(t *testing.T) {
	cases := []struct {
		current int
		factor  float64
		want    int
	}{
		{8, 1.5, 12},
		{4, 1.5, concurrencyFloor},
		{512, 1.5, concurrencyCap},
		{100, 0.75, 75},
		{8, 0.5, concurrencyFloor},
	}
	for _, item := range cases {
		if got := scaleConcurrency(item.current, item.factor); got != item.want {
			t.Errorf("scaleConcurrency(%d, %v) = %d, want %d", item.current, item.factor, got, item.want)
		}
	}
}

// startTestSSHServer 起一个仅完成密钥交换、拒绝认证的 SSH 服务端,
// 足以让 probeOne 在握手阶段捕获主机键指纹。
func startTestSSHServer(t *testing.T) (string, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, _ []byte) (*ssh.Permissions, error) {
		return nil, errors.New("probe server rejects auth")
	}}
	config.AddHostKey(signer)
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				serverConnection, _, _, serveErr := ssh.NewServerConn(connection, config)
				if serveErr == nil {
					_ = serverConnection.Close()
				}
				_ = connection.Close()
			}()
		}
	}()
	return listener.Addr().String(), ssh.FingerprintSHA256(signer.PublicKey())
}

func TestProbeOne(t *testing.T) {
	address, fingerprint := startTestSSHServer(t)
	host, port, _ := net.SplitHostPort(address)
	portNumber := 0
	_, _ = fmt.Sscanf(port, "%d", &portNumber)

	online := probeOne(t.Context(), probeTarget{HostID: "h1", Address: host, Port: int32(portNumber), PinnedHostKey: fingerprint})
	if online.Status != StatusOnline || online.Fingerprint != fingerprint || !online.Reached {
		t.Fatalf("expected online with matching key, got %+v", online)
	}

	changed := probeOne(t.Context(), probeTarget{HostID: "h2", Address: host, Port: int32(portNumber), PinnedHostKey: "SHA256:mismatch"})
	if changed.Status != StatusKeyChanged {
		t.Fatalf("expected key_changed, got %+v", changed)
	}

	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	offlineAddress := closed.Addr().String()
	_ = closed.Close()
	offlineHost, offlinePort, _ := net.SplitHostPort(offlineAddress)
	offlinePortNumber := 0
	_, _ = fmt.Sscanf(offlinePort, "%d", &offlinePortNumber)
	offline := probeOne(t.Context(), probeTarget{HostID: "h3", Address: offlineHost, Port: int32(offlinePortNumber)})
	if offline.Status != StatusOffline || offline.Error == "" {
		t.Fatalf("expected offline with error, got %+v", offline)
	}
}
