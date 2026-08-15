package argusctl

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type commandRunner struct {
	stdout io.Writer
	stderr io.Writer
}

func (r commandRunner) run(ctx context.Context, stdin io.Reader, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = stdin
	var captured bytes.Buffer
	cmd.Stdout = io.MultiWriter(r.stdout, &captured)
	cmd.Stderr = r.stderr
	if err := cmd.Run(); err != nil {
		return captured.String(), fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return captured.String(), nil
}

func (r commandRunner) quiet(ctx context.Context, name string, args ...string) (string, error) {
	return r.quietInput(ctx, nil, name, args...)
}

func (r commandRunner) quietInput(ctx context.Context, stdin io.Reader, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = stdin
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return output.String(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(output.String()))
	}
	return output.String(), nil
}
