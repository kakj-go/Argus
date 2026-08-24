package argusdev

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"testing"
	"time"
)

func TestMergeEnvOverridesCaseInsensitively(t *testing.T) {
	values := mergeEnv([]string{"PATH=/first", "HOME=/home"}, map[string]string{"Path": "/second", "NEW": "value"})
	joined := strings.Join(values, "\n")
	if strings.Contains(joined, "PATH=/first") || !strings.Contains(joined, "Path=/second") || !strings.Contains(joined, "NEW=value") {
		t.Fatalf("unexpected environment:\n%s", joined)
	}
}

func TestOneOf(t *testing.T) {
	if !oneOf("m7", "m2", "m7") || oneOf("m8", "m2", "m7") {
		t.Fatal("oneOf returned an unexpected result")
	}
}

func TestRunnerExecutesCommand(t *testing.T) {
	runner := Runner{Dir: t.TempDir(), Stdout: io.Discard, Stderr: io.Discard}
	if err := runner.Run(context.Background(), map[string]string{"ARGUS_DEV_HELPER_MODE": "exit"}, os.Args[0], "-test.run=TestRunnerHelperProcess"); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerCancellationTerminatesProcess(t *testing.T) {
	runner := Runner{Dir: t.TempDir(), Stdout: io.Discard, Stderr: io.Discard}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := runner.Run(ctx, map[string]string{"ARGUS_DEV_HELPER_MODE": "wait"}, os.Args[0], "-test.run=TestRunnerHelperProcess")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Runner.Run() error = %v, want deadline exceeded", err)
	}
}

func TestProcessStopIsIdempotent(t *testing.T) {
	runner := Runner{Dir: t.TempDir(), Stdout: io.Discard, Stderr: io.Discard}
	process, err := runner.Start(map[string]string{"ARGUS_DEV_HELPER_MODE": "wait"}, io.Discard, io.Discard, os.Args[0], "-test.run=TestRunnerHelperProcess")
	if err != nil {
		t.Fatal(err)
	}
	first := process.Stop(2 * time.Second)
	started := time.Now()
	second := process.Stop(2 * time.Second)
	if time.Since(started) > 250*time.Millisecond {
		t.Fatal("second Process.Stop call did not return cached result")
	}
	if fmt.Sprint(first) != fmt.Sprint(second) {
		t.Fatalf("Process.Stop results differ: %v and %v", first, second)
	}
}

func TestRunnerMissingToolIsCapabilityError(t *testing.T) {
	runner := Runner{Dir: t.TempDir(), Stdout: io.Discard, Stderr: io.Discard}
	err := runner.Run(context.Background(), nil, "argus-dev-command-that-does-not-exist")
	if !errors.Is(err, errCapability) {
		t.Fatalf("Runner.Run() error = %v, want capability error", err)
	}
}

func TestKubectlContextArgs(t *testing.T) {
	got := kubectlContextArgs("cluster-b", "get", "storageclass")
	want := []string{"--context", "cluster-b", "get", "storageclass"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("kubectlContextArgs() = %#v, want %#v", got, want)
	}
}

func TestRunnerHelperProcess(t *testing.T) {
	switch os.Getenv("ARGUS_DEV_HELPER_MODE") {
	case "exit":
		return
	case "wait":
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		defer signal.Stop(signals)
		<-signals
		os.Exit(0)
	}
}
