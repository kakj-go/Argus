package argusdev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/transport/spdy"
)

type KubeForward struct {
	stop       chan struct{}
	done       chan error
	stopOnce   sync.Once
	waitOnce   sync.Once
	closer     io.Closer
	stopResult error
}

func (k *E2EKube) PatchDeployment(ctx context.Context, namespace, name string, patch any) error {
	data, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = k.Client.AppsV1().Deployments(namespace).Patch(ctx, name, types.StrategicMergePatchType, data, metav1.PatchOptions{})
	return err
}

func (k *E2EKube) WaitDeployment(ctx context.Context, namespace, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		deployment, err := k.Client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		desired := int32(1)
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}
		if deployment.Status.ObservedGeneration >= deployment.Generation && deployment.Status.AvailableReplicas == desired {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("deployment %s/%s did not become available", namespace, name)
}

func (k *E2EKube) ScaleStatefulSet(ctx context.Context, namespace, name string, replicas int32) error {
	statefulSet, err := k.Client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	statefulSet.Spec.Replicas = &replicas
	_, err = k.Client.AppsV1().StatefulSets(namespace).Update(ctx, statefulSet, metav1.UpdateOptions{})
	return err
}

func (k *E2EKube) ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) error {
	deployment, err := k.Client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	deployment.Spec.Replicas = &replicas
	_, err = k.Client.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	return err
}

func (k *E2EKube) WaitDaemonSet(ctx context.Context, namespace, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		daemonSet, err := k.Client.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if daemonSet.Status.ObservedGeneration >= daemonSet.Generation && daemonSet.Status.NumberReady == daemonSet.Status.DesiredNumberScheduled && daemonSet.Status.DesiredNumberScheduled > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("daemonset %s/%s did not become ready", namespace, name)
}

func (k *E2EKube) RunJob(ctx context.Context, job *batchv1.Job, timeout time.Duration) error {
	jobs := k.Client.BatchV1().Jobs(job.Namespace)
	if err := jobs.Delete(ctx, job.Name, metav1.DeleteOptions{PropagationPolicy: deletePropagation(metav1.DeletePropagationForeground)}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	for {
		_, err := jobs.Get(ctx, job.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			break
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if _, err := jobs.Create(ctx, job, metav1.CreateOptions{}); err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		current, err := jobs.Get(ctx, job.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		for _, condition := range current.Status.Conditions {
			if condition.Status != corev1.ConditionTrue {
				continue
			}
			switch condition.Type {
			case batchv1.JobComplete:
				return nil
			case batchv1.JobFailed:
				return fmt.Errorf("job %s/%s failed: %s", job.Namespace, job.Name, condition.Message)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("job %s/%s did not complete", job.Namespace, job.Name)
}

func deletePropagation(value metav1.DeletionPropagation) *metav1.DeletionPropagation { return &value }

func (k *E2EKube) WaitStatefulSet(ctx context.Context, namespace, name string, replicas int32, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		statefulSet, err := k.Client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if statefulSet.Status.ObservedGeneration >= statefulSet.Generation && statefulSet.Status.ReadyReplicas == replicas {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("statefulset %s/%s did not reach %d ready replicas", namespace, name, replicas)
}

func (k *E2EKube) DeletePods(ctx context.Context, namespace, selector string) error {
	return k.Client.CoreV1().Pods(namespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: selector})
}

func (k *E2EKube) WaitPodReady(ctx context.Context, namespace, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pod, err := k.Client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				return nil
			}
		}
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			return fmt.Errorf("pod %s/%s ended as %s", namespace, name, pod.Status.Phase)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("pod %s/%s did not become ready", namespace, name)
}

func (k *E2EKube) AttachPod(ctx context.Context, namespace, name, container string, stdin io.Reader, stdout, stderr io.Writer) error {
	request := k.Client.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(name).SubResource("attach")
	request.VersionedParams(&corev1.PodAttachOptions{
		Container: container,
		Stdin:     stdin != nil,
		Stdout:    stdout != nil,
		Stderr:    stderr != nil,
		TTY:       false,
	}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(k.Config, http.MethodPost, request.URL())
	if err != nil {
		return err
	}
	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdin: stdin, Stdout: stdout, Stderr: stderr})
}

func (f *KubeForward) Stop() error {
	if f == nil {
		return nil
	}
	f.stopOnce.Do(func() { close(f.stop) })
	f.waitOnce.Do(func() {
		select {
		case f.stopResult = <-f.done:
		case <-time.After(5 * time.Second):
			f.stopResult = fmt.Errorf("Kubernetes port-forward did not stop")
		}
		if f.closer != nil {
			if err := f.closer.Close(); f.stopResult == nil {
				f.stopResult = err
			}
		}
	})
	return f.stopResult
}

func (k *E2EKube) PortForwardService(ctx context.Context, namespace, serviceName string, ports []string, output io.Writer) (*KubeForward, error) {
	service, err := k.Client.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	selector := labels.SelectorFromSet(service.Spec.Selector).String()
	pods, err := k.Client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	var names []string
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning && pod.DeletionTimestamp == nil {
			names = append(names, pod.Name)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("service %s/%s has no running pod", namespace, serviceName)
	}
	sort.Strings(names)
	requestURL := k.Client.CoreV1().RESTClient().Post().
		Resource("pods").Namespace(namespace).Name(names[0]).SubResource("portforward").URL()
	roundTripper, upgrader, err := spdy.RoundTripperFor(k.Config)
	if err != nil {
		return nil, err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, requestURL)
	stop := make(chan struct{})
	ready := make(chan struct{})
	forwarder, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, ports, stop, ready, output, output)
	if err != nil {
		return nil, err
	}
	handle := &KubeForward{stop: stop, done: make(chan error, 1)}
	if closer, ok := output.(io.Closer); ok {
		handle.closer = closer
	}
	go func() { handle.done <- forwarder.ForwardPorts() }()
	select {
	case <-ready:
		return handle, nil
	case err := <-handle.done:
		return nil, fmt.Errorf("port-forward %s/%s: %w", namespace, serviceName, err)
	case <-ctx.Done():
		_ = handle.Stop()
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		_ = handle.Stop()
		return nil, fmt.Errorf("port-forward %s/%s did not become ready", namespace, serviceName)
	}
}

func (k *E2EKube) Exec(ctx context.Context, namespace, selector, container string, command ...string) (string, error) {
	pods, err := k.Client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return "", err
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pod matches %s in %s", selector, namespace)
	}
	sort.Slice(pods.Items, func(i, j int) bool { return pods.Items[i].Name < pods.Items[j].Name })
	return k.execPod(ctx, namespace, pods.Items[0].Name, container, command...)
}

func (k *E2EKube) RemoveImages(ctx context.Context, namespace, selector, container string, images []string) error {
	if len(images) == 0 {
		return nil
	}
	pods, err := k.Client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	sort.Slice(pods.Items, func(i, j int) bool { return pods.Items[i].Name < pods.Items[j].Name })
	var removeErrors []error
	for _, pod := range pods.Items {
		for _, image := range images {
			_, err := k.execPod(ctx, namespace, pod.Name, container, "/host/ctr", "--address", "/run/containerd/containerd.sock", "--namespace", "k8s.io", "images", "remove", image)
			if err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
				removeErrors = append(removeErrors, fmt.Errorf("%s on %s: %w", image, pod.Name, err))
			}
		}
	}
	return errors.Join(removeErrors...)
}

func (k *E2EKube) execPod(ctx context.Context, namespace, podName, container string, command ...string) (string, error) {
	request := k.Client.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(podName).SubResource("exec")
	request.VersionedParams(&corev1.PodExecOptions{
		Container: container,
		Command:   command,
		Stdout:    true,
		Stderr:    true,
	}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(k.Config, http.MethodPost, request.URL())
	if err != nil {
		return "", err
	}
	var stdout, stderr bytes.Buffer
	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr}); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return "", fmt.Errorf("pod exec: %w: %s", err, message)
		}
		return "", fmt.Errorf("pod exec: %w", err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (k *E2EKube) DeleteNamespace(ctx context.Context, namespace string) error {
	err := k.Client.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		_, err := k.Client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("namespace %s was not deleted", namespace)
}
