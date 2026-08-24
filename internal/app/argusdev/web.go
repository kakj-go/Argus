package argusdev

import (
	"context"
	"flag"
	"fmt"
)

func (a *App) runWeb(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "build" {
		return fmt.Errorf("%w: usage: argus-dev web build --api-mode mock|real", errUsage)
	}
	flags := flag.NewFlagSet("web build", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	apiMode := flags.String("api-mode", "", "mock or real")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if flags.NArg() != 0 || !oneOf(*apiMode, "mock", "real") {
		return fmt.Errorf("%w: --api-mode must be mock or real", errUsage)
	}
	return a.runner.Run(ctx, nil, "node", "scripts/build-web.mjs", *apiMode)
}
