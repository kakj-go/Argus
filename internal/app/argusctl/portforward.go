package argusctl

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

type kubePortForward struct {
	localPort uint16
	stop      chan struct{}
	done      chan error
	stopOnce  sync.Once
	waitOnce  sync.Once
	result    error
}

func (forward *kubePortForward) Stop() error {
	if forward == nil {
		return nil
	}
	forward.stopOnce.Do(func() { close(forward.stop) })
	forward.waitOnce.Do(func() {
		select {
		case forward.result = <-forward.done:
		case <-time.After(5 * time.Second):
			forward.result = fmt.Errorf("Kubernetes port-forward did not stop")
		}
	})
	return forward.result
}

func portForwardService(ctx context.Context, clients *kubeClients, namespace, serviceName string, remotePort uint16) (*kubePortForward, error) {
	service, err := clients.typed.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	pods, err := clients.typed.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(service.Spec.Selector).String()})
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
	requestURL := clients.typed.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(names[0]).SubResource("portforward").URL()
	roundTripper, upgrader, err := spdy.RoundTripperFor(clients.rest)
	if err != nil {
		return nil, err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, requestURL)
	stop, ready := make(chan struct{}), make(chan struct{})
	forwarder, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, []string{fmt.Sprintf("0:%d", remotePort)}, stop, ready, io.Discard, io.Discard)
	if err != nil {
		return nil, err
	}
	handle := &kubePortForward{stop: stop, done: make(chan error, 1)}
	go func() { handle.done <- forwarder.ForwardPorts() }()
	select {
	case <-ready:
		ports, portErr := forwarder.GetPorts()
		if portErr != nil {
			_ = handle.Stop()
			return nil, fmt.Errorf("read port-forward binding: %w", portErr)
		}
		if len(ports) != 1 {
			_ = handle.Stop()
			return nil, fmt.Errorf("read port-forward binding: expected one port, got %d", len(ports))
		}
		handle.localPort = ports[0].Local
		return handle, nil
	case err = <-handle.done:
		return nil, fmt.Errorf("port-forward %s/%s: %w", namespace, serviceName, err)
	case <-ctx.Done():
		_ = handle.Stop()
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		_ = handle.Stop()
		return nil, fmt.Errorf("port-forward %s/%s did not become ready", namespace, serviceName)
	}
}
