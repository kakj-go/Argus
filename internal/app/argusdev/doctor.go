package argusdev

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const doctorProbeTimeout = 15 * time.Second

type DoctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type DoctorReport struct {
	Scope    string        `json:"scope"`
	OS       string        `json:"os"`
	Arch     string        `json:"arch"`
	Ready    bool          `json:"ready"`
	Checks   []DoctorCheck `json:"checks"`
	Warnings []string      `json:"warnings,omitempty"`
}

type doctorOptions struct {
	KubeContext string
	E2ESuite    string
}

func (a *App) runDoctor(ctx context.Context, args []string) error {
	if len(args) == 0 || !oneOf(args[0], "portable", "e2e", "release") {
		return fmt.Errorf("%w: usage: argus-dev doctor portable|e2e|release [--output text|json]", errUsage)
	}
	scope := args[0]
	flags := flag.NewFlagSet("doctor "+scope, flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	output := flags.String("output", "text", "text or json")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if flags.NArg() != 0 || !oneOf(*output, "text", "json") {
		return fmt.Errorf("%w: doctor accepts only --output text|json", errUsage)
	}
	report := a.doctor(ctx, scope)
	if err := writeDoctor(a.stdout, *output, report); err != nil {
		return err
	}
	if !report.Ready {
		return fmt.Errorf("%w: %s doctor found missing requirements", errCapability, scope)
	}
	return nil
}

func (a *App) doctor(ctx context.Context, scope string) DoctorReport {
	return a.doctorWithOptions(ctx, scope, doctorOptions{})
}

func (a *App) doctorWithOptions(ctx context.Context, scope string, options doctorOptions) DoctorReport {
	report := DoctorReport{Scope: scope, OS: runtime.GOOS, Arch: runtime.GOARCH, Ready: true}
	add := func(name, status, message string) {
		report.Checks = append(report.Checks, DoctorCheck{Name: name, Status: status, Message: message})
		if status == "fail" {
			report.Ready = false
		}
	}
	tools := []string{"go", "git", "node", "pnpm"}
	if scope == "e2e" {
		tools = append(tools, "docker", "kubectl")
	}
	if scope == "release" {
		tools = append(tools, "docker")
	}
	for _, tool := range tools {
		path, err := exec.LookPath(tool)
		if err != nil {
			add("tool/"+tool, "fail", "not found in PATH")
		} else {
			add("tool/"+tool, "pass", path)
		}
	}
	probePath := filepath.Join(a.root, ".argus-dev-write-probe")
	if err := os.WriteFile(probePath, []byte("probe"), 0o600); err != nil {
		add("workspace-write", "fail", err.Error())
	} else {
		_ = os.Remove(probePath)
		add("workspace-write", "pass", a.root)
	}
	if scope == "e2e" {
		if free, err := availableDiskBytes(a.root); err != nil {
			add("host-disk", "fail", err.Error())
		} else if free < 25*1024*1024*1024 {
			add("host-disk", "fail", fmt.Sprintf("only %.1fGi free; at least 25Gi is required for images and PVC backing", float64(free)/float64(1<<30)))
		} else {
			add("host-disk", "pass", fmt.Sprintf("%.1fGi free", float64(free)/float64(1<<30)))
		}
		if output, err := a.doctorOutput(ctx, "docker", "info", "--format", "{{.ServerVersion}}"); err != nil {
			add("docker-engine", "fail", err.Error())
		} else {
			add("docker-engine", "pass", output)
		}
		contextName := strings.TrimSpace(options.KubeContext)
		if contextName == "" {
			current, contextErr := a.doctorOutput(ctx, "kubectl", "config", "current-context")
			if contextErr != nil {
				add("kubernetes-context", "fail", contextErr.Error())
			} else {
				contextName = current
			}
		}
		if contextName != "" {
			add("kubernetes-context", "pass", contextName)
			kube, err := NewE2EKube(contextName, "")
			if err != nil {
				add("kubernetes-client", "fail", err.Error())
			} else {
				if architecture, err := nodeArchitectureWithTimeout(ctx, kube); err != nil {
					add("kubernetes-architecture", "fail", err.Error())
				} else if architecture != "amd64" && architecture != "arm64" {
					add("kubernetes-architecture", "fail", "unsupported node architecture "+architecture)
				} else if oneOf(options.E2ESuite, "m7", "m8", "m10-query") && architecture != "arm64" {
					add("kubernetes-architecture", "fail", options.E2ESuite+" requires arm64 for the locked Collector distribution")
				} else {
					add("kubernetes-architecture", "pass", architecture)
				}
				if conflicts, err := dedicatedClusterConflictsWithTimeout(ctx, kube); err != nil {
					add("kubernetes-dedicated-cluster", "fail", err.Error())
				} else if len(conflicts) != 0 {
					add("kubernetes-dedicated-cluster", "fail", "full E2E requires a dedicated cluster; conflicting resources: "+strings.Join(conflicts, ", "))
				} else {
					add("kubernetes-dedicated-cluster", "pass", "no conflicting Argus operator ownership found")
				}
			}
		}
		if output, err := a.doctorOutput(ctx, "docker", "buildx", "version"); err != nil {
			add("docker-buildx", "fail", err.Error())
		} else {
			add("docker-buildx", "pass", output)
		}
		storageArgs := kubectlContextArgs(contextName, "get", "storageclass", "--output=name")
		if output, err := a.doctorOutput(ctx, "kubectl", storageArgs...); err != nil || strings.TrimSpace(output) == "" {
			if err == nil {
				err = fmt.Errorf("no StorageClass found")
			}
			add("kubernetes-storage", "fail", err.Error())
		} else {
			add("kubernetes-storage", "pass", strings.ReplaceAll(output, "\n", ", "))
		}
	}
	if scope == "release" {
		if free, err := availableDiskBytes(a.root); err != nil {
			add("host-disk", "fail", err.Error())
		} else if free < 10*1024*1024*1024 {
			add("host-disk", "fail", fmt.Sprintf("only %.1fGi free; at least 10Gi is required for images, SBOMs, and archives", float64(free)/float64(1<<30)))
		} else {
			add("host-disk", "pass", fmt.Sprintf("%.1fGi free", float64(free)/float64(1<<30)))
		}
		if output, err := a.doctorOutput(ctx, "docker", "info", "--format", "{{.ServerVersion}}"); err != nil {
			add("docker-engine", "fail", err.Error())
		} else {
			add("docker-engine", "pass", output)
		}
		if help, err := a.doctorOutput(ctx, "docker", "sbom", "--help"); err == nil && strings.Contains(help, "spdx-json") {
			add("docker-sbom", "pass", "docker sbom supports spdx-json")
		} else if help, err := a.doctorOutput(ctx, "docker", "scout", "sbom", "--help"); err == nil && strings.Contains(help, "--format") {
			add("docker-sbom", "pass", "docker scout sbom supports SPDX output")
		} else {
			add("docker-sbom", "fail", "no SPDX-capable docker sbom or docker scout plugin")
		}
	}
	sort.Slice(report.Checks, func(i, j int) bool { return report.Checks[i].Name < report.Checks[j].Name })
	return report
}

func (a *App) doctorOutput(ctx context.Context, name string, args ...string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, doctorProbeTimeout)
	defer cancel()
	return a.runner.Output(probeCtx, nil, name, args...)
}

func nodeArchitectureWithTimeout(ctx context.Context, kube *E2EKube) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, doctorProbeTimeout)
	defer cancel()
	return kube.NodeArchitecture(probeCtx)
}

func dedicatedClusterConflictsWithTimeout(ctx context.Context, kube *E2EKube) ([]string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, doctorProbeTimeout)
	defer cancel()
	return kube.DedicatedClusterConflicts(probeCtx)
}

func kubectlContextArgs(contextName string, args ...string) []string {
	if strings.TrimSpace(contextName) == "" {
		return args
	}
	return append([]string{"--context", contextName}, args...)
}

func writeDoctor(w io.Writer, output string, report DoctorReport) error {
	if output == "json" {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	_, _ = fmt.Fprintf(w, "argus-dev doctor scope=%s ready=%t os=%s arch=%s\n", report.Scope, report.Ready, report.OS, report.Arch)
	for _, check := range report.Checks {
		_, _ = fmt.Fprintf(w, "[%s] %s: %s\n", strings.ToUpper(check.Status), check.Name, check.Message)
	}
	return nil
}

func oneOf(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}
