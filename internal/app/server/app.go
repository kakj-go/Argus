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
	"github.com/kakj-go/Argus/internal/artifactcheck"
	"github.com/kakj-go/Argus/internal/authorization"
	cardservice "github.com/kakj-go/Argus/internal/card"
	"github.com/kakj-go/Argus/internal/config"
	connectorservice "github.com/kakj-go/Argus/internal/connector"
	"github.com/kakj-go/Argus/internal/conversation"
	"github.com/kakj-go/Argus/internal/directexecutor"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/installinstruction"
	"github.com/kakj-go/Argus/internal/keywrap"
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
	telemetryservice "github.com/kakj-go/Argus/internal/telemetry"
	"github.com/kakj-go/Argus/internal/transport/httpapi"
	"github.com/kakj-go/Argus/internal/trustbundle"
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
	secretKeyring, err := secretservice.LoadConfiguredKeyring(a.config.SecretKEKPath, a.config.KeyWrappingMode,
		a.config.OpenBaoAddress, a.config.OpenBaoToken, a.config.OpenBaoTransitKey)
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
	bundles := trustbundle.Service{Store: postgresStore, MountedPath: a.config.TrustBundlePath, InitialEpoch: a.config.TrustBundleEpoch}
	activeBundle, err := bundles.EnsureInitial(ctx)
	if err != nil {
		return err
	}
	bundleNodeID := trustbundle.ProcessNodeID("server")
	if err := bundles.AcknowledgeMounted(ctx, bundleNodeID); err != nil {
		return err
	}
	go bundles.RunMountedAcknowledger(ctx, bundleNodeID, 5*time.Second, func(err error) {
		a.logger.Warn("Trust Bundle acknowledgement failed", "error", err)
	})
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
	var wrapping keywrap.Provider = keywrap.Local{Key: a.config.IdempotencyEncryptionKey, KeyID: "argus-evaluation"}
	if a.config.KeyWrappingMode == keywrap.ProviderOpenBaoTransit {
		wrapping = keywrap.OpenBao{Address: a.config.OpenBaoAddress, Token: a.config.OpenBaoToken, KeyID: a.config.OpenBaoTransitKey}
	}
	idempotency := postgres.Idempotency{Key: a.config.IdempotencyEncryptionKey, Provider: wrapping}
	identityService := identity.Service{Store: postgresStore, Redis: redisClient, IdleTTL: a.config.SessionIdleTTL, AbsoluteTTL: a.config.SessionAbsoluteTTL,
		KeyWrapping: wrapping, Idempotency: idempotency, BreakGlassEnabled: a.config.BreakGlassEnabled}
	cursorSigner := pagination.Signer{Key: a.config.CursorSigningKey}
	setupHandler := httpapi.SetupHandler{
		Config: a.config, Setup: platform.SetupService{Store: postgresStore, Idempotency: idempotency}, Identity: identityService,
		Token: platform.SetupTokenProvider{TokenPath: a.config.SetupTokenPath, ExpiresPath: a.config.SetupTokenExpiresPath},
	}
	m8Handler := httpapi.M8Handler{Auth: setupHandler}
	platformHandler := httpapi.PlatformHandler{Auth: setupHandler, Enterprise: platform.EnterpriseService{Store: postgresStore, Idempotency: idempotency}, Cursor: cursorSigner, TrustBundles: bundles}
	machineService := identity.MachineService{Store: postgresStore, Idempotency: idempotency}
	enterpriseIdentityHandler := httpapi.EnterpriseIdentityHandler{Auth: setupHandler, Service: identity.EnterpriseService{Store: postgresStore, Idempotency: idempotency}, Machine: machineService, Cursor: cursorSigner}
	enterpriseAuthorizationHandler := httpapi.EnterpriseAuthorizationHandler{Identity: enterpriseIdentityHandler, Service: authorization.Service{Store: postgresStore, Idempotency: idempotency}, Cursor: cursorSigner}
	machineHandler := httpapi.MachineHandler{Identity: enterpriseIdentityHandler, Service: machineService, Cursor: cursorSigner}
	auditHandler := httpapi.AuditHandler{Auth: setupHandler, Enterprise: enterpriseIdentityHandler, Store: postgresStore, Cursor: cursorSigner}
	secretDomain := secretservice.Service{Store: postgresStore, Idempotency: idempotency, Keyring: secretKeyring}
	actionDomain := resource.PendingActionService{Store: postgresStore, Idempotency: idempotency, Key: a.config.PendingActionKey}
	artifactChecker, err := artifactcheck.NewHTTPChecker(a.config.OtelcolArtifactCABundle, a.config.ArtifactProbeBaseURL)
	if err != nil {
		return err
	}
	connectorDomain := connectorservice.Service{Store: postgresStore, Redis: redisClient, GatewayEndpoint: a.config.ConnectorGatewayAddress,
		EnrollmentURL:   a.config.ConnectorEnrollmentURL,
		Artifacts:       artifactChecker,
		Credentials:     secretDomain,
		TrustBundlePath: a.config.TrustBundlePath, TrustBundleEpoch: a.config.TrustBundleEpoch,
		TrustBundles:     bundles,
		BootstrapTLSMode: installinstruction.DownloadTLSMode(a.config.BootstrapTLSMode),
		KubernetesImage:  a.config.ConnectorKubernetesImage,
		Issuer: connectorservice.CertManagerIssuer{Client: kubernetesClient, Namespace: a.config.SystemNamespace,
			IssuerName: a.config.ConnectorIssuerName, IssuerKind: "ClusterIssuer", IssuerGeneration: int32(activeBundle.Epoch)}}
	bastionDomain, err := connectorservice.NewBastionService(postgresStore, actionDomain, connectorDomain,
		a.config.PendingActionKey, a.config.ConnectorEnrollForwardTarget, a.config.ConnectorGatewayForwardTarget)
	if err != nil {
		return err
	}
	var actionExtension resource.ActionExtension = bastionDomain
	selfEnroll := (*telemetryservice.SelfEnrollService)(nil)
	if a.config.TelemetryEnabled {
		identityService := telemetryservice.IdentityService{Store: postgresStore, TrustBundles: bundles}
		selfEnroll = &telemetryservice.SelfEnrollService{Store: postgresStore, Actions: actionDomain, Identity: identityService,
			EnrollmentEndpoint: a.config.TelemetryEnrollment, IngestGRPCEndpoint: a.config.TelemetryIngestGRPC,
			IngestHTTPEndpoint: a.config.TelemetryIngestHTTP, BootstrapSecretKey: a.config.PendingActionKey, Artifacts: artifactChecker,
			TrustBundles: bundles, TrustBundlePath: a.config.TrustBundlePath, TrustBundleEpoch: a.config.TrustBundleEpoch,
			BootstrapTLSMode: installinstruction.DownloadTLSMode(a.config.BootstrapTLSMode), InstallerSHA256: a.config.HostInstallerSHA256}
		actionExtension = telemetryservice.ActionExtension{Next: bastionDomain, Credentials: secretDomain,
			EnrollmentEndpoint: a.config.TelemetryEnrollment, IngestGRPCEndpoint: a.config.TelemetryIngestGRPC,
			IngestHTTPEndpoint: a.config.TelemetryIngestHTTP, TrustBundles: bundles, SelfEnroll: selfEnroll}
	}
	resourceDomain := resource.Service{Store: postgresStore, Actions: actionDomain, Access: resource.AccessService{},
		Direct: resource.DirectTargetValidator{DeniedCIDRs: deniedCIDRs}, Commands: connectorDomain, DirectCommands: directDispatcher, Extension: actionExtension,
		ClusterEnrollment: connectorDomain,
		Kubernetes: kubernetesreader.Reader{Store: postgresStore, Secrets: secretDomain, Validator: resource.DirectTargetValidator{DeniedCIDRs: deniedCIDRs},
			Notifier: connectorDomain}}
	workflowDomain := action.Service{Store: postgresStore, Idempotency: idempotency, Resources: resourceDomain,
		OneTimeResultKey: a.config.PendingActionKey}
	secretHandler := httpapi.SecretHandler{Identity: enterpriseIdentityHandler, Service: secretDomain}
	hostHandler := httpapi.HostHandler{Identity: enterpriseIdentityHandler, Service: resourceDomain, Queries: postgresStore.Queries, Onboarding: selfEnroll}
	kubernetesHandler := httpapi.KubernetesHandler{Identity: enterpriseIdentityHandler, Service: resourceDomain}
	connectionHandler := httpapi.ConnectionHandler{Identity: enterpriseIdentityHandler, Service: resourceDomain}
	actionHandler := httpapi.ResourceActionHandler{Identity: enterpriseIdentityHandler, Service: resourceDomain, Workflow: workflowDomain, Cursor: cursorSigner}
	workflowHandler := httpapi.WorkflowHandler{Identity: enterpriseIdentityHandler, Service: workflowDomain}
	conversationHandler := httpapi.ConversationHandler{Identity: enterpriseIdentityHandler,
		Service: conversation.Service{Store: postgresStore, Idempotency: idempotency}}
	modelHandler := httpapi.ModelHandler{Identity: enterpriseIdentityHandler,
		Service: modelservice.Service{Store: postgresStore, Idempotency: idempotency, Keyring: secretKeyring}}
	toolRegistry := mcp.NewRegistry()
	if err := (agent.ResourceTools{Store: postgresStore, Resources: resourceDomain}).Register(toolRegistry); err != nil {
		return err
	}
	cardDomain := cardservice.Service{Store: postgresStore, Idempotency: idempotency, Tools: toolRegistry, PresentationTTL: a.config.CardPresentationTTL,
		ValidationTTL: a.config.CardValidationTTL, RuntimeVersion: a.config.CardRuntimeVersion, MaxPresentation: a.config.CardMaxPresentationBytes}
	if err := cardDomain.RegisterRenderTool(toolRegistry); err != nil {
		return err
	}
	cardHandler := httpapi.CardHandler{Identity: enterpriseIdentityHandler, Service: cardDomain, Workflow: workflowDomain}
	sandboxHandler := httpapi.SandboxHandler{Auth: setupHandler, Service: sandbox.Service{Store: postgresStore, Keyring: secretKeyring}}
	connectorHandler := httpapi.ConnectorHandler{Identity: enterpriseIdentityHandler, Service: connectorDomain, Bastion: bastionDomain, Queries: postgresStore.Queries}
	remoteWebsocketURL, err := websocketURL(a.config.RemoteOrigin)
	if err != nil {
		return err
	}
	remoteAccessHandler := httpapi.RemoteAccessHandler{Identity: enterpriseIdentityHandler, WebsocketURL: remoteWebsocketURL,
		Cursor: cursorSigner,
		Service: remoteaccessservice.Service{Store: postgresStore, Idempotency: idempotency,
			Access: resource.AccessService{}, Keyring: secretKeyring, ObjectStore: objects, UserLimit: a.config.RemoteUserLimit,
			HostLimit: a.config.RemoteHostLimit, EnterpriseLimit: a.config.RemoteEnterpriseLimit}}
	var telemetryHandler *httpapi.TelemetryHandler
	if a.config.TelemetryEnabled {
		telemetryTLS, telemetryErr := telemetryservice.ClientTLSConfig(a.config.TelemetryClientCert, a.config.TelemetryClientKey, a.config.TelemetryCABundle, a.config.TelemetryServerName)
		if telemetryErr != nil {
			return telemetryErr
		}
		telemetryQuery, telemetryErr := telemetryservice.NewGRPCQueryBackend(a.config.TelemetryQueryEndpoint, telemetryTLS, a.logger)
		if telemetryErr != nil {
			return telemetryErr
		}
		defer telemetryQuery.Close()
		platformHandler.Enterprise.Telemetry = telemetryQuery
		telemetryDomain := telemetryservice.Service{Store: postgresStore, Access: resource.AccessService{}, Actions: actionDomain,
			Query: telemetryQuery, Engine: telemetryQuery, OtelcolKubernetesImage: a.config.OtelcolKubernetesImage}
		telemetryIdentity := telemetryservice.IdentityService{Store: postgresStore, TrustBundles: bundles, Issuer: connectorservice.CertManagerIssuer{
			Client: kubernetesClient, Namespace: a.config.SystemNamespace, IssuerName: a.config.TelemetryIssuerName, IssuerKind: "ClusterIssuer",
			RequestPrefix: "argus-telemetry-", SubjectLabel: "argus.io/telemetry-collector-id", IssuerGeneration: int32(activeBundle.Epoch),
			Usages: []string{"client auth"},
		}, ServerIssuer: connectorservice.CertManagerIssuer{
			Client: kubernetesClient, Namespace: a.config.SystemNamespace, IssuerName: a.config.TelemetryIssuerName, IssuerKind: "ClusterIssuer",
			RequestPrefix: "argus-telemetry-server-", SubjectLabel: "argus.io/telemetry-collector-id", IssuerGeneration: int32(activeBundle.Epoch),
			Usages: []string{"server auth"},
		}}
		if err := (telemetryservice.Tools{Service: telemetryDomain}).Register(toolRegistry); err != nil {
			return err
		}
		telemetryHandler = &httpapi.TelemetryHandler{Identity: enterpriseIdentityHandler, Service: telemetryDomain, CollectorIdentity: telemetryIdentity,
			IngestGRPCEndpoint: a.config.TelemetryIngestGRPC, IngestHTTPEndpoint: a.config.TelemetryIngestHTTP}
	}
	go (outbox.Relay{Store: postgresStore, Redis: redisClient, Logger: a.logger}).Run(ctx)
	server := &http.Server{
		Addr: a.config.Address,
		Handler: httpapi.NewRouterWithOptions(httpapi.RouterOptions{
			Logger: a.logger, PostgreSQL: postgresStore, Redis: redisClient, Setup: &setupHandler, M8: &m8Handler, Platform: &platformHandler,
			EnterpriseIdentity: &enterpriseIdentityHandler, EnterpriseAuthorization: &enterpriseAuthorizationHandler,
			Machine: &machineHandler, Audit: &auditHandler, Secret: &secretHandler, Host: &hostHandler, Kubernetes: &kubernetesHandler,
			Connection: &connectionHandler, ResourceAction: &actionHandler, Workflow: &workflowHandler, Conversation: &conversationHandler, Model: &modelHandler,
			Sandbox:        &sandboxHandler,
			AllowedOrigins: a.config.AllowedOrigins,
			Connector:      &connectorHandler,
			Card:           &cardHandler,
			RemoteAccess:   &remoteAccessHandler,
			Telemetry:      telemetryHandler,
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
