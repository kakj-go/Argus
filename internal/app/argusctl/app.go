// Package argusctl contains the installation CLI application.
package argusctl

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/kakj-go/Argus/internal/buildinfo"
)

const usage = `usage:
  argusctl version
  argusctl preflight --config FILE [--output text|json]
  argusctl plan --config FILE [--output text|json]
  argusctl images build|load|clean --config FILE [--platform linux/arm64]
  argusctl install --config FILE
  argusctl status --config FILE [--output text|json]
  argusctl verify --config FILE [--output text|json] [--artifacts DIR]
  argusctl tunnel --config FILE
  argusctl uninstall --config FILE --delete-data --delete-owned-crds --yes`

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "version" {
		_, _ = fmt.Fprintf(stdout, "argusctl %s (commit=%s, built=%s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
		return 0
	}
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, usage)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := App{stdout: stdout, stderr: stderr, runner: commandRunner{stdout: stdout, stderr: stderr}}
	if err := app.run(ctx, args); err != nil {
		_, _ = fmt.Fprintf(stderr, "argusctl: %v\n", err)
		return 1
	}
	return 0
}

type App struct {
	stdout io.Writer
	stderr io.Writer
	runner commandRunner
}

func (a *App) run(ctx context.Context, args []string) error {
	switch args[0] {
	case "preflight", "plan", "install", "status", "verify", "tunnel", "uninstall":
		return a.runConfigCommand(ctx, args[0], args[1:])
	case "images":
		if len(args) < 2 || !contains([]string{"build", "load", "clean"}, args[1]) {
			return errors.New("usage: argusctl images build|load|clean --config FILE")
		}
		return a.runImages(ctx, args[1], args[2:])
	default:
		return fmt.Errorf("unknown command %q\n%s", args[0], usage)
	}
}

func (a *App) runConfigCommand(ctx context.Context, command string, args []string) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	configPath := flags.String("config", "deploy/profiles/evaluation.yaml", "ArgusInstallConfig file")
	output := flags.String("output", "text", "text or json")
	artifacts := flags.String("artifacts", "", "diagnostic artifact directory")
	deleteData := flags.Bool("delete-data", false, "delete persistent data")
	deleteCRDs := flags.Bool("delete-owned-crds", false, "delete owned operator CRDs")
	yes := flags.Bool("yes", false, "confirm destructive cleanup")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *output != "text" && *output != "json" {
		return fmt.Errorf("unsupported output %q", *output)
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}

	switch command {
	case "preflight":
		return a.preflight(ctx, cfg, *output)
	case "plan":
		return a.plan(cfg, *output)
	case "install":
		return a.install(ctx, cfg)
	case "status":
		return a.status(ctx, cfg, *output)
	case "verify":
		return a.verify(ctx, cfg, *output, *artifacts)
	case "tunnel":
		return a.tunnel(ctx, cfg)
	case "uninstall":
		if !*yes || !*deleteData {
			return errors.New("uninstall requires --delete-data --yes; data deletion must be explicit")
		}
		return a.uninstall(ctx, cfg, *deleteCRDs)
	default:
		return fmt.Errorf("unsupported command %q", command)
	}
}

func (a *App) runImages(ctx context.Context, operation string, args []string) error {
	flags := flag.NewFlagSet("images "+operation, flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	configPath := flags.String("config", "deploy/profiles/evaluation.yaml", "ArgusInstallConfig file")
	platform := flags.String("platform", "linux/arm64", "build platform")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	switch operation {
	case "build":
		return a.imagesBuild(ctx, cfg, *platform)
	case "load":
		return a.imagesLoad(ctx, cfg)
	case "clean":
		return a.imagesClean(ctx, cfg)
	default:
		return fmt.Errorf("unsupported images operation %q", operation)
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
