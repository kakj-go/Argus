// Package httpapi contains the public HTTP transport for argus-server.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kakj-go/Argus/internal/buildinfo"
	auditapi "github.com/kakj-go/Argus/internal/gen/openapi/audit"
	authzapi "github.com/kakj-go/Argus/internal/gen/openapi/enterpriseauthz"
	enterpriseapi "github.com/kakj-go/Argus/internal/gen/openapi/enterpriseidentity"
	machineapi "github.com/kakj-go/Argus/internal/gen/openapi/machine"
	platformapi "github.com/kakj-go/Argus/internal/gen/openapi/platform"
	setupapi "github.com/kakj-go/Argus/internal/gen/openapi/setup"
)

type response struct {
	Service      string            `json:"service,omitempty"`
	Version      string            `json:"version,omitempty"`
	Status       string            `json:"status,omitempty"`
	Locale       string            `json:"locale,omitempty"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
}

type localeContextKey struct{}

const DefaultLocale = "zh-CN"

type Readiness interface{ Ready(context.Context) error }

type RouterOptions struct {
	PostgreSQL              Readiness
	Redis                   Readiness
	Setup                   *SetupHandler
	Platform                *PlatformHandler
	EnterpriseIdentity      *EnterpriseIdentityHandler
	EnterpriseAuthorization *EnterpriseAuthorizationHandler
	Machine                 *MachineHandler
	Audit                   *AuditHandler
	AllowedOrigins          []string
}

func NewRouter() http.Handler { return NewRouterWithOptions(RouterOptions{}) }

func NewRouterWithOptions(options RouterOptions) http.Handler {
	router := chi.NewRouter()
	router.Use(requestIDMiddleware)
	router.Use(corsMiddleware(options.AllowedOrigins))
	router.Use(localeMiddleware)
	router.Use(bodyLimitMiddleware)
	router.Get("/", serviceInfo)
	router.Get("/healthz", health)
	router.Get("/readyz", ready(options))
	if options.Setup != nil {
		strict := setupapi.NewStrictHandler(*options.Setup, []setupapi.StrictMiddlewareFunc{
			func(next setupapi.StrictHandlerFunc, _ string) setupapi.StrictHandlerFunc {
				return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value any) (any, error) {
					return next(WithRequestContext(ctx, writer, request), writer, request, value)
				}
			},
		})
		setupapi.HandlerFromMuxWithBaseURL(strict, router, "/api/v1")
	}
	if options.Platform != nil {
		strict := platformapi.NewStrictHandler(*options.Platform, []platformapi.StrictMiddlewareFunc{
			func(next platformapi.StrictHandlerFunc, _ string) platformapi.StrictHandlerFunc {
				return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value any) (any, error) {
					return next(WithRequestContext(ctx, writer, request), writer, request, value)
				}
			},
		})
		platformapi.HandlerFromMuxWithBaseURL(strict, router, "/api/v1")
	}
	if options.EnterpriseIdentity != nil {
		strict := enterpriseapi.NewStrictHandler(*options.EnterpriseIdentity, []enterpriseapi.StrictMiddlewareFunc{enterpriseIdentityRequestContext})
		enterpriseapi.HandlerFromMuxWithBaseURL(strict, router, "/api/v1")
	}
	if options.EnterpriseAuthorization != nil {
		strict := authzapi.NewStrictHandler(*options.EnterpriseAuthorization, []authzapi.StrictMiddlewareFunc{enterpriseAuthorizationRequestContext})
		authzapi.HandlerFromMuxWithBaseURL(strict, router, "/api/v1")
	}
	if options.Machine != nil {
		strict := machineapi.NewStrictHandler(*options.Machine, []machineapi.StrictMiddlewareFunc{machineRequestContext})
		machineapi.HandlerFromMuxWithBaseURL(strict, router, "/api/v1")
	}
	if options.Audit != nil {
		strict := auditapi.NewStrictHandler(*options.Audit, []auditapi.StrictMiddlewareFunc{auditRequestContext})
		auditapi.HandlerFromMuxWithBaseURL(strict, router, "/api/v1")
	}
	return router
}

func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			origin := request.Header.Get("Origin")
			if origin != "" {
				if !allowed[origin] {
					http.Error(writer, "origin not allowed", http.StatusForbidden)
					return
				}
				writer.Header().Set("Access-Control-Allow-Origin", origin)
				writer.Header().Set("Access-Control-Allow-Credentials", "true")
				writer.Header().Set("Vary", "Origin")
			}
			if request.Method == http.MethodOptions {
				writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				writer.Header().Set("Access-Control-Allow-Headers", "Accept, Accept-Language, Content-Type, Idempotency-Key, X-Argus-Setup-Token, X-CSRF-Token, X-Request-ID")
				writer.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(writer, request)
		})
	}
}

func enterpriseIdentityRequestContext(next enterpriseapi.StrictHandlerFunc, _ string) enterpriseapi.StrictHandlerFunc {
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value any) (any, error) {
		return next(WithRequestContext(ctx, writer, request), writer, request, value)
	}
}

func enterpriseAuthorizationRequestContext(next authzapi.StrictHandlerFunc, _ string) authzapi.StrictHandlerFunc {
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value any) (any, error) {
		return next(WithRequestContext(ctx, writer, request), writer, request, value)
	}
}

func machineRequestContext(next machineapi.StrictHandlerFunc, _ string) machineapi.StrictHandlerFunc {
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value any) (any, error) {
		return next(WithRequestContext(ctx, writer, request), writer, request, value)
	}
}

func auditRequestContext(next auditapi.StrictHandlerFunc, _ string) auditapi.StrictHandlerFunc {
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value any) (any, error) {
		return next(WithRequestContext(ctx, writer, request), writer, request, value)
	}
}

func serviceInfo(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, response{
		Service: "argus-server",
		Version: buildinfo.Version,
		Locale:  LocaleFromContext(request.Context()),
	})
}

func LocaleFromContext(ctx context.Context) string {
	if locale, ok := ctx.Value(localeContextKey{}).(string); ok {
		return locale
	}
	return DefaultLocale
}

func localeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		locale := negotiateLocale(request.Header.Get("Accept-Language"))
		writer.Header().Set("Content-Language", locale)
		next.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), localeContextKey{}, locale)))
	})
}

func negotiateLocale(header string) string {
	for _, candidate := range strings.Split(header, ",") {
		language := strings.ToLower(strings.TrimSpace(strings.SplitN(candidate, ";", 2)[0]))
		switch {
		case strings.HasPrefix(language, "en"):
			return "en-US"
		case strings.HasPrefix(language, "zh"):
			return "zh-CN"
		}
	}
	return DefaultLocale
}

func health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, response{Status: "ok"})
}

func ready(options RouterOptions) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		dependencies := map[string]string{"postgresql": "unknown", "redis": "unknown"}
		if options.PostgreSQL != nil {
			if err := options.PostgreSQL.Ready(request.Context()); err != nil {
				dependencies["postgresql"] = "unavailable"
				writeJSON(writer, http.StatusServiceUnavailable, response{Status: "not_ready", Dependencies: dependencies})
				return
			}
			dependencies["postgresql"] = "ready"
		}
		if options.Redis != nil {
			if err := options.Redis.Ready(request.Context()); err != nil {
				dependencies["redis"] = "degraded"
			} else {
				dependencies["redis"] = "ready"
			}
		}
		writeJSON(writer, http.StatusOK, response{Status: "ready", Dependencies: dependencies})
	}
}

func writeJSON(writer http.ResponseWriter, status int, body response) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestID := request.Header.Get("X-Request-ID")
		if requestID == "" {
			buffer := make([]byte, 16)
			_, _ = rand.Read(buffer)
			requestID = hex.EncodeToString(buffer)
			request.Header.Set("X-Request-ID", requestID)
		}
		writer.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(writer, request)
	})
}

func bodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(writer, request.Body, 1<<20)
		next.ServeHTTP(writer, request)
	})
}
