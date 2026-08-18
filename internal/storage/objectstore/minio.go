package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const defaultOperationTimeout = 3 * time.Second

type Client struct {
	client           *minio.Client
	bucket           string
	operationTimeout time.Duration
}

func Open(ctx context.Context, endpoint, bucket, accessKey, secretKey string) (*Client, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || strings.TrimSpace(bucket) == "" || accessKey == "" || secretKey == "" {
		return nil, errors.New("invalid ObjectStore configuration")
	}
	client, err := minio.New(parsed.Host, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: parsed.Scheme == "https", Region: "us-east-1"})
	if err != nil {
		return nil, err
	}
	result := &Client{client: client, bucket: bucket, operationTimeout: defaultOperationTimeout}
	if err := result.Ready(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (client *Client) Ready(ctx context.Context) error {
	if client == nil || client.client == nil || client.bucket == "" {
		return errors.New("ObjectStore is not configured")
	}
	operationCtx, cancel := client.operationContext(ctx)
	defer cancel()
	exists, err := client.client.BucketExists(operationCtx, client.bucket)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("ObjectStore bucket %q does not exist", client.bucket)
	}
	return nil
}

func (client *Client) Put(ctx context.Context, key string, value []byte) error {
	if client == nil || client.client == nil || client.bucket == "" || key == "" || len(value) == 0 {
		return errors.New("ObjectStore key and value are required")
	}
	operationCtx, cancel := client.operationContext(ctx)
	defer cancel()
	_, err := client.client.PutObject(operationCtx, client.bucket, key, bytes.NewReader(value), int64(len(value)), minio.PutObjectOptions{ContentType: "application/octet-stream"})
	return err
}

func (client *Client) Get(ctx context.Context, key string) ([]byte, error) {
	if client == nil || client.client == nil || client.bucket == "" || key == "" {
		return nil, errors.New("ObjectStore key is required")
	}
	operationCtx, cancel := client.operationContext(ctx)
	defer cancel()
	object, err := client.client.GetObject(operationCtx, client.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer object.Close()
	value, err := io.ReadAll(io.LimitReader(object, 2<<20))
	if err != nil {
		return nil, err
	}
	if len(value) > (1<<20)+(64<<10) {
		return nil, errors.New("ObjectStore recording chunk exceeds limit")
	}
	return value, nil
}

func (client *Client) operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := client.operationTimeout
	if timeout <= 0 {
		timeout = defaultOperationTimeout
	}
	return context.WithTimeout(ctx, timeout)
}
