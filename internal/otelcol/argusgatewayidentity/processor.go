package argusgatewayidentity

import (
	"context"
	"crypto/x509"
	"errors"
	"strings"

	"go.opentelemetry.io/collector/client"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

var componentType = component.MustNewType("argus_gateway_identity")

type Config struct{}

func NewFactory() processor.Factory {
	return processor.NewFactory(componentType, func() component.Config { return &Config{} },
		processor.WithMetrics(createMetrics, component.StabilityLevelBeta),
		processor.WithLogs(createLogs, component.StabilityLevelBeta),
		processor.WithTraces(createTraces, component.StabilityLevelBeta))
}

func createMetrics(ctx context.Context, set processor.Settings, config component.Config, next consumer.Metrics) (processor.Metrics, error) {
	return processorhelper.NewMetrics(ctx, set, config, next, func(ctx context.Context, value pmetric.Metrics) (pmetric.Metrics, error) {
		identity, err := identityFromContext(ctx)
		if err != nil {
			return value, err
		}
		resources := value.ResourceMetrics()
		for index := 0; index < resources.Len(); index++ {
			overwrite(resources.At(index).Resource().Attributes(), identity)
		}
		return value, nil
	})
}

func createLogs(ctx context.Context, set processor.Settings, config component.Config, next consumer.Logs) (processor.Logs, error) {
	return processorhelper.NewLogs(ctx, set, config, next, func(ctx context.Context, value plog.Logs) (plog.Logs, error) {
		identity, err := identityFromContext(ctx)
		if err != nil {
			return value, err
		}
		resources := value.ResourceLogs()
		for index := 0; index < resources.Len(); index++ {
			overwrite(resources.At(index).Resource().Attributes(), identity)
		}
		return value, nil
	})
}

func createTraces(ctx context.Context, set processor.Settings, config component.Config, next consumer.Traces) (processor.Traces, error) {
	return processorhelper.NewTraces(ctx, set, config, next, func(ctx context.Context, value ptrace.Traces) (ptrace.Traces, error) {
		identity, err := identityFromContext(ctx)
		if err != nil {
			return value, err
		}
		resources := value.ResourceSpans()
		for index := 0; index < resources.Len(); index++ {
			overwrite(resources.At(index).Resource().Attributes(), identity)
		}
		return value, nil
	})
}

type downstreamIdentity struct{ collectorID, serial string }

func identityFromContext(ctx context.Context) (downstreamIdentity, error) {
	auth := client.FromContext(ctx).Auth
	if auth != nil {
		collectorID, _ := auth.GetAttribute("argus.telemetry.collector_id").(string)
		serial, _ := auth.GetAttribute("argus.telemetry.certificate_serial").(string)
		if collectorID != "" && serial != "" {
			return downstreamIdentity{collectorID: collectorID, serial: serial}, nil
		}
	}
	// Some Collector receiver paths do not preserve client.Info.Auth when
	// invoking a downstream processor. Derive the same identity directly from
	// the authenticated gRPC peer certificate as a fail-closed fallback.
	peerValue, ok := peer.FromContext(ctx)
	if !ok {
		return downstreamIdentity{}, errors.New("downstream telemetry identity is unavailable")
	}
	tlsInfo, ok := peerValue.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) != 1 {
		return downstreamIdentity{}, errors.New("downstream telemetry peer certificate is unavailable")
	}
	certificate := tlsInfo.State.PeerCertificates[0]
	collectorID, ok := telemetryCollectorID(certificate)
	if !ok || certificate.SerialNumber == nil {
		return downstreamIdentity{}, errors.New("downstream telemetry peer identity is incomplete")
	}
	return downstreamIdentity{collectorID: collectorID, serial: strings.ToLower(certificate.SerialNumber.Text(16))}, nil
}

func telemetryCollectorID(certificate *x509.Certificate) (string, bool) {
	if certificate == nil || len(certificate.URIs) != 1 {
		return "", false
	}
	const prefix = "spiffe://argus/telemetry/collectors/"
	value := certificate.URIs[0].String()
	if !strings.HasPrefix(value, prefix) {
		return "", false
	}
	id := strings.TrimPrefix(value, prefix)
	return id, id != "" && !strings.Contains(id, "/")
}

func overwrite(attributes pcommon.Map, identity downstreamIdentity) {
	keys := make([]string, 0, attributes.Len())
	attributes.Range(func(key string, _ pcommon.Value) bool {
		if strings.HasPrefix(key, "argus.") {
			keys = append(keys, key)
		}
		return true
	})
	for _, key := range keys {
		attributes.Remove(key)
	}
	attributes.PutStr("argus.downstream.collector.id", identity.collectorID)
	attributes.PutStr("argus.downstream.certificate.serial", identity.serial)
}
