package argusctl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type WorkloadStatus struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Phase     string `json:"phase"`
	Ready     bool   `json:"ready"`
	Restarts  int32  `json:"restarts"`
}

type StatusReport struct {
	ReleaseID string            `json:"releaseId"`
	Stage     map[string]string `json:"stage,omitempty"`
	Network   *NetworkProfile   `json:"network,omitempty"`
	Pods      []WorkloadStatus  `json:"pods"`
	PVCs      int               `json:"pvcs"`
	Ready     bool              `json:"ready"`
}

func (a *App) status(ctx context.Context, cfg *InstallConfig, output string) error {
	clients, err := clientsFor(cfg.Spec.KubeContext)
	if err != nil {
		return err
	}
	report := StatusReport{ReleaseID: cfg.Spec.ReleaseID, Stage: map[string]string{}, Ready: true}
	if configMap, getErr := clients.typed.CoreV1().ConfigMaps(cfg.Spec.Namespaces.System).Get(ctx, cfg.Spec.ReleaseID+"-install-status", metav1.GetOptions{}); getErr == nil {
		report.Stage = configMap.Data
		if encoded := configMap.Data["network-profile"]; encoded != "" {
			var profile NetworkProfile
			if decodeErr := json.Unmarshal([]byte(encoded), &profile); decodeErr == nil {
				report.Network = &profile
			}
		}
	}
	if report.Network == nil {
		profile := discoverNetworkProfile(ctx, clients, cfg)
		report.Network = &profile
	}
	for _, namespace := range []string{cfg.Spec.Namespaces.System, cfg.Spec.Namespaces.Sandbox, cfg.Spec.Namespaces.Observability} {
		pods, listErr := clients.typed.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if listErr != nil {
			report.Ready = false
			continue
		}
		for _, pod := range pods.Items {
			ready := true
			var restarts int32
			for _, status := range pod.Status.ContainerStatuses {
				ready = ready && status.Ready
				restarts += status.RestartCount
			}
			if len(pod.Status.ContainerStatuses) == 0 || pod.Status.Phase == "Failed" {
				ready = false
			}
			if pod.Status.Phase == "Succeeded" {
				ready = true
			}
			report.Ready = report.Ready && ready
			report.Pods = append(report.Pods, WorkloadStatus{Namespace: namespace, Name: pod.Name, Phase: string(pod.Status.Phase), Ready: ready, Restarts: restarts})
		}
		pvcs, _ := clients.typed.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
		report.PVCs += len(pvcs.Items)
	}
	sort.Slice(report.Pods, func(i, j int) bool {
		if report.Pods[i].Namespace == report.Pods[j].Namespace {
			return report.Pods[i].Name < report.Pods[j].Name
		}
		return report.Pods[i].Namespace < report.Pods[j].Namespace
	})
	return writeOutput(a.stdout, output, report, func(w io.Writer) {
		_, _ = fmt.Fprintf(w, "Argus %s ready=%t PVCs=%d\n", report.ReleaseID, report.Ready, report.PVCs)
		if stage := report.Stage["current-stage"]; stage != "" {
			_, _ = fmt.Fprintf(w, "Stage: %s (%s) %s\n", stage, report.Stage["state"], report.Stage["message"])
		}
		if report.Network != nil {
			_, _ = fmt.Fprintf(w, "NetworkPolicy: %s; Egress Gateway: %s; Security posture: %s\n", report.Network.Policy.Enforcement, report.Network.Egress.Status, report.Network.SecurityPosture)
		}
		for _, pod := range report.Pods {
			_, _ = fmt.Fprintf(w, "%s/%s phase=%s ready=%t restarts=%d\n", pod.Namespace, pod.Name, pod.Phase, pod.Ready, pod.Restarts)
		}
	})
}
