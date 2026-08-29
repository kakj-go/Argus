package argusdev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var contractDomains = []string{
	"common", "identity", "authorization", "labels", "action", "card", "agent", "stream", "setup", "m8api", "platform",
	"enterpriseidentity", "enterpriseauthz", "machine", "audit", "secretapi", "hostapi", "kubernetesapi", "connectionapi",
	"actionapi", "connectorapi", "conversationapi", "modelapi", "workflowapi", "sandboxapi", "cardapi",
	"remoteaccessapi", "telemetryapi",
}

var contractServerDomains = []string{
	"setup", "m8api", "platform", "enterpriseidentity", "enterpriseauthz", "machine", "audit", "secretapi", "hostapi",
	"kubernetesapi", "connectionapi", "actionapi", "connectorapi", "conversationapi", "modelapi", "workflowapi",
	"sandboxapi", "cardapi", "remoteaccessapi", "telemetryapi",
}

var splitServerDomains = []string{
	"enterpriseauthz", "secretapi", "hostapi", "kubernetesapi", "connectionapi", "actionapi", "connectorapi", "sandboxapi",
	"cardapi", "remoteaccessapi", "telemetryapi",
}

func (a *App) runContracts(ctx context.Context, args []string) error {
	if len(args) != 1 || !oneOf(args[0], "lint", "generate", "check", "breaking") {
		return fmt.Errorf("%w: usage: argus-dev contracts lint|generate|check|breaking", errUsage)
	}
	switch args[0] {
	case "lint":
		return a.contractLint(ctx)
	case "generate":
		return a.contractGenerate(ctx)
	case "check":
		return a.contractCheck(ctx)
	case "breaking":
		return a.contractBreaking(ctx)
	default:
		return fmt.Errorf("%w: unsupported contracts operation", errUsage)
	}
}

func (a *App) contractLint(ctx context.Context) error {
	// Expand the files ourselves because exec.Cmd never invokes a shell and
	// therefore behaves identically on Unix and Windows.
	files, err := filepath.Glob(filepath.Join(a.root, "api", "openapi", "generation", "*.yaml"))
	if err != nil {
		return err
	}
	arguments := []string{"exec", "redocly", "lint", "api/openapi/argus.yaml"}
	for _, file := range files {
		relative, relErr := filepath.Rel(a.root, file)
		if relErr != nil {
			return relErr
		}
		arguments = append(arguments, filepath.ToSlash(relative))
	}
	if err := a.runner.Run(ctx, nil, "pnpm", arguments...); err != nil {
		return err
	}
	if err := a.runner.Run(ctx, nil, "go", "tool", "buf", "lint", "api/proto"); err != nil {
		return err
	}
	return a.runner.Run(ctx, nil, "go", "test", "./tests/contract", "-skip", "^TestContractCompatibility$")
}

func (a *App) contractGenerate(ctx context.Context) error {
	generatedOpenAPI := filepath.Join(a.root, "internal", "gen", "openapi")
	generatedWeb := filepath.Join(a.root, "web", "packages", "api-client", "src", "generated")
	generatedBundles := filepath.Join(a.root, "api", "openapi", "generated")
	for _, path := range []string{generatedOpenAPI, generatedWeb} {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		if err := os.MkdirAll(path, 0o755); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(generatedBundles, 0o755); err != nil {
		return err
	}
	passwordPolicyDirectory := filepath.Join(generatedOpenAPI, "passwordpolicy")
	if err := os.MkdirAll(passwordPolicyDirectory, 0o755); err != nil {
		return err
	}
	if err := a.runner.Run(ctx, nil, "node", "scripts/generate-password-policy.mjs",
		"api/contracts/password-policy.json",
		"internal/gen/openapi/passwordpolicy/policy.gen.go",
		"web/packages/api-client/src/generated/password-policy.ts"); err != nil {
		return err
	}
	if err := a.runner.Run(ctx, nil, "gofmt", "-w", "internal/gen/openapi/passwordpolicy/policy.gen.go"); err != nil {
		return err
	}
	matches, err := filepath.Glob(filepath.Join(generatedBundles, "*.bundle.*"))
	if err != nil {
		return err
	}
	for _, match := range matches {
		if err := os.Remove(match); err != nil {
			return err
		}
	}
	if err := a.runner.Run(ctx, nil, "pnpm", "exec", "redocly", "bundle", "api/openapi/argus.yaml", "--output", "api/openapi/generated/argus.bundle.json", "--ext", "json"); err != nil {
		return err
	}
	if err := a.runner.Run(ctx, nil, "node", "scripts/minify-json.mjs", "api/openapi/generated/argus.bundle.json"); err != nil {
		return err
	}
	if err := a.runner.Run(ctx, nil, "node", "scripts/generate-form-constraints.mjs",
		"api/openapi/generated/argus.bundle.json",
		"web/packages/api-client/src/generated/form-constraints.ts"); err != nil {
		return err
	}
	for _, domain := range contractDomains {
		bundle := "api/openapi/generated/" + domain + ".bundle.yaml"
		if err := a.runner.Run(ctx, nil, "pnpm", "exec", "redocly", "bundle", "api/openapi/generation/"+domain+".yaml", "--output", bundle, "--ext", "yaml"); err != nil {
			return err
		}
		directory := filepath.Join(generatedOpenAPI, domain)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
		if err := a.runner.Run(ctx, nil, "go", "tool", "oapi-codegen", "-generate", "types,skip-prune", "-package", domain, "-o", "internal/gen/openapi/"+domain+"/types.gen.go", bundle); err != nil {
			return err
		}
		if err := a.runner.Run(ctx, nil, "node", "scripts/split-generated-go-types.mjs", "internal/gen/openapi/"+domain+"/types.gen.go"); err != nil {
			return err
		}
		if err := a.runner.Run(ctx, nil, "node", "scripts/generate-openapi-types.mjs", bundle, "web/packages/api-client/src/generated/"+domain+".ts"); err != nil {
			return err
		}
		if err := a.runner.Run(ctx, nil, "node", "scripts/split-generated-openapi-types.mjs", "web/packages/api-client/src/generated/"+domain+".ts"); err != nil {
			return err
		}
	}
	for _, domain := range contractServerDomains {
		if err := a.runner.Run(ctx, nil, "go", "tool", "oapi-codegen", "-generate", "chi-server,strict-server", "-package", domain, "-o", "internal/gen/openapi/"+domain+"/server.gen.go", "api/openapi/generated/"+domain+".bundle.yaml"); err != nil {
			return err
		}
	}
	for _, domain := range splitServerDomains {
		if err := a.runner.Run(ctx, nil, "node", "scripts/split-generated-go-server.mjs", "internal/gen/openapi/"+domain+"/server.gen.go"); err != nil {
			return err
		}
	}
	indexArgs := []string{"scripts/generate-contract-index.mjs", "web/packages/api-client/src/generated/contracts.ts"}
	indexArgs = append(indexArgs, contractDomains...)
	if err := a.runner.Run(ctx, nil, "node", indexArgs...); err != nil {
		return err
	}
	return a.runner.Run(ctx, nil, "go", "tool", "buf", "generate", "api/proto", "--template", "api/proto/buf.gen.yaml")
}

func (a *App) contractCheck(ctx context.Context) error {
	if err := a.contractLint(ctx); err != nil {
		return err
	}
	roots := []string{"api/openapi/generated", "internal/gen", "web/packages/api-client/src/generated"}
	before, err := hashTrees(a.root, roots)
	if err != nil {
		return err
	}
	if err := a.contractGenerate(ctx); err != nil {
		return err
	}
	after, err := hashTrees(a.root, roots)
	if err != nil {
		return err
	}
	if difference := hashDifference(before, after); difference != "" {
		return fmt.Errorf("generated contracts are stale:\n%s", difference)
	}
	_, _ = fmt.Fprintln(a.stdout, "generated contracts are current")
	return nil
}

func (a *App) contractBreaking(ctx context.Context) error {
	if err := a.runner.Run(ctx, nil, "go", "test", "./tests/contract", "-run", "TestContractCompatibility"); err != nil {
		return err
	}
	if _, err := a.runner.Output(ctx, nil, "git", "cat-file", "-e", "origin/main:api/proto/buf.yaml"); err != nil {
		_, _ = fmt.Fprintln(a.stdout, "origin/main has no protobuf baseline; this merge establishes it")
		return nil
	}
	return a.runner.Run(ctx, nil, "go", "tool", "buf", "breaking", "api/proto", "--against", ".git#branch=origin/main,subdir=api/proto")
}

func hashTrees(root string, paths []string) (map[string]string, error) {
	result := map[string]string{}
	for _, relativeRoot := range paths {
		absoluteRoot := filepath.Join(root, filepath.FromSlash(relativeRoot))
		err := filepath.WalkDir(absoluteRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			hash := sha256.New()
			if _, err := io.Copy(hash, file); err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			result[filepath.ToSlash(relative)] = hex.EncodeToString(hash.Sum(nil))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func hashDifference(before, after map[string]string) string {
	keys := make(map[string]struct{}, len(before)+len(after))
	for key := range before {
		keys[key] = struct{}{}
	}
	for key := range after {
		keys[key] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	var changes []string
	for _, key := range ordered {
		switch {
		case before[key] == "":
			changes = append(changes, "+ "+key)
		case after[key] == "":
			changes = append(changes, "- "+key)
		case before[key] != after[key]:
			changes = append(changes, "~ "+key)
		}
	}
	return strings.Join(changes, "\n")
}
