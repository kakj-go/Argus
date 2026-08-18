package objectstore

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type blockedTransport struct{}

func (blockedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	<-request.Context().Done()
	return nil, request.Context().Err()
}

func TestPutStopsWhenObjectStoreDoesNotRespond(t *testing.T) {
	minioClient, err := minio.New("object-store.invalid", &minio.Options{
		Creds:     credentials.NewStaticV4("access", "secret", ""),
		Secure:    true,
		Transport: blockedTransport{},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{client: minioClient, bucket: "recordings", operationTimeout: 50 * time.Millisecond}
	started := time.Now()
	if err := client.Put(context.Background(), "recordings/test.cast.enc", []byte("ciphertext")); err == nil {
		t.Fatal("expected the blocked ObjectStore write to fail")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("ObjectStore write ignored its deadline: %s", elapsed)
	}
}
