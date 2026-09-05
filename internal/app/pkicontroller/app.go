package pkicontroller

import (
	"context"
	"log/slog"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/kakj-go/Argus/internal/config"
	"github.com/kakj-go/Argus/internal/pkirotation"
	"github.com/kakj-go/Argus/internal/storage/postgres"
)

func Run(ctx context.Context, logger *slog.Logger) error {
	cfg := config.LoadPKIController()
	if err := cfg.Validate(); err != nil {
		return err
	}
	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	typed, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}
	dynamicClient, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}
	return (pkirotation.Reconciler{Store: store, Typed: typed, Dynamic: dynamicClient, Logger: logger, Config: pkirotation.Config{
		ReleaseID: cfg.ReleaseID, Mode: cfg.Mode, TrustSourceName: cfg.TrustSourceName,
		TrustBundleName: cfg.TrustBundleName, Namespaces: cfg.Namespaces, Interval: cfg.Interval,
	}}).Run(ctx)
}
