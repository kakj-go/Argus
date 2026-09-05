package telemetry

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestValidKubernetesImage(t *testing.T) {
	cases := []struct {
		name  string
		image string
		valid bool
	}{
		{"registry with tag", "registry.example.internal/argus/otelcol:0.1.0-m7", true},
		{"docker hub with tag", "docker.io/kakj-go/argus-otelcol:0.1.0-m7", true},
		{"digest reference", "registry.example.internal/argus/otelcol@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", true},
		{"localhost port with tag", "localhost:5001/argus/argus-otelcol:dev", true},
		{"empty", "", false},
		{"missing tag separator", "registry.example.internal/argus/otelcol", false},
		{"inner space", "registry.example.internal/argus/otel col:0.1.0", false},
		{"trailing newline", "registry.example.internal/argus/otelcol:0.1.0\n", false},
		{"too long", "r." + string(make([]byte, 512)) + ":tag", false},
	}
	for _, item := range cases {
		if got := validKubernetesImage(item.image); got != item.valid {
			t.Errorf("validKubernetesImage(%q) = %v, want %v (%s)", item.image, got, item.valid, item.name)
		}
	}
}

func TestCollectorPreviewImage(t *testing.T) {
	cases := []struct {
		name         string
		resourceType string
		input        string
		defaultImage string
		wantImage    string
		wantErr      error
	}{
		{
			name:         "k8s override wins",
			resourceType: "kubernetes_cluster",
			input:        "registry.internal/argus/otelcol:1.0.0",
			defaultImage: "docker.io/kakj-go/argus-otelcol:0.1.0-m7",
			wantImage:    "registry.internal/argus/otelcol:1.0.0",
		},
		{
			name:         "k8s falls back to server default",
			resourceType: "kubernetes_cluster",
			input:        "",
			defaultImage: "docker.io/kakj-go/argus-otelcol:0.1.0-m7",
			wantImage:    "docker.io/kakj-go/argus-otelcol:0.1.0-m7",
		},
		{
			name:         "k8s trims whitespace before override",
			resourceType: "kubernetes_cluster",
			input:        "  registry.internal/argus/otelcol:1.0.0  ",
			defaultImage: "docker.io/kakj-go/argus-otelcol:0.1.0-m7",
			wantImage:    "registry.internal/argus/otelcol:1.0.0",
		},
		{
			name:         "k8s without default and input is unavailable",
			resourceType: "kubernetes_cluster",
			wantErr:      ErrUnavailable,
		},
		{
			name:         "k8s rejects invalid override format",
			resourceType: "kubernetes_cluster",
			input:        "registry.internal/argus/otelcol",
			defaultImage: "docker.io/kakj-go/argus-otelcol:0.1.0-m7",
			wantErr:      ErrQueryInvalid,
		},
		{
			name:         "k8s rejects invalid default format",
			resourceType: "kubernetes_cluster",
			defaultImage: "registry.internal/argus/otelcol",
			wantErr:      ErrQueryInvalid,
		},
		{
			name:         "host rejects the field",
			resourceType: "host",
			input:        "registry.internal/argus/otelcol:1.0.0",
			wantErr:      ErrQueryInvalid,
		},
		{
			name:         "host without the field returns empty",
			resourceType: "host",
		},
	}
	for _, item := range cases {
		gotImage, gotErr := collectorPreviewImage(item.resourceType, item.input, item.defaultImage)
		switch {
		case item.wantErr != nil:
			if !errors.Is(gotErr, item.wantErr) {
				t.Errorf("%s: collectorPreviewImage err = %v, want %v", item.name, gotErr, item.wantErr)
			}
		case gotErr != nil:
			t.Errorf("%s: collectorPreviewImage unexpected err = %v", item.name, gotErr)
		case gotImage != item.wantImage:
			t.Errorf("%s: collectorPreviewImage = %q, want %q", item.name, gotImage, item.wantImage)
		}
	}
}

func TestHostCollectorPlatform(t *testing.T) {
	cases := []struct {
		name     string
		platform string
		arch     pgtype.Text
		want     string
		wantErr  bool
	}{
		{"windows host", "windows", pgtype.Text{}, "windows_amd64", false},
		{"linux arm64 detected", "linux", pgtype.Text{String: "arm64", Valid: true}, "linux_arm64", false},
		{"linux amd64 detected", "linux", pgtype.Text{String: "amd64", Valid: true}, "linux_amd64", false},
		{"linux unknown arch rejected", "linux", pgtype.Text{}, "", true},
		{"linux invalid arch rejected", "linux", pgtype.Text{String: "riscv64", Valid: true}, "", true},
	}
	for _, item := range cases {
		host := db.Host{Platform: item.platform, Architecture: item.arch}
		got, err := hostCollectorPlatform(host)
		if (err != nil) != item.wantErr {
			t.Errorf("%s: hostCollectorPlatform err = %v, wantErr %v", item.name, err, item.wantErr)
		} else if got != item.want {
			t.Errorf("%s: hostCollectorPlatform = %q, want %q", item.name, got, item.want)
		}
	}
}
