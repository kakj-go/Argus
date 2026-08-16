package httpapi

import (
	"context"
	"net"
	"net/http"
)

type requestContextKey struct{}

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
