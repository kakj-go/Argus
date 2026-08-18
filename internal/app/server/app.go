// Package server contains the argus-server application bootstrap.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/kakj-go/Argus/internal/action"
	"github.com/kakj-go/Argus/internal/agent"
	"github.com/kakj-go/Argus/internal/authorization"
	"github.com/kakj-go/Argus/internal/automation"
	cardservice "github.com/kakj-go/Argus/internal/card"
	"github.com/kakj-go/Argus/internal/config"
	connectorservice "github.com/kakj-go/Argus/internal/connector"
	"github.com/kakj-go/Argus/internal/conversation"
	"github.com/kakj-go/Argus/internal/directexecutor"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/kubernetesreader"
	"github.com/kakj-go/Argus/internal/mcp"
	modelservice "github.com/kakj-go/Argus/internal/model"
	"github.com/kakj-go/Argus/internal/outbox"
	"github.com/kakj-go/Argus/internal/pagination"
	"github.com/kakj-go/Argus/internal/platform"
	remoteaccessservice "github.com/kakj-go/Argus/internal/remoteaccess"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/sandbox"
	secretservice "github.com/kakj-go/Argus/internal/secret"
	objectstore "github.com/kakj-go/Argus/internal/storage/objectstore"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	redisstore "github.com/kakj-go/Argus/internal/storage/redis"
	"github.com/kakj-go/Argus/internal/transport/httpapi"
)

const shutdownTimeout = 10 * time.Second

type App struct {
	config config.Server
	logger *slog.Logger
}

func New(cfg config.Server, logger *slog.Logger) *App {
	return &App{config: cfg, logger: logger}
}

func (a *App) Run(ctx context.Context) error {
	if err := a.config.Validate(); err != nil {
		return err
	}
	secretKeyring, err := secretservice.LoadKeyring(a.config.SecretKEKPath)
	if err != nil {
		return err
	}
	kubernetesClient, err := connectorservice.NewDynamicClient(a.config.KubeconfigPath)
	if err != nil {
		return err
	}
	deniedCIDRs, err := resource.ParseDeniedCIDRs(a.config.DirectDeniedCIDRs)
	if err != nil {
		return err
	}
	postgresStore, err := postgres.Open(ctx, a.config.DatabaseURL)
	if err != nil {
		return err
	}
	defer postgresStore.Close()
	directDispatcher, err := directexecutor.NewDispatcher(a.config.DirectExecutorEndpoint, a.config.DirectExecutorServerName,
		a.config.DirectExecutorTLSCert, a.config.DirectExecutorTLSKey, a.config.DirectExecutorCABundle)
	if err != nil {
		return err
	}
	defer directDispatcher.Close()
	redisClient, err := redisstore.Open(ctx, a.config.RedisURL)
	if err != nil {
		if redisClient == nil {
			return err
		}
		a.logger.Warn("Redis unavailable; starting in degraded mode", "error", err)
	}
	defer func() { _ = redisClient.Close() }()
	objects, err := objectstore.Open(ctx, a.config.ObjectStoreURL, a.config.ObjectStoreBucket, a.config.ObjectStoreAccess, a.config.ObjectStoreSecret)
	if err != nil {
		return err
	}
	identityService := identity.Service{Store: postgresStore, Redis: redisClient, IdleTTL: a.config.SessionIdleTTL, AbsoluteTTL: a.config.SessionAbsoluteTTL}
	cursorSigner := pagination.Signer{Key: a.config.CursorSigningKey}
	setupHandler := httpapi.SetupHandler{
		Config: a.config, Setup: platform.SetupService{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey}}, Identity: identityService,
		Token: platform.SetupTokenProvider{TokenPath: a.config.SetupTokenPath, ExpiresPath: a.config.SetupTokenExpiresPath},
	}
	platformHandler := httpapi.PlatformHandler{Auth: setupHandler, Enterprise: platform.EnterpriseService{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey}}, Cursor: cursorSigner}
	machineService := identity.MachineService{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey}}
	enterpriseIdentityHandler := httpapi.EnterpriseIdentityHandler{Auth: setupHandler, Service: identity.EnterpriseService{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey}}, Machine: machineService, Cursor: cursorSigner}
	enterpriseAuthorizationHandler := httpapi.EnterpriseAuthorizationHandler{Identity: enterpriseIdentityHandler, Service: authorization.Service{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey}}, Cursor: cursorSigner}
	machineHandler := httpapi.MachineHandler{Identity: enterpriseIdentityHandler, Service: machineService, Cursor: cursorSigner}
	auditHandler := httpapi.AuditHandler{Auth: setupHandler, Enterprise: enterpriseIdentityHandler, Store: postgresStore, Cursor: cursorSigner}
	secretDomain := secretservice.Service{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey}, Keyring: secretKeyring}
	actionDomain := resource.PendingActionService{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey}, Key: a.config.PendingActionKey}
	connectorDomain := connectorservice.Service{Store: postgresStore, Redis: redisClient, GatewayEndpoint: a.config.ConnectorGatewayAddress,
		EnrollmentURL: a.config.ConnectorEnrollmentURL,
		Credentials:   secretDomain,
		Issuer: connectorservice.CertManagerIssuer{Client: kubernetesClient, Namespace: a.config.SystemNamespace,
			IssuerName: a.config.ConnectorIssuerName, IssuerGeneration: a.config.ConnectorIssuerGeneration}}
	bastionDomain := connectorservice.BastionService{Store: postgresStore, Actions: actionDomain, Enrollment: connectorDomain}
	resourceDomain := resource.Service{Store: postgresStore, Actions: actionDomain, Access: resource.AccessService{Store: postgresStore},
		Direct: resource.DirectTargetValidator{DeniedCIDRs: deniedCIDRs}, Commands: connectorDomain, DirectCommands: directDispatcher, Extension: bastionDomain,
		ClusterEnrollment: connectorDomain,
		Kubernetes: kubernetesreader.Reader{Store: postgresStore, Secrets: secretDomain, Validator: resource.DirectTargetValidator{DeniedCIDRs: deniedCIDRs},
			Notifier: connectorDomain}}
	workflowDomain := action.Service{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey}, Resources: resourceDomain,
		OneTimeResultKey: a.config.PendingActionKey}
	secretHandler := httpapi.SecretHandler{Identity: enterpriseIdentityHandler, Service: secretDomain}
	hostHandler := httpapi.HostHandler{Identity: enterpriseIdentityHandler, Service: resourceDomain}
	kubernetesHandler := httpapi.KubernetesHandler{Identity: enterpriseIdentityHandler, Service: resourceDomain}
	connectionHandler := httpapi.ConnectionHandler{Identity: enterpriseIdentityHandler, Service: resourceDomain}
	actionHandler := httpapi.ResourceActionHandler{Identity: enterpriseIdentityHandler, Service: resourceDomain, Workflow: workflowDomain}
	workflowHandler := httpapi.WorkflowHandler{Identity: enterpriseIdentityHandler, Service: workflowDomain}
	conversationHandler := httpapi.ConversationHandler{Identity: enterpriseIdentityHandler,
		Service: conversation.Service{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey}}}
	modelHandler := httpapi.ModelHandler{Identity: enterpriseIdentityHandler,
		Service: modelservice.Service{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey}, Keyring: secretKeyring}}
	toolRegistry := mcp.NewRegistry()
	if err := (agent.ResourceTools{Store: postgresStore, Resources: resourceDomain}).Register(toolRegistry); err != nil {
		return err
	}
	cardDomain := cardservice.Service{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey}, Tools: toolRegistry, PresentationTTL: a.config.CardPresentationTTL,
		ValidationTTL: a.config.CardValidationTTL, RuntimeVersion: a.config.CardRuntimeVersion, MaxPresentation: a.config.CardMaxPresentationBytes}
	if err := cardDomain.RegisterRenderTool(toolRegistry); err != nil {
		return err
	}
	cardHandler := httpapi.CardHandler{Identity: enterpriseIdentityHandler, Service: cardDomain, Workflow: workflowDomain}
	automationHandler := httpapi.AutomationHandler{Identity: enterpriseIdentityHandler,
		Service: automation.Service{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey}, Tools: toolRegistry}}
	sandboxHandler := httpapi.SandboxHandler{Auth: setupHandler, Service: sandbox.Service{Store: postgresStore, Keyring: secretKeyring}}
	connectorHandler := httpapi.ConnectorHandler{Identity: enterpriseIdentityHandler, Service: connectorDomain, Bastion: bastionDomain}
	remoteWebsocketURL, err := websocketURL(a.config.RemoteOrigin)
	if err != nil {
		return err
	}
	remoteAccessHandler := httpapi.RemoteAccessHandler{Identity: enterpriseIdentityHandler, WebsocketURL: remoteWebsocketURL,
		Service: remoteaccessservice.Service{Store: postgresStore, Idempotency: postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey},
			Access: resource.AccessService{Store: postgresStore}, Keyring: secretKeyring, ObjectStore: objects, UserLimit: a.config.RemoteUserLimit,
			HostLimit: a.config.RemoteHostLimit, EnterpriseLimit: a.config.RemoteEnterpriseLimit}}
	go (outbox.Relay{Store: postgresStore, Redis: redisClient, Logger: a.logger}).Run(ctx)
	server := &http.Server{
		Addr: a.config.Address,
		Handler: httpapi.NewRouterWithOptions(httpapi.RouterOptions{
			PostgreSQL: postgresStore, Redis: redisClient, Setup: &setupHandler, Platform: &platformHandler,
			EnterpriseIdentity: &enterpriseIdentityHandler, EnterpriseAuthorization: &enterpriseAuthorizationHandler,
			Machine: &machineHandler, Audit: &auditHandler, Secret: &secretHandler, Host: &hostHandler, Kubernetes: &kubernetesHandler,
			Connection: &connectionHandler, ResourceAction: &actionHandler, Workflow: &workflowHandler, Conversation: &conversationHandler, Model: &modelHandler,
			Automation:     &automationHandler,
			Sandbox:        &sandboxHandler,
			AllowedOrigins: a.config.AllowedOrigins,
			Connector:      &connectorHandler,
			Card:           &cardHandler,
			RemoteAccess:   &remoteAccessHandler,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			a.logger.Error("HTTP server graceful shutdown failed", "error", err)
		}
	}()

	a.logger.Info("argus-server started", "address", a.config.Address)
	err = server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	<-shutdownDone
	a.logger.Info("argus-server stopped")
	return nil
}

func websocketURL(origin string) (string, error) {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", errors.New("ARGUS_REMOTE_ORIGIN must be an HTTP(S) origin")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("ARGUS_REMOTE_ORIGIN must not include a path")
	}
	if parsed.Scheme == "https" {
		parsed.Scheme = "wss"
	} else {
		parsed.Scheme = "ws"
	}
	parsed.Path, parsed.RawQuery, parsed.Fragment = "", "", ""
	return parsed.String(), nil
}
