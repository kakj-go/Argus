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
	actionapi "github.com/kakj-go/Argus/internal/gen/openapi/actionapi"
	auditapi "github.com/kakj-go/Argus/internal/gen/openapi/audit"
	automationapi "github.com/kakj-go/Argus/internal/gen/openapi/automationapi"
	connectionapi "github.com/kakj-go/Argus/internal/gen/openapi/connectionapi"
	connectorapi "github.com/kakj-go/Argus/internal/gen/openapi/connectorapi"
	conversationapi "github.com/kakj-go/Argus/internal/gen/openapi/conversationapi"
	authzapi "github.com/kakj-go/Argus/internal/gen/openapi/enterpriseauthz"
	enterpriseapi "github.com/kakj-go/Argus/internal/gen/openapi/enterpriseidentity"
	hostapi "github.com/kakj-go/Argus/internal/gen/openapi/hostapi"
	kubernetesapi "github.com/kakj-go/Argus/internal/gen/openapi/kubernetesapi"
	machineapi "github.com/kakj-go/Argus/internal/gen/openapi/machine"
	modelapi "github.com/kakj-go/Argus/internal/gen/openapi/modelapi"
	platformapi "github.com/kakj-go/Argus/internal/gen/openapi/platform"
	sandboxapi "github.com/kakj-go/Argus/internal/gen/openapi/sandboxapi"
	secretapi "github.com/kakj-go/Argus/internal/gen/openapi/secretapi"
	setupapi "github.com/kakj-go/Argus/internal/gen/openapi/setup"
	workflowapi "github.com/kakj-go/Argus/internal/gen/openapi/workflowapi"
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
	Secret                  *SecretHandler
	Host                    *HostHandler
	Kubernetes              *KubernetesHandler
	Connection              *ConnectionHandler
	ResourceAction          *ResourceActionHandler
	Workflow                *WorkflowHandler
	Conversation            *ConversationHandler
	Model                   *ModelHandler
	Automation              *AutomationHandler
	Sandbox                 *SandboxHandler
	Connector               *ConnectorHandler
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
	if options.Secret != nil {
		strict := secretapi.NewStrictHandler(*options.Secret, []secretapi.StrictMiddlewareFunc{secretRequestContext})
		secretapi.HandlerFromMuxWithBaseURL(strict, router, "/api/v1")
	}
	if options.Host != nil {
		strict := hostapi.NewStrictHandler(*options.Host, []hostapi.StrictMiddlewareFunc{hostRequestContext})
		hostapi.HandlerFromMuxWithBaseURL(strict, router, "/api/v1")
	}
	if options.Kubernetes != nil {
		strict := kubernetesapi.NewStrictHandler(*options.Kubernetes, []kubernetesapi.StrictMiddlewareFunc{kubernetesRequestContext})
		kubernetesapi.HandlerFromMuxWithBaseURL(strict, router, "/api/v1")
	}
	if options.Connection != nil {
		strict := connectionapi.NewStrictHandler(*options.Connection, []connectionapi.StrictMiddlewareFunc{connectionRequestContext})
		connectionapi.HandlerFromMuxWithBaseURL(strict, router, "/api/v1")
	}
	if options.ResourceAction != nil {
		strict := actionapi.NewStrictHandler(*options.ResourceAction, []actionapi.StrictMiddlewareFunc{actionRequestContext})
		actionapi.HandlerFromMuxWithBaseURL(strict, router, "/api/v1")
	}
	if options.Workflow != nil {
		strict := workflowapi.NewStrictHandler(*options.Workflow, []workflowapi.StrictMiddlewareFunc{workflowRequestContext})
		workflowapi.HandlerFromMuxWithBaseURL(strict, router, "/api/v1")
	}
	if options.Conversation != nil {
		strict := conversationapi.NewStrictHandler(*options.Conversation, []conversationapi.StrictMiddlewareFunc{conversationRequestContext})
		conversationapi.HandlerFromMuxWithBaseURL(strict, router, "/api/v1")
	}
	if options.Model != nil {
		strict := modelapi.NewStrictHandler(*options.Model, []modelapi.StrictMiddlewareFunc{modelRequestContext})
		modelapi.HandlerFromMuxWithBaseURL(strict, router, "/api/v1")
	}
	if options.Automation != nil {
		strict := automationapi.NewStrictHandler(*options.Automation, []automationapi.StrictMiddlewareFunc{automationRequestContext})
		automationapi.HandlerFromMuxWithBaseURL(strict, router, "/api/v1")
	}
	if options.Sandbox != nil {
		strict := sandboxapi.NewStrictHandler(*options.Sandbox, []sandboxapi.StrictMiddlewareFunc{sandboxRequestContext})
		sandboxapi.HandlerFromMuxWithBaseURL(strict, router, "/api/v1")
	}
	if options.Connector != nil {
		strict := connectorapi.NewStrictHandler(*options.Connector, []connectorapi.StrictMiddlewareFunc{connectorRequestContext})
		connectorapi.HandlerFromMuxWithBaseURL(strict, router, "/api/v1")
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
				writer.Header().Set("Access-Control-Allow-Headers", "Accept, Accept-Language, Content-Type, Idempotency-Key, X-Argus-Enrollment-Token, X-Argus-Setup-Token, X-CSRF-Token, X-Request-ID")
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

func workflowRequestContext(next workflowapi.StrictHandlerFunc, _ string) workflowapi.StrictHandlerFunc {
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value any) (any, error) {
		return next(WithRequestContext(ctx, writer, request), writer, request, value)
	}
}

func conversationRequestContext(next conversationapi.StrictHandlerFunc, _ string) conversationapi.StrictHandlerFunc {
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value any) (any, error) {
		return next(WithRequestContext(ctx, writer, request), writer, request, value)
	}
}

func modelRequestContext(next modelapi.StrictHandlerFunc, _ string) modelapi.StrictHandlerFunc {
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value any) (any, error) {
		return next(WithRequestContext(ctx, writer, request), writer, request, value)
	}
}

func automationRequestContext(next automationapi.StrictHandlerFunc, _ string) automationapi.StrictHandlerFunc {
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value any) (any, error) {
		return next(WithRequestContext(ctx, writer, request), writer, request, value)
	}
}

func sandboxRequestContext(next sandboxapi.StrictHandlerFunc, _ string) sandboxapi.StrictHandlerFunc {
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

func secretRequestContext(next secretapi.StrictHandlerFunc, _ string) secretapi.StrictHandlerFunc {
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value any) (any, error) {
		return next(WithRequestContext(ctx, writer, request), writer, request, value)
	}
}

func hostRequestContext(next hostapi.StrictHandlerFunc, _ string) hostapi.StrictHandlerFunc {
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value any) (any, error) {
		return next(WithRequestContext(ctx, writer, request), writer, request, value)
	}
}

func kubernetesRequestContext(next kubernetesapi.StrictHandlerFunc, _ string) kubernetesapi.StrictHandlerFunc {
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value any) (any, error) {
		return next(WithRequestContext(ctx, writer, request), writer, request, value)
	}
}

func connectionRequestContext(next connectionapi.StrictHandlerFunc, _ string) connectionapi.StrictHandlerFunc {
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value any) (any, error) {
		return next(WithRequestContext(ctx, writer, request), writer, request, value)
	}
}

func actionRequestContext(next actionapi.StrictHandlerFunc, _ string) actionapi.StrictHandlerFunc {
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, value any) (any, error) {
		return next(WithRequestContext(ctx, writer, request), writer, request, value)
	}
}

func connectorRequestContext(next connectorapi.StrictHandlerFunc, _ string) connectorapi.StrictHandlerFunc {
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
