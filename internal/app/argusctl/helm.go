package argusctl

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"helm.sh/helm/v4/pkg/action"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/kube"
)

const (
	certManagerVersion  = "1.20.3"
	trustManagerVersion = "0.24.0"
	strimziChartURL     = "https://github.com/strimzi/strimzi-kafka-operator/releases/download/1.1.0/strimzi-kafka-operator-helm-3-chart-1.1.0.tgz"
	altinityChartURL    = "https://github.com/Altinity/clickhouse-operator/releases/download/release-0.27.3/altinity-clickhouse-operator-0.27.3.tgz"
	openSandboxURL      = "https://github.com/opensandbox-group/OpenSandbox/archive/refs/tags/helm/opensandbox/0.2.0.tar.gz"
	certManagerURL      = "https://charts.jetstack.io/charts/cert-manager-v" + certManagerVersion + ".tgz"
	trustManagerURL     = "https://charts.jetstack.io/charts/trust-manager-v" + trustManagerVersion + ".tgz"
)

type helmManager struct {
	contextName string
	cacheDir    string
	log         io.Writer
}

func (h helmManager) configuration(namespace string) (*action.Configuration, error) {
	settings := cli.New()
	settings.KubeContext = h.contextName
	settings.SetNamespace(namespace)
	configuration := action.NewConfiguration()
	configuration.SetLogger(slog.NewTextHandler(h.log, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := configuration.Init(settings.RESTClientGetter(), namespace, "secret"); err != nil {
		return nil, fmt.Errorf("initialize Helm for namespace %s: %w", namespace, err)
	}
	return configuration, nil
}

func (h helmManager) installOrUpgrade(ctx context.Context, releaseName, namespace string, ch *chart.Chart, values map[string]any) error {
	configuration, err := h.configuration(namespace)
	if err != nil {
		return err
	}
	if _, err := action.NewStatus(configuration).Run(releaseName); err == nil {
		upgrade := action.NewUpgrade(configuration)
		upgrade.Namespace = namespace
		upgrade.Timeout = 15 * time.Minute
		upgrade.WaitStrategy = kube.StatusWatcherStrategy
		upgrade.WaitForJobs = true
		upgrade.CleanupOnFail = true
		// Argus rotation and runtime controllers intentionally update a small
		// set of Helm-owned fields. The values passed to this upgrade have first
		// preserved that live PKI state, so reclaim ownership instead of failing
		// every post-rotation upgrade with an SSA field-manager conflict.
		upgrade.ForceConflicts = true
		if _, err := upgrade.RunWithContext(ctx, releaseName, ch, values); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "another operation") {
				return fmt.Errorf("upgrade Helm release %s: %w", releaseName, err)
			}
			uninstall := action.NewUninstall(configuration)
			uninstall.Timeout = 2 * time.Minute
			uninstall.WaitStrategy = kube.StatusWatcherStrategy
			if _, uninstallErr := uninstall.Run(releaseName); uninstallErr != nil {
				return fmt.Errorf("remove pending Helm release %s: %w", releaseName, uninstallErr)
			}
		} else {
			return nil
		}
	}
	install := action.NewInstall(configuration)
	install.ReleaseName = releaseName
	install.Namespace = namespace
	install.Timeout = 15 * time.Minute
	install.WaitStrategy = kube.StatusWatcherStrategy
	install.WaitForJobs = true
	install.RollbackOnFailure = true
	if _, err := install.RunWithContext(ctx, ch, values); err != nil {
		return fmt.Errorf("install Helm release %s: %w", releaseName, err)
	}
	return nil
}

func (h helmManager) uninstall(releaseName, namespace string) error {
	configuration, err := h.configuration(namespace)
	if err != nil {
		return err
	}
	uninstall := action.NewUninstall(configuration)
	uninstall.Timeout = 10 * time.Minute
	uninstall.WaitStrategy = kube.StatusWatcherStrategy
	if _, err := uninstall.Run(releaseName); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "release: not found") {
			return nil
		}
		return fmt.Errorf("uninstall Helm release %s: %w", releaseName, err)
	}
	return nil
}

func loadLocalChart(root, name string) (*chart.Chart, error) {
	path := filepath.Join(root, "deploy", "helm", name)
	loaded, err := loader.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load chart %s: %w", name, err)
	}
	return loaded, nil
}

func (h helmManager) loadRemoteChart(ctx context.Context, name, source string) (*chart.Chart, error) {
	path, err := downloadFile(ctx, h.cacheDir, name+".tgz", source)
	if err != nil {
		return nil, err
	}
	loaded, err := loader.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load downloaded chart %s: %w", name, err)
	}
	return loaded, nil
}

func (h helmManager) loadOpenSandboxChart(ctx context.Context) (*chart.Chart, error) {
	archivePath, err := downloadFile(ctx, h.cacheDir, "opensandbox-0.2.0-source.tgz", openSandboxURL)
	if err != nil {
		return nil, err
	}
	digest, err := fileDigest(archivePath)
	if err != nil {
		return nil, err
	}
	extractDir := filepath.Join(h.cacheDir, "opensandbox-"+digest[:12])
	marker := filepath.Join(extractDir, ".complete")
	if _, err := os.Stat(marker); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(extractDir, 0o755); err != nil {
			return nil, err
		}
		if err := extractTarGzip(archivePath, extractDir); err != nil {
			return nil, err
		}
		if err := os.WriteFile(marker, []byte(digest+"\n"), 0o600); err != nil {
			return nil, err
		}
	}
	chartsDir, err := findDirectoryContaining(extractDir, filepath.Join("kubernetes", "charts", "opensandbox", "Chart.yaml"))
	if err != nil {
		return nil, err
	}
	chartsDir = filepath.Join(chartsDir, "kubernetes", "charts")
	parent, err := loader.Load(filepath.Join(chartsDir, "opensandbox"))
	if err != nil {
		return nil, err
	}
	controller, err := loader.Load(filepath.Join(chartsDir, "opensandbox-controller"))
	if err != nil {
		return nil, err
	}
	server, err := loader.Load(filepath.Join(chartsDir, "opensandbox-server"))
	if err != nil {
		return nil, err
	}
	parent.SetDependencies(controller, server)
	return parent, nil
}

func downloadFile(ctx context.Context, directory, filename, source string) (string, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(directory, filename)
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		return path, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return "", err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", source, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("download %s: HTTP %s", source, response.Status)
	}
	temporary, err := os.CreateTemp(directory, filename+".*.tmp")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.Copy(temporary, response.Body); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return "", err
	}
	return path, nil
}

func extractTarGzip(path, destination string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	cleanDestination := filepath.Clean(destination) + string(os.PathSeparator)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(destination, header.Name)
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), cleanDestination) {
			return fmt.Errorf("archive entry escapes destination: %s", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode)&0o755)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(output, reader)
			closeErr := output.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
}

func findDirectoryContaining(root, suffix string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(filepath.ToSlash(path), filepath.ToSlash(suffix)) {
			found = strings.TrimSuffix(path, suffix)
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("could not find %s below %s", suffix, root)
	}
	return filepath.Clean(found), nil
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
