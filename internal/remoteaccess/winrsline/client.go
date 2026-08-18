package winrsline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/masterzen/winrm"
)

const MaxOutputBytes = 64 << 10

var (
	ErrTLSRequired    = errors.New("WINRM_TLS_REQUIRED")
	ErrOutputTooLarge = errors.New("WinRS output too large")
)

type Options struct {
	Host     string
	Port     int
	Username string
	Password string
	CACert   []byte
	Dial     func(network, address string) (net.Conn, error)
	Timeout  time.Duration
}

type Client struct {
	shell *winrm.Shell
}

type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

func Open(options Options) (*Client, error) {
	if options.Port != 443 && options.Port != 5986 {
		return nil, ErrTLSRequired
	}
	if options.Host == "" || options.Username == "" {
		return nil, errors.New("WinRS host and username are required")
	}
	if options.Timeout <= 0 {
		options.Timeout = 30 * time.Second
	}
	endpoint := winrm.NewEndpoint(options.Host, options.Port, true, false, options.CACert, nil, nil, options.Timeout)
	endpoint.TLSServerName = options.Host
	parameters := *winrm.DefaultParameters
	parameters.Dial = options.Dial
	client, err := winrm.NewClientWithParameters(endpoint, options.Username, options.Password, &parameters)
	if err != nil {
		return nil, fmt.Errorf("create WinRS client: %w", err)
	}
	shell, err := client.CreateShell()
	if err != nil {
		return nil, fmt.Errorf("create WinRS shell: %w", err)
	}
	return &Client{shell: shell}, nil
}

func (client *Client) ExecuteLine(ctx context.Context, line string) (Result, error) {
	command, err := client.shell.ExecuteWithContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", line)
	if err != nil {
		return Result{}, err
	}
	defer command.Close()

	type readResult struct {
		value []byte
		err   error
	}
	read := func(reader io.Reader, output chan<- readResult) {
		value, readErr := io.ReadAll(io.LimitReader(reader, MaxOutputBytes+1))
		if len(value) > MaxOutputBytes {
			readErr = ErrOutputTooLarge
		}
		output <- readResult{value: value, err: readErr}
	}
	stdoutResult := make(chan readResult, 1)
	stderrResult := make(chan readResult, 1)
	go read(command.Stdout, stdoutResult)
	go read(command.Stderr, stderrResult)
	command.Wait()
	stdout := <-stdoutResult
	stderr := <-stderrResult
	if stdout.err != nil {
		return Result{}, stdout.err
	}
	if stderr.err != nil {
		return Result{}, stderr.err
	}
	return Result{Stdout: stdout.value, Stderr: stderr.value, ExitCode: command.ExitCode()}, command.Error()
}

func (client *Client) Close() error {
	if client == nil || client.shell == nil {
		return nil
	}
	return client.shell.Close()
}
