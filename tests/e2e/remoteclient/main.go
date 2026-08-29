package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const protocol = "argus.remote_access/v1"

type frame struct {
	Protocol string `json:"protocol"`
	Type     string `json:"type"`
	Sequence uint64 `json:"sequence"`
	Ticket   string `json:"ticket,omitempty"`
	Nonce    string `json:"nonce,omitempty"`
	Cols     int    `json:"cols,omitempty"`
	Rows     int    `json:"rows,omitempty"`
	Data     string `json:"data,omitempty"`
	Stream   string `json:"stream,omitempty"`
	Status   string `json:"status,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

func main() {
	url := flag.String("url", "", "remote access WebSocket URL")
	origin := flag.String("origin", "", "enterprise UI Origin")
	command := flag.String("command", "echo argus-e2e-ok", "command to execute")
	expect := flag.String("expect", "argus-e2e-ok", "expected output")
	expectStatus := flag.String("expect-status", "", "expected terminal status")
	expectReason := flag.String("expect-reason", "", "expected terminal reason")
	hold := flag.Duration("hold", 0, "keep the session open after observing expected output")
	timeout := flag.Duration("timeout", 30*time.Second, "overall timeout")
	flag.Parse()
	ticket, err := bufio.NewReader(io.LimitReader(os.Stdin, 4096)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		fatal(err)
	}
	ticket = strings.TrimSpace(ticket)
	if *url == "" || *origin == "" || len(ticket) < 43 {
		fatal(errors.New("URL, Origin, and ticket on stdin are required"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, *url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{*origin}},
		HTTPClient: remoteHTTPClient(),
	})
	if err != nil {
		fatal(err)
	}
	defer connection.CloseNow()
	connection.SetReadLimit(64 << 10)
	nonce := fmt.Sprintf("%032x", time.Now().UnixNano())
	if err := write(ctx, connection, frame{Protocol: protocol, Type: "client_hello", Sequence: 1, Ticket: ticket, Nonce: nonce, Cols: 100, Rows: 30}); err != nil {
		fatal(err)
	}
	ticket = ""
	ready, err := read(ctx, connection)
	if err != nil || ready.Type != "server_ready" || ready.Sequence != 1 || ready.Nonce != nonce {
		fatal(fmt.Errorf("remote access server_ready validation failed: read_error=%v type=%s sequence=%d status=%s reason=%s", err, ready.Type, ready.Sequence, ready.Status, ready.Reason))
	}
	if err := write(ctx, connection, frame{Protocol: protocol, Type: "resize", Sequence: 2, Cols: 120, Rows: 40}); err != nil {
		fatal(err)
	}
	if err := write(ctx, connection, frame{Protocol: protocol, Type: "input", Sequence: 3, Data: *command + "\r\n"}); err != nil {
		fatal(err)
	}
	for {
		value, err := read(ctx, connection)
		if err != nil {
			fatal(err)
		}
		if *expectStatus == "" && value.Type == "output" && strings.Contains(value.Data, *expect) {
			break
		}
		if *expectStatus != "" && value.Type == "state" && value.Status == *expectStatus && (*expectReason == "" || value.Reason == *expectReason) {
			fmt.Println("remote access terminal state observed")
			return
		}
		if value.Type == "state" && value.Status != "active" && value.Status != "connecting" && value.Status != "terminating" {
			fatal(fmt.Errorf("session ended before expected output: %s %s", value.Status, value.Reason))
		}
	}
	if *hold > 0 {
		select {
		case <-ctx.Done():
			fatal(ctx.Err())
		case <-time.After(*hold):
		}
	}
	_ = write(ctx, connection, frame{Protocol: protocol, Type: "close", Sequence: 4, Reason: "e2e_complete"})
	_ = connection.Close(websocket.StatusNormalClosure, "e2e_complete")
	fmt.Println("remote access command completed")
}

func write(ctx context.Context, connection *websocket.Conn, value frame) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return connection.Write(ctx, websocket.MessageText, raw)
}

func read(ctx context.Context, connection *websocket.Conn) (frame, error) {
	messageType, raw, err := connection.Read(ctx)
	if err != nil {
		return frame{}, err
	}
	if messageType != websocket.MessageText || len(raw) > 64<<10 {
		return frame{}, errors.New("invalid remote access frame")
	}
	var value frame
	if json.Unmarshal(raw, &value) != nil || value.Protocol != protocol {
		return frame{}, errors.New("invalid remote access JSON")
	}
	return value, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

// remoteHTTPClient honors two E2E-only environment variables:
// ARGUS_E2E_CA_FILE (PEM bundle trusted instead of system roots) and
// ARGUS_E2E_HOST_MAP ("host=ip,host=ip" pairs that pin public hostnames to
// load-balancer addresses while TLS ServerName stays on the hostname).
func remoteHTTPClient() *http.Client {
	pool := x509.NewCertPool()
	if caFile := os.Getenv("ARGUS_E2E_CA_FILE"); caFile != "" {
		caPEM, err := os.ReadFile(caFile)
		if err != nil || !pool.AppendCertsFromPEM(caPEM) {
			fatal(errors.New("ARGUS_E2E_CA_FILE does not contain a valid CA bundle"))
		}
	}
	pinned := map[string]string{}
	for _, pair := range strings.Split(os.Getenv("ARGUS_E2E_HOST_MAP"), ",") {
		if host, ip, found := strings.Cut(strings.TrimSpace(pair), "="); found && host != "" && ip != "" {
			pinned[host] = ip
		}
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if host, port, err := net.SplitHostPort(addr); err == nil {
				if ip, mapped := pinned[host]; mapped {
					addr = net.JoinHostPort(ip, port)
				}
			}
			return dialer.DialContext(ctx, network, addr)
		},
		TLSClientConfig: &tls.Config{RootCAs: pool},
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}
}
