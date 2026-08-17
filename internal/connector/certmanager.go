package connector

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var certificateRequestGVR = schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificaterequests"}

type CertManagerIssuer struct {
	Client           dynamic.Interface
	Namespace        string
	IssuerName       string
	IssuerGeneration int32
	PollInterval     time.Duration
	Timeout          time.Duration
}

func (issuer CertManagerIssuer) Issue(ctx context.Context, connectorID uuid.UUID, csr *x509.CertificateRequest, ttl time.Duration) (Certificate, error) {
	if issuer.Client == nil || issuer.Namespace == "" || issuer.IssuerName == "" {
		return Certificate{}, errors.New("cert-manager issuer is not configured")
	}
	requestPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr.Raw})
	request := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cert-manager.io/v1",
		"kind":       "CertificateRequest",
		"metadata": map[string]any{
			"generateName": "argus-connector-",
			"namespace":    issuer.Namespace,
			"labels": map[string]any{
				"argus.io/connector-id": fmt.Sprint(connectorID),
			},
		},
		"spec": map[string]any{
			"request":  base64.StdEncoding.EncodeToString(requestPEM),
			"duration": ttl.String(),
			"isCA":     false,
			"usages":   []any{"client auth"},
			"issuerRef": map[string]any{
				"name":  issuer.IssuerName,
				"kind":  "Issuer",
				"group": "cert-manager.io",
			},
		},
	}}
	created, err := issuer.Client.Resource(certificateRequestGVR).Namespace(issuer.Namespace).Create(ctx, request, metav1.CreateOptions{})
	if err != nil {
		return Certificate{}, fmt.Errorf("create cert-manager CertificateRequest: %w", err)
	}
	return issuer.wait(ctx, created.GetName())
}

func (issuer CertManagerIssuer) wait(ctx context.Context, name string) (Certificate, error) {
	interval := issuer.PollInterval
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	timeout := issuer.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		request, err := issuer.Client.Resource(certificateRequestGVR).Namespace(issuer.Namespace).Get(waitCtx, name, metav1.GetOptions{})
		if err != nil {
			return Certificate{}, fmt.Errorf("read cert-manager CertificateRequest: %w", err)
		}
		ready, failed, reason := certificateRequestCondition(request)
		if failed {
			return Certificate{}, fmt.Errorf("cert-manager CertificateRequest failed: %s", reason)
		}
		if ready {
			return certificateFromRequest(request, issuer.IssuerGeneration)
		}
		select {
		case <-waitCtx.Done():
			return Certificate{}, fmt.Errorf("wait for cert-manager CertificateRequest: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func certificateRequestCondition(request *unstructured.Unstructured) (bool, bool, string) {
	conditions, _, _ := unstructured.NestedSlice(request.Object, "status", "conditions")
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok || condition["type"] != "Ready" {
			continue
		}
		status, _ := condition["status"].(string)
		reason, _ := condition["reason"].(string)
		return status == "True", status == "False", reason
	}
	return false, false, ""
}

func certificateFromRequest(request *unstructured.Unstructured, generation int32) (Certificate, error) {
	encodedCertificate, _, _ := unstructured.NestedString(request.Object, "status", "certificate")
	encodedCA, _, _ := unstructured.NestedString(request.Object, "status", "ca")
	certificatePEM, err := base64.StdEncoding.DecodeString(encodedCertificate)
	if err != nil || len(certificatePEM) == 0 {
		return Certificate{}, errors.New("cert-manager returned an invalid certificate")
	}
	caPEM, err := base64.StdEncoding.DecodeString(encodedCA)
	if err != nil {
		return Certificate{}, errors.New("cert-manager returned an invalid CA bundle")
	}
	block, _ := pem.Decode(certificatePEM)
	if block == nil {
		return Certificate{}, errors.New("cert-manager returned non-PEM certificate data")
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return Certificate{}, err
	}
	return Certificate{PEM: string(certificatePEM), CABundlePEM: string(caPEM), SerialNumber: serialString(parsed.SerialNumber),
		CertificateRequestName: request.GetName(), IssuerGeneration: generation, NotBefore: parsed.NotBefore, NotAfter: parsed.NotAfter}, nil
}

func serialString(value *big.Int) string {
	if value == nil {
		return ""
	}
	return value.String()
}
