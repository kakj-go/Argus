package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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
	connection, _, err := websocket.Dial(ctx, *url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{*origin}}})
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
