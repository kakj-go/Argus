package connector

import (
	"fmt"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func NewDynamicClient(kubeconfigPath string) (dynamic.Interface, error) {
	var (
		configuration *rest.Config
		err           error
	)
	if kubeconfigPath != "" {
		configuration, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	} else {
		configuration, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("load Kubernetes configuration for cert-manager: %w", err)
	}
	client, err := dynamic.NewForConfig(configuration)
	if err != nil {
		return nil, fmt.Errorf("create cert-manager dynamic client: %w", err)
	}
	return client, nil
}
