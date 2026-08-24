package argusdev

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) runE2EScenarios(ctx context.Context, env *E2EEnvironment) error {
	if err := a.runM2Scenario(ctx, env); err != nil {
		return err
	}
	if env.Options.Suite == "m8" {
		return a.runM8Scenario(ctx, env)
	}
	for _, phase := range suiteDependencies[env.Options.Suite] {
		var err error
		switch phase {
		case "m2":
			continue
		case "m3":
			err = a.runM3Scenario(ctx, env)
		case "m4":
			err = a.runM4Scenario(ctx, env)
		case "m5":
			err = a.runM5Scenario(ctx, env)
		case "m6":
			err = a.runM6Scenario(ctx, env)
		case "m7":
			err = a.runM7Scenario(ctx, env)
		case "m10-query":
			err = a.runM10QueryScenario(ctx, env)
		default:
			err = fmt.Errorf("unsupported E2E dependency %q", phase)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
func (a *App) runPlaywright(ctx context.Context, env *E2EEnvironment, spec string, variables map[string]string) error {
	variables["ARGUS_E2E_EXTERNAL"] = "1"
	artifactDir := filepath.Join(env.Options.Artifacts, "playwright-"+env.Options.Suite)
	variables["ARGUS_E2E_ARTIFACTS"] = artifactDir
	variables["ARGUS_E2E_ENTERPRISE_ORIGIN"] = "http://127.0.0.1:4173"
	variables["ARGUS_E2E_PLATFORM_ORIGIN"] = "http://127.0.0.1:4174"
	variables["ARGUS_E2E_CARD_ORIGIN"] = "http://127.0.0.1:4176"
	variables["ARGUS_E2E_ENTERPRISE_TOTP_SECRET"] = env.State.Values["enterprise_mfa_secret"]
	variables["ARGUS_E2E_ENTERPRISE_TOTP_LAST_CODE"] = env.State.Values["enterprise_mfa_last"]
	variables["ARGUS_E2E_PLATFORM_TOTP_SECRET"] = env.State.Values["platform_mfa_secret"]
	variables["ARGUS_E2E_PLATFORM_TOTP_LAST_CODE"] = env.State.Values["platform_mfa_last"]
	if err := a.runner.Run(ctx, variables, "pnpm", "--filter", "@argus/enterprise", "exec", "playwright", "test", spec, "--workers=1"); err != nil {
		return err
	}
	return syncPlaywrightMFAState(env, artifactDir)
}

func syncPlaywrightMFAState(env *E2EEnvironment, artifactDir string) error {
	for audience, stateKey := range map[string]string{
		"enterprise": "enterprise_mfa_last",
		"platform":   "platform_mfa_last",
	} {
		path := filepath.Join(artifactDir, ".argus-"+audience+"-totp-last-code")
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read Playwright %s MFA state: %w", audience, err)
		}
		if code := strings.TrimSpace(string(data)); code != "" {
			env.State.Values[stateKey] = code
		}
	}
	return nil
}

func waitForNextTOTP(secret, previous string) (string, error) {
	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		now := time.Now()
		code, err := generateTOTP(secret, now)
		if err != nil {
			return "", err
		}
		secondsRemaining := 30 - now.Unix()%30
		if code != previous && secondsRemaining >= 5 {
			return code, nil
		}
		time.Sleep(time.Second)
	}
	return "", fmt.Errorf("TOTP period did not advance")
}
