package argusdev

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

type Runner struct {
	Dir    string
	Stdout io.Writer
	Stderr io.Writer
}

type Process struct {
	cmd      *exec.Cmd
	done     chan error
	stopOnce sync.Once
	stopDone chan struct{}
	stopErr  error
}

func (r Runner) Start(env map[string]string, stdout, stderr io.Writer, name string, args ...string) (*Process, error) {
	cmd := r.command(context.Background(), env, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	process := &Process{cmd: cmd, done: make(chan error, 1), stopDone: make(chan struct{})}
	go func() { process.done <- cmd.Wait() }()
	return process, nil
}

func (p *Process) Stop(timeout time.Duration) error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	p.stopOnce.Do(func() {
		defer close(p.stopDone)
		_ = interruptProcess(p.cmd.Process)
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case p.stopErr = <-p.done:
		case <-timer.C:
			_ = killProcessTree(p.cmd.Process)
			p.stopErr = <-p.done
		}
	})
	<-p.stopDone
	return p.stopErr
}

func (p *Process) Done() <-chan error { return p.done }

func (r Runner) Run(ctx context.Context, env map[string]string, name string, args ...string) error {
	return r.RunIO(ctx, env, nil, r.Stdout, r.Stderr, name, args...)
}

func (r Runner) RunIO(ctx context.Context, env map[string]string, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := r.command(ctx, env, name, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := runCommand(ctx, cmd); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("%w: %s was not found in PATH", errCapability, name)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func (r Runner) Output(ctx context.Context, env map[string]string, name string, args ...string) (string, error) {
	cmd := r.command(ctx, env, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := runCommand(ctx, cmd); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("%w: %s was not found in PATH", errCapability, name)
		}
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, message)
		}
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (r Runner) command(ctx context.Context, env map[string]string, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = r.Dir
	cmd.Env = mergeEnv(os.Environ(), env)
	configureProcess(cmd)
	return cmd
}

func mergeEnv(base []string, overrides map[string]string) []string {
	values := make(map[string]string, len(base)+len(overrides))
	for _, item := range base {
		key, value, found := strings.Cut(item, "=")
		if found {
			values[strings.ToUpper(key)] = key + "=" + value
		}
	}
	for key, value := range overrides {
		values[strings.ToUpper(key)] = key + "=" + value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, values[key])
	}
	return result
}

func runCommand(ctx context.Context, cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = interruptProcess(cmd.Process)
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			_ = killProcessTree(cmd.Process)
			<-done
		}
		return ctx.Err()
	}
}
