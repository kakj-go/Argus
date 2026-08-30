// Package argusdev contains the cross-platform repository development CLI.
package argusdev

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

const usage = `usage:
  argus-dev doctor portable|e2e|release [--output text|json]
  argus-dev contracts lint|generate|check|breaking
  argus-dev check query-parsers|production-artifacts|web-entrypoints|all
  argus-dev repo fmt|test|vet|run-server|migrate|sqlc
  argus-dev collector build linux-arm64|windows-amd64|all
  argus-dev collector publish-image [--repository REPO] [--tag TAG] [--push]  # --push 发布 arm64+amd64 多架构 manifest
  argus-dev collector publish-artifacts --endpoint URL --access-key K --secret-key K [--bucket B] [--public-base URL] [--key-id ID] [--windows]
  argus-dev query promql|kql|skywalking|tenant-schema
  argus-dev web build --api-mode mock|real
  argus-dev e2e run --suite m2|m3|m4|m5|m6|m7|m8|m10-query [options]
  argus-dev release local [--version VERSION] [--output DIR]`

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, usage)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root, err := findRepoRoot("")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "argus-dev: %v\n", err)
		return 2
	}
	app := App{
		root:   root,
		stdout: stdout,
		stderr: stderr,
		runner: Runner{Dir: root, Stdout: stdout, Stderr: stderr},
	}
	if err := app.run(ctx, args); err != nil {
		_, _ = fmt.Fprintf(stderr, "argus-dev: %v\n", err)
		if errors.Is(err, errUsage) || errors.Is(err, errCapability) {
			return 2
		}
		return 1
	}
	return 0
}

var (
	errUsage      = errors.New("invalid command usage")
	errCapability = errors.New("required capability unavailable")
)

type App struct {
	root   string
	stdout io.Writer
	stderr io.Writer
	runner Runner
}

func (a *App) run(ctx context.Context, args []string) error {
	switch args[0] {
	case "doctor":
		return a.runDoctor(ctx, args[1:])
	case "contracts":
		return a.runContracts(ctx, args[1:])
	case "check":
		return a.runCheck(ctx, args[1:])
	case "repo":
		return a.runRepo(ctx, args[1:])
	case "collector":
		return a.runCollector(ctx, args[1:])
	case "query":
		return a.runQuery(ctx, args[1:])
	case "web":
		return a.runWeb(ctx, args[1:])
	case "e2e":
		return a.runE2E(ctx, args[1:])
	case "release":
		return a.runRelease(ctx, args[1:])
	default:
		return fmt.Errorf("%w: unknown command %q\n%s", errUsage, args[0], usage)
	}
}
