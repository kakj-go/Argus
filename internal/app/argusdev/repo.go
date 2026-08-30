package argusdev

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
)

func (a *App) runRepo(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%w: usage: argus-dev repo fmt|test|vet|run-server|migrate|sqlc", errUsage)
	}
	switch args[0] {
	case "fmt":
		var files []string
		for _, root := range []string{"cmd", "internal"} {
			err := filepath.WalkDir(filepath.Join(a.root, root), func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if !entry.IsDir() && filepath.Ext(path) == ".go" {
					files = append(files, path)
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		sort.Strings(files)
		for _, file := range files {
			if err := a.runner.Run(ctx, nil, "gofmt", "-w", file); err != nil {
				return err
			}
		}
		return nil
	case "test":
		return a.runner.Run(ctx, nil, "go", "test", "./...")
	case "vet":
		return a.runner.Run(ctx, nil, "go", "vet", "-stdmethods=false", "./...")
	case "run-server":
		return a.runner.Run(ctx, nil, "go", "run", "./cmd/argus-server")
	case "migrate":
		return a.runner.Run(ctx, nil, "go", "run", "./cmd/argus-migrate", "up")
	case "sqlc":
		return a.runner.Run(ctx, nil, "go", "tool", "sqlc", "generate")
	default:
		return fmt.Errorf("%w: unsupported repo command %q", errUsage, args[0])
	}
}

func (a *App) runCollector(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%w: usage: argus-dev collector build linux-arm64|windows-amd64|all|publish-image|publish-artifacts", errUsage)
	}
	switch args[0] {
	case "build":
		if len(args) != 2 || !oneOf(args[1], "linux-arm64", "linux-amd64", "windows-amd64", "all") {
			return fmt.Errorf("%w: usage: argus-dev collector build linux-arm64|linux-amd64|windows-amd64|all", errUsage)
		}
		platforms := []string{args[1]}
		if args[1] == "all" {
			platforms = []string{"linux-arm64", "linux-amd64", "windows-amd64"}
		}
		for _, platform := range platforms {
			destination := filepath.Join(a.root, "build", "otelcol", "artifacts", "argus-otelcol-"+platform)
			if platform == "windows-amd64" {
				destination += ".zip"
			} else {
				destination += ".tar.gz"
			}
			if err := a.buildCollectorArtifact(ctx, platform, destination, true); err != nil {
				return err
			}
		}
		return nil
	case "publish-image":
		return a.publishCollectorImage(ctx, args[1:])
	case "publish-artifacts":
		return a.publishCollectorArtifacts(ctx, args[1:])
	default:
		return fmt.Errorf("%w: unsupported collector command %q", errUsage, args[0])
	}
}

func (a *App) runQuery(ctx context.Context, args []string) error {
	if len(args) != 1 || !oneOf(args[0], "promql", "kql", "skywalking", "tenant-schema") {
		return fmt.Errorf("%w: usage: argus-dev query promql|kql|skywalking|tenant-schema", errUsage)
	}
	commands := map[string][][]string{
		"promql":        {{"go", "test", "./internal/telemetry/queryengine/promql", "-count=1"}, {"go", "test", "./internal/telemetry", "-run", "TestPromQLClickHouse", "-count=1"}},
		"kql":           {{"go", "test", "./internal/telemetry/queryengine/kql", "-count=1"}},
		"skywalking":    {{"go", "test", "./internal/telemetry/queryengine/skywalking", "-count=1"}},
		"tenant-schema": {{"go", "test", "./internal/telemetry", "-run", "TestTenant|TestTelemetrySchemaV3", "-count=1"}},
	}
	for _, command := range commands[args[0]] {
		if err := a.runner.Run(ctx, nil, command[0], command[1:]...); err != nil {
			return err
		}
	}
	return nil
}
