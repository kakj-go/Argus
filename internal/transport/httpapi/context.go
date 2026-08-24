package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
)

type requestContextKey struct{}
type requestLoggerContextKey struct{}

type RequestContext struct {
	Writer    http.ResponseWriter
	Request   *http.Request
	RequestID string
	ClientIP  string
}

func WithRequestContext(ctx context.Context, writer http.ResponseWriter, request *http.Request) context.Context {
	requestID := request.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = "server-generated-request"
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	value := RequestContext{Writer: writer, Request: request, RequestID: requestID, ClientIP: host}
	return context.WithValue(ctx, requestContextKey{}, value)
}

func RequestFromContext(ctx context.Context) (RequestContext, bool) {
	value, ok := ctx.Value(requestContextKey{}).(RequestContext)
	return value, ok
}

func logMappedError(ctx context.Context, code string, err error) {
	logger, ok := ctx.Value(requestLoggerContextKey{}).(*slog.Logger)
	if !ok || logger == nil {
		return
	}
	metadata, _ := RequestFromContext(ctx)
	attributes := []any{"error_code", code}
	if metadata.Request != nil {
		attributes = append(attributes, "method", metadata.Request.Method, "path", metadata.Request.URL.Path)
	}
	if code == "INTERNAL_ERROR" {
		logger.Error("HTTP request failed", append(attributes, "error", err)...)
		return
	}
	logger.Warn("HTTP request rejected", attributes...)
}

func copyErrorParams[T any](source any) *T {
	if source == nil {
		return nil
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		return nil
	}
	var target T
	if err := json.Unmarshal(encoded, &target); err != nil {
		return nil
	}
	return &target
}
