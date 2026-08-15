import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Pencil, PlugZap, TerminalSquare, Trash2, Unplug } from "lucide-react";
import {
  useApi,
  type BastionScope,
  type ConnectionTestResult,
  type Connector,
  type Environment,
  type Host,
  type HostConnectionStatus,
  type HostFilter,
  type MockApiClient,
} from "@argus/api-client";
import {
  Badge,
  Alert,
  Button,
  Card,
  CheckItem,
  CodeBlock,
  ConfirmDialog,
  Dialog,
  EmptyState,
  FilterBar,
  PageShell,
  StatusBadge,
} from "@argus/ui";
import "../styles/hosts.css";
import { AddHostWizard } from "../components/hosts/add-host-wizard";
import { CollectorInstallWizard } from "../components/hosts/components-tab";
import {
  AddBastionDrawer,
  BastionUninstallDialog,
  EditBastionDrawer,
  EditHostDrawer,
} from "../components/hosts/host-drawers";
import {
  ARGUS_EGRESS_IP,
  collectorTone,
  connectionPathKey,
  environmentTone,
  formatDateTime,
  hostStatusTone,
  scopeOf,
} from "../components/hosts/host-utils";

const ENVIRONMENTS: Environment[] = ["development", "staging", "production"];
const STATUSES: HostConnectionStatus[] = [
  "online",
  "offline",
  "onboarding",
  "degraded",
  "unknown",
];

/** 成员主机紧凑卡片：名称/IP/连接路径/状态/环境标签 + 行操作。 */
function HostTile({
  host,
  scopes,
  onEdit,
  onDelete,
  onTest,
  onCollectorAction,
}: {
  host: Host;
  scopes: BastionScope[];
  onEdit: (host: Host) => void;
  onDelete: (host: Host) => void;
  onTest: (host: Host) => void;
  onCollectorAction: (host: Host) => void;
}) {
  const { t } = useTranslation();
  const scope = scopeOf(host, scopes);
  const pathKey = connectionPathKey(host);
  const path = t(`hosts.path.${pathKey}`, {
    scope: scope?.name ?? host.bastionScopeId ?? "",
    address: `${host.address}:${host.port}`,
  });
  return (
    <div className="argus-host-tile">
      <div className="argus-host-tile__top">
        <span className="argus-host-tile__name">
          <Link params={{ hostId: host.id }} to="/hosts/$hostId">
            {host.name}
          </Link>
          <StatusBadge
            pulse={host.connectionStatus === "online"}
            tone={hostStatusTone(host.connectionStatus)}
          >
            {t(`hosts.status.${host.connectionStatus}`)}
          </StatusBadge>
        </span>
        <span className="argus-host-tile__addr">
          {host.address}:{host.port}
        </span>
      </div>
      <div className="argus-host-tile__path" title={path}>
        {path}
      </div>
      <div className="argus-host-tile__footer">
        <span className="argus-host-tile__tags">
          <Badge tone={environmentTone(host.environment)}>
            {t(`hosts.env.${host.environment}`)}
          </Badge>
          <button
            aria-label={t(
              host.collectorStatus === "not_installed"
                ? "hosts.row.installCollector"
                : "hosts.row.openCollector",
              { name: host.name },
            )}
            className="argus-collector-status-action"
            onClick={() => onCollectorAction(host)}
            type="button"
          >
            <StatusBadge tone={collectorTone(host.collectorStatus)}>
              {t(`hosts.collectorStatus.${host.collectorStatus}`)}
            </StatusBadge>
          </button>
        </span>
        <span className="argus-host-tile__actions">
          <Button
            aria-label={t("hosts.row.detail")}
            onClick={() => onTest(host)}
            size="icon"
            title={t("hosts.row.testConnection")}
            variant="ghost"
          >
            <PlugZap aria-hidden size={14} />
          </Button>
          <Button
            aria-label={t("hosts.row.edit")}
            onClick={() => onEdit(host)}
            size="icon"
            title={t("hosts.row.edit")}
            variant="ghost"
          >
            <Pencil aria-hidden size={14} />
          </Button>
          <Button
            aria-label={t("hosts.row.delete")}
            onClick={() => onDelete(host)}
            size="icon"
            title={t("hosts.row.delete")}
            variant="ghost"
          >
            <Trash2 aria-hidden size={14} />
          </Button>
        </span>
      </div>
    </div>
  );
}

/** 主机列表页：Bastion Scope 分组卡片 + 独立主机。 */
export function HostsPage() {
  const { t } = useTranslation();
  const api = useApi();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [search, setSearch] = useState("");
  const [envFilter, setEnvFilter] = useState("");
  const [statusFilter, setStatusFilter] = useState("");
  const [addBastionOpen, setAddBastionOpen] = useState(false);
  const [addHostOpen, setAddHostOpen] = useState(false);
  const [editBastion, setEditBastion] = useState<BastionScope | null>(null);
  const [uninstallBastion, setUninstallBastion] = useState<BastionScope | null>(
    null,
  );
  const [deleteBastion, setDeleteBastion] = useState<BastionScope | null>(null);
  const [deletingBastion, setDeletingBastion] = useState(false);
  const [deleteBastionError, setDeleteBastionError] = useState("");
  const [editHost, setEditHost] = useState<Host | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Host | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [testTarget, setTestTarget] = useState<Host | null>(null);
  const [testResult, setTestResult] = useState<ConnectionTestResult | null>(
    null,
  );
  const [testing, setTesting] = useState(false);
  const [regenerating, setRegenerating] = useState<string | null>(null);
  const [collectorInstallTarget, setCollectorInstallTarget] =
    useState<Host | null>(null);

  const filter: HostFilter = {
    query: search.trim() || undefined,
    environment: envFilter ? [envFilter as Environment] : undefined,
    status: statusFilter ? [statusFilter as HostConnectionStatus] : undefined,
  };

  const hostsQuery = useQuery({
    queryKey: ["hosts", filter],
    queryFn: () => api.hosts.list(filter),
  });
  const scopesQuery = useQuery({
    queryKey: ["bastion-scopes"],
    queryFn: () => api.connectors.listBastionScopes(),
  });
  const connectorsQuery = useQuery({
    queryKey: ["connectors"],
    queryFn: () => api.connectors.list(),
  });
  const activeSessionsQuery = useQuery({
    queryKey: ["remote-sessions", "active"],
    queryFn: () => api.hosts.listSessions({ status: ["active"] }),
  });

  const hosts = useMemo(() => hostsQuery.data?.items ?? [], [hostsQuery.data]);
  const scopes = useMemo(() => scopesQuery.data ?? [], [scopesQuery.data]);
  const connectors = useMemo(
    () => connectorsQuery.data?.items ?? [],
    [connectorsQuery.data],
  );
  const activeSessions = activeSessionsQuery.data ?? [];

  const invalidateAll = () => {
    void queryClient.invalidateQueries({ queryKey: ["hosts"] });
    void queryClient.invalidateQueries({ queryKey: ["bastion-scopes"] });
    void queryClient.invalidateQueries({ queryKey: ["connectors"] });
    void queryClient.invalidateQueries({ queryKey: ["remote-sessions"] });
  };

  const refresh = () => {
    void hostsQuery.refetch();
    void scopesQuery.refetch();
    void connectorsQuery.refetch();
    void activeSessionsQuery.refetch();
  };

  const connectorOf = (scope: BastionScope): Connector | undefined =>
    connectors.find((entry) => entry.id === scope.activeConnectorId);

  const collectorSummary = (members: Host[]): string => {
    if (members.length === 0) return t("hosts.scope.collectorEmpty");
    const counts = new Map<string, number>();
    for (const host of members) {
      counts.set(
        host.collectorStatus,
        (counts.get(host.collectorStatus) ?? 0) + 1,
      );
    }
    return [...counts.entries()]
      .map(
        ([status, count]) => `${count} ${t(`hosts.collectorStatus.${status}`)}`,
      )
      .join(" · ");
  };

  const runTest = async (host: Host) => {
    setTestTarget(host);
    setTestResult(null);
    setTesting(true);
    try {
      setTestResult(await api.hosts.testConnection(host.id));
    } finally {
      setTesting(false);
    }
  };

  const confirmDelete = async () => {
    if (!deleteTarget || deleting) return;
    setDeleting(true);
    try {
      await api.hosts.delete(deleteTarget.id);
      setDeleteTarget(null);
      invalidateAll();
    } finally {
      setDeleting(false);
    }
  };

  const regenerate = async (scopeId: string) => {
    setRegenerating(scopeId);
    try {
      await api.connectors.regenerateEnrollmentToken(scopeId);
      void scopesQuery.refetch();
    } finally {
      setRegenerating(null);
    }
  };

  // mock 客户端附带的演示辅助（真实 API 无此方法）。
  const simulate = (api as unknown as Partial<MockApiClient>).simulate;
  const simulateRegister = (scopeId: string) => {
    simulate?.connectorRegister(scopeId);
    invalidateAll();
  };
  const simulateUninstall = (scopeId: string, commandId: string) =>
    simulate?.connectorUninstall(scopeId, commandId) ?? {
      success: false,
      code: "command_missing" as const,
      message: t("hosts.bastionUninstall.commandFailed"),
    };

  // pending Scope 置顶，其余按创建时间排序。
  const sortedScopes = [...scopes].sort((a, b) => {
    if (a.status === "pending" && b.status !== "pending") return -1;
    if (b.status === "pending" && a.status !== "pending") return 1;
    return a.createdAt.localeCompare(b.createdAt);
  });
  const bastionHostOf = (scope: BastionScope): Host | undefined =>
    hosts.find((host) => host.id === scope.connectorHostId);
  const membersOf = (scope: BastionScope): Host[] =>
    hosts.filter(
      (host) =>
        host.bastionScopeId === scope.id && host.id !== scope.connectorHostId,
    );
  const standaloneHosts = hosts.filter((host) => !host.bastionScopeId);
  const activeScopes = scopes.filter(
    (scope) =>
      scope.status === "active" && connectorOf(scope)?.status === "online",
  );
  const deleteBastionMembers = deleteBastion ? membersOf(deleteBastion) : [];

  const confirmDeleteBastion = async () => {
    if (!deleteBastion || deleteBastionMembers.length > 0 || deletingBastion) {
      return;
    }
    setDeletingBastion(true);
    setDeleteBastionError("");
    try {
      await api.connectors.deleteBastionScope(deleteBastion.id);
      setDeleteBastion(null);
      invalidateAll();
    } catch (error) {
      setDeleteBastionError(
        error instanceof Error
          ? error.message
          : t("hosts.bastionDelete.failed"),
      );
    } finally {
      setDeletingBastion(false);
    }
  };

  const openCollector = (host: Host) => {
    if (host.collectorStatus === "not_installed") {
      setCollectorInstallTarget(host);
      return;
    }
    void navigate({
      to: "/hosts/$hostId",
      params: { hostId: host.id },
      hash: "otlp-collector",
    });
  };

  return (
    <PageShell
      actions={
        <>
          <Button onClick={() => setAddBastionOpen(true)} variant="secondary">
            {t("hosts.addBastion")}
          </Button>
          <Button onClick={() => setAddHostOpen(true)} variant="primary">
            {t("hosts.addHost")}
          </Button>
        </>
      }
      description={t("hosts.description")}
      title={t("hosts.title")}
    >
      <div className="argus-hosts-stack">
        <FilterBar
          filters={[
            {
              key: "environment",
              value: envFilter,
              allLabel: t("hosts.filter.allEnvs"),
              options: ENVIRONMENTS.map((env) => ({
                value: env,
                label: t(`hosts.env.${env}`),
              })),
              onChange: setEnvFilter,
            },
            {
              key: "status",
              value: statusFilter,
              allLabel: t("hosts.filter.allStatuses"),
              options: STATUSES.map((status) => ({
                value: status,
                label: t(`hosts.status.${status}`),
              })),
              onChange: setStatusFilter,
            },
          ]}
          onRefresh={refresh}
          refreshing={hostsQuery.isFetching}
          search={{
            value: search,
            onChange: setSearch,
            placeholder: t("hosts.filter.searchPlaceholder"),
          }}
        />

        {sortedScopes.map((scope) => {
          const members = membersOf(scope);
          const bastionHost = bastionHostOf(scope);
          const connector = connectorOf(scope);
          const sessionCount = activeSessions.filter(
            (session) => session.bastionScopeId === scope.id,
          ).length;

          if (scope.status === "pending") {
            const token = scope.registrationToken;
            return (
              <Card
                className="argus-scope-card argus-scope-card--pending"
                key={scope.id}
              >
                <div className="argus-scope-card__head">
                  <span className="argus-scope-card__title">
                    {scope.name}
                    <Badge tone={environmentTone(scope.environment)}>
                      {t(`hosts.env.${scope.environment}`)}
                    </Badge>
                    <StatusBadge pulse tone="info">
                      {t("hosts.scope.waiting")}
                    </StatusBadge>
                  </span>
                </div>
                <div className="argus-scope-card__body">
                  <p className="argus-muted">{t("hosts.scope.waitingDesc")}</p>
                  {token && (
                    <div className="argus-scope-card__token">
                      <CodeBlock code={token.installCommand} language="bash" />
                      <span className="argus-scope-card__token-meta">
                        {t("hosts.scope.tokenExpires", {
                          time: formatDateTime(token.expiresAt),
                        })}
                      </span>
                    </div>
                  )}
                  <div className="argus-scope-card__actions">
                    <Button
                      loading={regenerating === scope.id}
                      onClick={() => void regenerate(scope.id)}
                      variant="secondary"
                    >
                      {t("hosts.scope.regenerateToken")}
                    </Button>
                    {simulate && (
                      <Button
                        onClick={() => simulateRegister(scope.id)}
                        variant="ghost"
                      >
                        <TerminalSquare aria-hidden size={14} />
                        {t("hosts.scope.simulateOnline")}（
                        {t("hosts.scope.demoOnly")}）
                      </Button>
                    )}
                  </div>
                </div>
              </Card>
            );
          }

          return (
            <Card className="argus-scope-card" key={scope.id}>
              <div className="argus-scope-card__head">
                <span className="argus-scope-card__title">
                  {bastionHost ? (
                    <Link
                      params={{ hostId: bastionHost.id }}
                      to="/hosts/$hostId"
                    >
                      {scope.name}
                    </Link>
                  ) : (
                    scope.name
                  )}
                  <Badge tone={environmentTone(scope.environment)}>
                    {t(`hosts.env.${scope.environment}`)}
                  </Badge>
                  <Badge tone="info">{t("hosts.scope.bastionHost")}</Badge>
                  {connector && (
                    <StatusBadge
                      pulse={connector.status === "online"}
                      tone={
                        connector.status === "online" ? "success" : "danger"
                      }
                    >
                      {connector.status === "online"
                        ? t("hosts.scope.connectorOnline")
                        : connector.status === "uninstalled"
                          ? t("hosts.scope.connectorUninstalled")
                          : t("hosts.scope.connectorOffline")}
                    </StatusBadge>
                  )}
                  {scope.status === "uninstalled" && (
                    <StatusBadge tone="warning">
                      {t("hosts.scope.connectorUninstalled")}
                    </StatusBadge>
                  )}
                  {scope.status === "uninstalling" && (
                    <StatusBadge pulse tone="warning">
                      {t("hosts.scope.connectorUninstalling")}
                    </StatusBadge>
                  )}
                  {bastionHost && (
                    <>
                      <button
                        aria-label={t(
                          bastionHost.collectorStatus === "not_installed"
                            ? "hosts.row.installCollector"
                            : "hosts.row.openCollector",
                          { name: bastionHost.name },
                        )}
                        className="argus-collector-status-action"
                        onClick={() => openCollector(bastionHost)}
                        type="button"
                      >
                        <StatusBadge
                          tone={collectorTone(bastionHost.collectorStatus)}
                        >
                          {t(
                            `hosts.collectorStatus.${bastionHost.collectorStatus}`,
                          )}
                        </StatusBadge>
                      </button>
                      <span className="argus-scope-card__title-actions">
                        <Button
                          aria-label={t("hosts.row.testConnection")}
                          onClick={() => void runTest(bastionHost)}
                          size="icon"
                          title={t("hosts.row.testConnection")}
                          variant="ghost"
                        >
                          <PlugZap aria-hidden size={14} />
                        </Button>
                        <Button
                          aria-label={t("hosts.row.edit")}
                          onClick={() => setEditBastion(scope)}
                          size="icon"
                          title={t("hosts.row.edit")}
                          variant="ghost"
                        >
                          <Pencil aria-hidden size={14} />
                        </Button>
                        {connector?.status === "online" &&
                          scope.status === "active" && (
                            <Button
                              aria-label={t("hosts.bastionUninstall.action")}
                              onClick={() => setUninstallBastion(scope)}
                              size="icon"
                              title={t("hosts.bastionUninstall.action")}
                              variant="ghost"
                            >
                              <Unplug aria-hidden size={14} />
                            </Button>
                          )}
                        {(connector?.status === "offline" ||
                          scope.status === "uninstalled") && (
                          <Button
                            aria-label={t("hosts.bastionDelete.action")}
                            onClick={() => {
                              setDeleteBastionError("");
                              setDeleteBastion(scope);
                            }}
                            size="icon"
                            title={t("hosts.bastionDelete.action")}
                            variant="ghost"
                          >
                            <Trash2 aria-hidden size={14} />
                          </Button>
                        )}
                      </span>
                    </>
                  )}
                </span>
                <span className="argus-scope-card__meta">
                  <span>
                    {t("hosts.scope.members", { count: members.length })}
                  </span>
                  <span>
                    {t("hosts.scope.activeSessions", { count: sessionCount })}
                  </span>
                  <span>
                    {t("hosts.scope.collectorSummary", {
                      summary: collectorSummary(members),
                    })}
                  </span>
                </span>
              </div>
              <div className="argus-scope-card__body">
                {members.length > 0 ? (
                  <div className="argus-host-grid">
                    {members.map((host) => (
                      <HostTile
                        host={host}
                        key={host.id}
                        onCollectorAction={openCollector}
                        onDelete={setDeleteTarget}
                        onEdit={setEditHost}
                        onTest={(target) => void runTest(target)}
                        scopes={scopes}
                      />
                    ))}
                  </div>
                ) : (
                  <span className="argus-muted">
                    {t("hosts.scope.members", { count: 0 })}
                  </span>
                )}
              </div>
            </Card>
          );
        })}

        {standaloneHosts.length > 0 && (
          <Card className="argus-scope-card">
            <div className="argus-scope-card__head">
              <span className="argus-scope-card__title">
                {t("hosts.standalone.title")}
                <Badge tone="accent">
                  {t("hosts.standalone.directExecutor")}
                </Badge>
              </span>
              <span className="argus-standalone__hint">
                {t("hosts.standalone.egressHint", { ip: ARGUS_EGRESS_IP })}
              </span>
            </div>
            <div className="argus-scope-card__body">
              <div className="argus-host-grid">
                {standaloneHosts.map((host) => (
                  <HostTile
                    host={host}
                    key={host.id}
                    onCollectorAction={openCollector}
                    onDelete={setDeleteTarget}
                    onEdit={setEditHost}
                    onTest={(target) => void runTest(target)}
                    scopes={scopes}
                  />
                ))}
              </div>
            </div>
          </Card>
        )}

        {!hostsQuery.isLoading && hosts.length === 0 && scopes.length === 0 && (
          <EmptyState
            description={t("hosts.empty.description")}
            title={t("hosts.empty.title")}
          />
        )}
      </div>

      <AddBastionDrawer
        onCreated={invalidateAll}
        onOpenChange={setAddBastionOpen}
        open={addBastionOpen}
      />
      <AddHostWizard
        onCreated={invalidateAll}
        onOpenChange={setAddHostOpen}
        open={addHostOpen}
        scopes={activeScopes}
      />
      <EditBastionDrawer
        connectorStatus={
          editBastion ? connectorOf(editBastion)?.status : undefined
        }
        onOpenChange={(open) => {
          if (!open) setEditBastion(null);
        }}
        onSaved={invalidateAll}
        scope={editBastion}
      />
      <BastionUninstallDialog
        onChanged={invalidateAll}
        onOpenChange={(open) => {
          if (!open) setUninstallBastion(null);
        }}
        onSimulate={simulate ? simulateUninstall : undefined}
        scope={uninstallBastion}
      />
      <EditHostDrawer
        host={editHost}
        onOpenChange={(open) => {
          if (!open) setEditHost(null);
        }}
        onSaved={invalidateAll}
      />
      {collectorInstallTarget && (
        <CollectorInstallWizard
          host={collectorInstallTarget}
          onInstalled={invalidateAll}
          onOpenChange={(open) => {
            if (!open) setCollectorInstallTarget(null);
          }}
          open
          scopes={activeScopes}
        />
      )}
      <ConfirmDialog
        danger
        description={t("hosts.delete.description")}
        confirmLabel={t("hosts.delete.confirm")}
        loading={deleting}
        onConfirm={() => void confirmDelete()}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null);
        }}
        open={deleteTarget !== null}
        title={t("hosts.delete.title")}
      >
        {deleteTarget && (
          <p className="argus-mono">
            {deleteTarget.name}（{deleteTarget.address}:{deleteTarget.port}）
          </p>
        )}
      </ConfirmDialog>
      <Dialog
        description={t("hosts.bastionDelete.description")}
        footer={
          <>
            <Button
              disabled={deletingBastion}
              onClick={() => setDeleteBastion(null)}
              variant="secondary"
            >
              {t("hosts.cancel")}
            </Button>
            <Button
              disabled={deleteBastionMembers.length > 0}
              loading={deletingBastion}
              onClick={() => void confirmDeleteBastion()}
              variant="danger"
            >
              {t("hosts.bastionDelete.confirm")}
            </Button>
          </>
        }
        onOpenChange={(open) => {
          if (!open) setDeleteBastion(null);
        }}
        open={deleteBastion !== null}
        title={t("hosts.bastionDelete.title", {
          name: deleteBastion?.name ?? "",
        })}
      >
        {deleteBastionMembers.length > 0 ? (
          <Alert
            description={t("hosts.bastionDelete.membersBlocked", {
              count: deleteBastionMembers.length,
            })}
            title={t("hosts.bastionDelete.membersBlockedTitle")}
            tone="warning"
          />
        ) : (
          <Alert
            description={t("hosts.bastionDelete.ready")}
            title={t("hosts.bastionDelete.readyTitle")}
            tone="danger"
          />
        )}
        {deleteBastionError && (
          <Alert
            description={deleteBastionError}
            title={t("hosts.bastionDelete.failed")}
            tone="danger"
          />
        )}
      </Dialog>
      <Dialog
        onOpenChange={(open) => {
          if (!open) {
            setTestTarget(null);
            setTestResult(null);
          }
        }}
        open={testTarget !== null}
        title={t("hosts.test.title", { name: testTarget?.name ?? "" })}
      >
        {testing && <p className="argus-muted">{t("common.loading")}</p>}
        {testResult && (
          <div className="argus-detail-section">
            <StatusBadge tone={testResult.success ? "success" : "danger"}>
              {testResult.success
                ? `${t("hosts.test.success")} · ${t("hosts.test.latency", { ms: testResult.latencyMs })}`
                : t("hosts.test.failed")}
            </StatusBadge>
            {testResult.checks.map((check) => (
              <CheckItem checked={check.status === "passed"} key={check.name}>
                <span className="argus-mono">{check.name}</span>
                {check.detail && (
                  <span className="argus-muted"> · {check.detail}</span>
                )}
                {check.status === "skipped" && (
                  <span className="argus-muted"> · skipped</span>
                )}
              </CheckItem>
            ))}
          </div>
        )}
      </Dialog>
    </PageShell>
  );
}
