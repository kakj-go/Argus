import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import {
  useApi,
  type BastionScope,
  type CollectorInstance,
  type Host,
  type PendingActionPublic,
} from "@argus/api-client";
import {
  Badge,
  ActionGroup,
  Button,
  Card,
  CardContent,
  CardHeader,
  DiffViewer,
  Field,
  FormDrawer,
  KeyValueGrid,
  PreviewCommitCard,
  Select,
  StatusBadge,
  Switch,
  Wizard,
  type PreviewCommitStatus,
} from "@argus/ui";
import { PendingActionConfirm } from "./pending-action-confirm";
import {
  collectorTone,
  formatDateTime,
  scopeOf,
  telemetryRouteOf,
} from "./host-utils";

/** 采集能力开关键（docs/09 §5.1 的 Collection Profile 草稿开关）。 */
const CAPABILITY_KEYS = [
  "hostBasic",
  "systemLogs",
  "fileLogs",
  "collectorSelf",
  "prometheus",
  "otlp",
] as const;
type CapabilityKey = (typeof CAPABILITY_KEYS)[number];

function capabilitiesFromProfile(
  profile: string,
): Record<CapabilityKey, boolean> {
  const parts = profile.split(",").map((part) => part.trim());
  return {
    hostBasic: parts.includes("host-basic"),
    systemLogs: parts.includes("linux-journald"),
    fileLogs: parts.includes("file-log"),
    collectorSelf: parts.includes("collector-self"),
    prometheus: parts.includes("prometheus-endpoint"),
    otlp: parts.includes("otlp-receiver"),
  };
}

function profileFromCapabilities(caps: Record<CapabilityKey, boolean>): string {
  const parts: string[] = [];
  if (caps.hostBasic) parts.push("host-basic");
  if (caps.systemLogs) parts.push("linux-journald");
  if (caps.fileLogs) parts.push("file-log");
  if (caps.collectorSelf) parts.push("collector-self");
  if (caps.prometheus) parts.push("prometheus-endpoint");
  if (caps.otlp) parts.push("otlp-receiver");
  return parts.join(",") || "host-basic";
}

const COLLECTOR_PROFILES = [
  "host-basic",
  "host-basic,linux-journald",
  "host-basic,linux-journald,collector-self",
];

/** Connector 卡片：仅堡垒机（connector_local）主机显示。 */
function ConnectorCard({
  host,
  onChanged,
}: {
  host: Host;
  onChanged: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const [rotating, setRotating] = useState(false);
  const [previewingUninstall, setPreviewingUninstall] = useState(false);
  const [uninstallAction, setUninstallAction] =
    useState<PendingActionPublic | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [rotateStatus, setRotateStatus] =
    useState<PreviewCommitStatus>("pending");

  const connectorsQuery = useQuery({
    queryKey: ["connectors"],
    queryFn: () => api.connectors.list(),
  });
  const connector = (connectorsQuery.data?.items ?? []).find(
    (entry) => entry.id === host.connector_id || entry.host_id === host.id,
  );
  if (!connector) return null;

  const rotate = async () => {
    if (confirming) return;
    setConfirming(true);
    try {
      // rotateCertificate 为单步写操作（无独立 PendingActionPublic），
      // 用 PreviewCommitCard 呈现确认流程后直接执行。
      await api.connectors.rotateCertificate(connector.id);
      setRotateStatus("success");
      onChanged();
    } catch {
      setRotateStatus("failed");
    } finally {
      setConfirming(false);
    }
  };

  const previewUninstall = async () => {
    if (previewingUninstall) return;
    setPreviewingUninstall(true);
    try {
      setRotating(false);
      setUninstallAction(
        await api.connectors.previewUninstallConnector(
          connector.id,
          connector.version,
        ),
      );
    } finally {
      setPreviewingUninstall(false);
    }
  };

  return (
    <Card>
      <CardHeader
        action={
          !rotating &&
          !uninstallAction && (
            <ActionGroup>
              <Button
                onClick={() => {
                  setRotating(true);
                  setRotateStatus("pending");
                }}
                variant="secondary"
              >
                {t("hosts.components.rotateCert")}
              </Button>
              {connector.status === "online" ? (
                <Button
                  loading={previewingUninstall}
                  onClick={() => void previewUninstall()}
                  variant="danger"
                >
                  {t("hosts.components.uninstall")}
                </Button>
              ) : null}
            </ActionGroup>
          )
        }
        title={
          <>
            {t("hosts.components.connectorTitle")}{" "}
            <StatusBadge
              pulse={connector.status === "online"}
              tone={connector.status === "online" ? "success" : "danger"}
            >
              {connector.status === "online"
                ? t("hosts.scope.connectorOnline")
                : t("hosts.scope.connectorOffline")}
            </StatusBadge>
          </>
        }
      />
      <CardContent className="argus-detail-section">
        <KeyValueGrid
          columns={3}
          items={[
            {
              label: t("hosts.components.connectorTitle"),
              value: <span className="argus-mono">{connector.name}</span>,
            },
            {
              label: t("hosts.components.version", {
                version: connector.version,
              }),
              value: (
                <span className="argus-mono">
                  epoch {connector.connection_epoch}
                </span>
              ),
            },
            {
              label: t("hosts.components.certExpires", {
                time: formatDateTime(connector.certificate_expires_at),
              }),
              value: formatDateTime(connector.last_heartbeat_at),
            },
          ]}
        />
        {rotating && (
          <PreviewCommitCard
            confirming={confirming}
            diff={[
              {
                type: "context",
                content: `~ certificate ${connector.name} → +90d`,
              },
            ]}
            onCancel={() => setRotating(false)}
            onConfirm={() => void rotate()}
            resultMessage={
              rotateStatus === "success"
                ? `${t("hosts.components.rotated")} · ${t("hosts.components.certExpires", { time: formatDateTime(connector.certificate_expires_at) })}`
                : undefined
            }
            risk="write"
            status={rotateStatus}
            title={t("hosts.components.rotateTitle")}
          >
            <p className="argus-muted">{t("hosts.components.rotateDesc")}</p>
          </PreviewCommitCard>
        )}
        {uninstallAction ? (
          <PendingActionConfirm
            action={uninstallAction}
            onCancel={() => setUninstallAction(null)}
            onDone={() => {
              setUninstallAction(null);
              void connectorsQuery.refetch();
              onChanged();
            }}
          />
        ) : null}
      </CardContent>
    </Card>
  );
}

/** Collector 安装向导：Profile 选择 → 推送路由（含路由测试）→ 预览确认。 */
export function CollectorInstallWizard({
  host,
  scopes,
  onOpenChange,
  onInstalled,
  open,
}: {
  host: Host;
  scopes: BastionScope[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onInstalled: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const [step, setStep] = useState(0);
  const [profile, setProfile] = useState(COLLECTOR_PROFILES[0]!);
  const [route, setRoute] = useState("direct_argus");
  const [pendingAction, setPendingAction] =
    useState<PendingActionPublic | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const distributionsQuery = useQuery({
    queryKey: ["telemetry", "distributions"],
    queryFn: () => api.telemetry.listDistributions(),
  });
  const profilesQuery = useQuery({
    queryKey: ["telemetry", "profiles"],
    queryFn: () => api.telemetry.listProfiles(),
  });
  const collectorsQuery = useQuery({
    queryKey: ["telemetry", "collectors"],
    queryFn: () => api.telemetry.listCollectors(),
  });
  const platform =
    host.platform === "windows" ? "windows_amd64" : "linux_arm64";
  const distribution = distributionsQuery.data?.find(
    (item) =>
      item.support_status === "supported" &&
      item.artifacts.some((artifact) => artifact.platform === platform),
  );

  const scope = scopeOf(host, scopes);
  const gatewayScopes =
    host.connection_mode === "connector_local"
      ? scopes.filter(
          (entry) =>
            entry.status === "active" &&
            entry.id !== host.bastion_scope_id &&
            Boolean(entry.connector_host_id),
        )
      : scope
        ? [scope]
        : [];

  const close = (next: boolean) => {
    if (!next) {
      setStep(0);
      setProfile(COLLECTOR_PROFILES[0]!);
      setRoute("direct_argus");
      setPendingAction(null);
    }
    onOpenChange(next);
  };

  const submit = async () => {
    if (submitting || !distribution) return;
    const profileIds = profile
      .split(",")
      .map((key) => profilesQuery.data?.find((item) => item.key === key)?.id)
      .filter((id): id is string => Boolean(id));
    if (profileIds.length === 0) return;
    const gatewayCollector =
      route === "direct_argus"
        ? undefined
        : collectorsQuery.data?.find((item) => item.resource_id === route);
    setSubmitting(true);
    try {
      setPendingAction(
        await api.hosts.previewCollectorInstall(host.id, {
          distribution_version_id: distribution.id,
          profile_ids: profileIds,
          route_kind:
            route === "direct_argus" ? "direct_argus" : "bastion_gateway",
          gateway_collector_id: gatewayCollector?.id,
        }),
      );
    } finally {
      setSubmitting(false);
    }
  };

  const profileLabels = [
    t("hosts.components.installWizard.profileBasic"),
    t("hosts.components.installWizard.profileLogs"),
    t("hosts.components.installWizard.profileFull"),
  ];

  return (
    <FormDrawer
      footer={<></>}
      onOpenChange={close}
      open={open}
      title={t("hosts.components.installWizard.title", { name: host.name })}
      width={560}
    >
      {pendingAction ? (
        <PendingActionConfirm
          action={pendingAction}
          onCancel={() => setPendingAction(null)}
          onDone={() => {
            onInstalled();
            close(false);
          }}
        />
      ) : (
        <Wizard
          canNext={step === 0 ? Boolean(profile && distribution) : true}
          current={step}
          onBack={() => setStep(0)}
          onNext={() => setStep(1)}
          onSubmit={() => void submit()}
          steps={[
            {
              id: "profile",
              title: t("hosts.components.installWizard.stepProfile"),
              description: t("hosts.components.installWizard.stepProfileDesc"),
            },
            {
              id: "route",
              title: t("hosts.components.installWizard.stepRoute"),
              description: t("hosts.components.installWizard.stepRouteDesc"),
            },
          ]}
          submitLabel={t("hosts.components.installWizard.submit")}
          submitting={submitting}
        >
          {step === 0 ? (
            <div className="argus-choice-list">
              {COLLECTOR_PROFILES.map((item, index) => (
                <button
                  className={`argus-choice ${profile === item ? "is-selected" : ""}`}
                  key={item}
                  onClick={() => setProfile(item)}
                  type="button"
                >
                  <span className="argus-choice__text">
                    <b>{profileLabels[index]}</b>
                    <small className="argus-mono">{item}</small>
                  </span>
                </button>
              ))}
            </div>
          ) : (
            <div className="argus-choice-list">
              <button
                className={`argus-choice ${route === "direct_argus" ? "is-selected" : ""}`}
                onClick={() => {
                  setRoute("direct_argus");
                }}
                type="button"
              >
                <span className="argus-choice__text">
                  <b>{t("hosts.components.installWizard.routeDirect")}</b>
                  <small>
                    {t("hosts.components.installWizard.routeDirectDesc")}
                  </small>
                </span>
              </button>
              {gatewayScopes.map((gatewayScope) => {
                const value = gatewayScope.connector_host_id ?? gatewayScope.id;
                return (
                  <button
                    className={`argus-choice ${route === value ? "is-selected" : ""}`}
                    key={gatewayScope.id}
                    onClick={() => {
                      setRoute(value);
                    }}
                    type="button"
                  >
                    <span className="argus-choice__text">
                      <b>{t("hosts.components.installWizard.routeGateway")}</b>
                      <small>
                        {t("hosts.components.installWizard.routeGatewayDesc", {
                          scope: gatewayScope.name,
                        })}
                      </small>
                    </span>
                  </button>
                );
              })}
            </div>
          )}
        </Wizard>
      )}
    </FormDrawer>
  );
}

/** Collector 卡片：未安装 → 安装向导；已安装 → 状态 + 采集能力 + 推送路由。 */
function CollectorCard({
  host,
  scopes,
  onChanged,
}: {
  host: Host;
  scopes: BastionScope[];
  onChanged: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const [installOpen, setInstallOpen] = useState(false);
  const [draft, setDraft] = useState<Record<CapabilityKey, boolean> | null>(
    null,
  );
  const [configAction, setConfigAction] = useState<PendingActionPublic | null>(
    null,
  );
  const [routeAction, setRouteAction] = useState<PendingActionPublic | null>(
    null,
  );
  const [lifecycleAction, setLifecycleAction] =
    useState<PendingActionPublic | null>(null);
  const [routeChoice, setRouteChoice] = useState("direct_argus");
  const [routeTested, setRouteTested] = useState(false);
  const [routeTesting, setRouteTesting] = useState(false);
  const [routeEditing, setRouteEditing] = useState(false);
  const [previewing, setPreviewing] = useState(false);

  const collectorQuery = useQuery({
    queryKey: ["host-collector", host.id],
    queryFn: () => api.hosts.getCollector(host.id),
  });
  const collector: CollectorInstance | null = collectorQuery.data ?? null;
  const profilesQuery = useQuery({
    queryKey: ["telemetry", "profiles"],
    queryFn: () => api.telemetry.listProfiles(),
  });
  const distributionsQuery = useQuery({
    queryKey: ["telemetry", "distributions"],
    queryFn: () => api.telemetry.listDistributions(),
  });
  const claimsQuery = useQuery({
    queryKey: ["telemetry", "claims", host.id],
    queryFn: () => api.telemetry.listClaims(host.id),
    enabled: Boolean(collector),
  });
  const routesQuery = useQuery({
    queryKey: ["telemetry", "routes"],
    queryFn: () => api.telemetry.listRoutes(),
    enabled: Boolean(collector),
  });
  const collectorsQuery = useQuery({
    queryKey: ["telemetry", "collectors"],
    queryFn: () => api.telemetry.listCollectors(),
  });
  const currentRoute = routesQuery.data?.find(
    (entry) => entry.collector_id === collector?.id,
  );

  const scope = scopeOf(host, scopes);
  const gatewayScopes =
    host.connection_mode === "connector_local"
      ? scopes.filter(
          (entry) =>
            entry.status === "active" &&
            entry.id !== host.bastion_scope_id &&
            Boolean(entry.connector_host_id),
        )
      : scope
        ? [scope]
        : [];

  const activeProfileIds = useMemo(
    () => [
      ...new Set(
        (claimsQuery.data ?? [])
          .filter((claim) => claim.collector_id === collector?.id && claim.status === "active")
          .map((claim) => claim.profile_id)
          .filter((id): id is string => Boolean(id)),
      ),
    ],
    [claimsQuery.data, collector?.id],
  );
  const activeProfileKeys = useMemo(
    () => (profilesQuery.data ?? [])
      .filter((profile) => activeProfileIds.includes(profile.id))
      .map((profile) => profile.key),
    [profilesQuery.data, activeProfileIds],
  );
  const current = useMemo(
    () => (collector ? capabilitiesFromProfile(activeProfileKeys.join(",")) : null),
    [collector, activeProfileKeys],
  );
  const effective = draft ?? current;
  const dirty =
    current !== null &&
    effective !== null &&
    CAPABILITY_KEYS.some((key) => current[key] !== effective[key]);

  const diffLines =
    current && effective
      ? CAPABILITY_KEYS.filter((key) => current[key] !== effective[key]).map(
          (key) => ({
            type: effective[key] ? ("add" as const) : ("remove" as const),
            content: `${effective[key] ? "+" : "-"} ${t(`hosts.components.installed.cap.${key}`)}`,
          }),
        )
      : [];

  const previewConfig = async () => {
    if (!effective || !collector || previewing) return;
    const keys = profileFromCapabilities(effective).split(",");
    const profileIds = (profilesQuery.data ?? [])
      .filter((profile) => keys.includes(profile.key))
      .map((profile) => profile.id);
    if (profileIds.length === 0) return;
    setPreviewing(true);
    try {
      setConfigAction(
        await api.hosts.previewCollectorAction(host.id, "configure", {
          distribution_version_id: collector.distribution_version_id,
          profile_ids: profileIds,
          route_kind: currentRoute?.kind ?? "direct_argus",
          gateway_collector_id: currentRoute?.gateway_collector_id,
          expected_version: collector.version,
        }),
      );
    } finally {
      setPreviewing(false);
    }
  };

  const previewRoute = async () => {
    if (previewing || !collector || activeProfileIds.length === 0) return;
    const gatewayCollectorId = routeChoice === "direct_argus" ? undefined : routeChoice;
    setPreviewing(true);
    try {
      setRouteAction(
        await api.hosts.previewCollectorAction(host.id, "configure", {
          distribution_version_id: collector.distribution_version_id,
          profile_ids: activeProfileIds,
          route_kind: gatewayCollectorId ? "bastion_gateway" : "direct_argus",
          gateway_collector_id: gatewayCollectorId,
          expected_version: collector.version,
        }),
      );
    } finally {
      setPreviewing(false);
    }
  };

  const testCurrentRoute = async () => {
    if (!collector || !currentRoute || routeTesting) return;
    setRouteTesting(true);
    setRouteTested(false);
    try {
      const result = await api.telemetry.testRoute({
        collector_id: collector.id,
        route_kind: currentRoute.kind,
        gateway_collector_id: currentRoute.gateway_collector_id,
      });
      setRouteTested(result.status === "succeeded");
    } finally {
      setRouteTesting(false);
    }
  };

  const previewLifecycle = async (action: "upgrade" | "repair" | "uninstall") => {
    if (!collector || activeProfileIds.length === 0 || previewing) return;
    const distribution = action === "upgrade"
      ? distributionsQuery.data?.find((item) => item.support_status === "supported" && item.artifacts.some((artifact) => artifact.platform === collector.platform))
      : undefined;
    setPreviewing(true);
    try {
      setLifecycleAction(await api.hosts.previewCollectorAction(host.id, action, {
        distribution_version_id: distribution?.id ?? collector.distribution_version_id,
        profile_ids: activeProfileIds,
        route_kind: currentRoute?.kind ?? "direct_argus",
        gateway_collector_id: currentRoute?.gateway_collector_id,
        expected_version: collector.version,
      }));
    } finally {
      setPreviewing(false);
    }
  };

  if (collectorQuery.isLoading) return null;

  if (!collector) {
    return (
      <Card>
        <CardHeader
          action={
            <Button onClick={() => setInstallOpen(true)} variant="primary">
              {t("hosts.components.installCollector")}
            </Button>
          }
          title={
            <>
              {t("hosts.components.collectorTitle")}{" "}
              <StatusBadge tone="neutral">
                {t("hosts.components.collectorNotInstalled")}
              </StatusBadge>
            </>
          }
        />
        <CardContent>
          <p className="argus-muted">
            {t("hosts.components.collectorNotInstalledDesc")}
          </p>
        </CardContent>
        <CollectorInstallWizard
          host={host}
          onInstalled={onChanged}
          onOpenChange={setInstallOpen}
          open={installOpen}
          scopes={scopes}
        />
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader
        title={
          <>
            {t("hosts.components.collectorTitle")}{" "}
            <StatusBadge
              pulse={collector.status === "converged"}
              tone={collectorTone(collector.status)}
            >
              {t(`hosts.collectorStatus.${collector.status}`)}
            </StatusBadge>
          </>
        }
      />
      <CardContent className="argus-detail-section">
        <KeyValueGrid
          columns={3}
          items={[
            {
              label: t("hosts.components.installed.profile"),
              value: (
                <span className="argus-mono">
                  {collector.distribution_version_id}
                </span>
              ),
            },
            {
              label: t("hosts.components.version", {
                version: collector.platform,
              }),
              value: collector.role,
            },
            {
              label: t("hosts.components.installed.revision"),
              value: t("hosts.components.installed.revisionValue", {
                effective: collector.effective_revision,
                desired: collector.desired_revision,
              }),
            },
          ]}
        />

        <section className="argus-detail-section">
          <h3 className="argus-detail-section__title">
            {t("hosts.components.installed.capabilities")}
          </h3>
          <p className="argus-muted">
            {t("hosts.components.installed.capabilitiesDesc")}
          </p>
          <div>
            {CAPABILITY_KEYS.map((key) => (
              <div className="argus-capability" key={key}>
                <span className="argus-capability__label">
                  {t(`hosts.components.installed.cap.${key}`)}
                </span>
                <Switch
                  checked={Boolean(effective?.[key])}
                  label={t(`hosts.components.installed.cap.${key}`)}
                  onChange={(checked) =>
                    setDraft({
                      ...(effective ?? capabilitiesFromProfile("")),
                      [key]: checked,
                    })
                  }
                />
              </div>
            ))}
          </div>
          {dirty && diffLines.length > 0 && (
            <>
              <DiffViewer lines={diffLines} />
              <div className="argus-form-actions">
                <Button
                  loading={previewing}
                  onClick={() => void previewConfig()}
                  variant="primary"
                >
                  {t("hosts.components.installed.previewChanges")}
                </Button>
                <Button onClick={() => setDraft(null)} variant="ghost">
                  {t("hosts.reset")}
                </Button>
              </div>
            </>
          )}
          {configAction && (
            <PendingActionConfirm
              action={configAction}
              onCancel={() => setConfigAction(null)}
              onDone={() => {
                setConfigAction(null);
                setDraft(null);
                onChanged();
              }}
            />
          )}
        </section>

        <section className="argus-detail-section">
          <h3 className="argus-detail-section__title">
            {t("hosts.components.installed.route")}
          </h3>
          <p>
            {telemetryRouteOf(host) &&
            telemetryRouteOf(host) !== "direct_argus" ? (
              <Badge tone="accent">
                {t("hosts.components.installed.routeViaGateway", {
                  route: telemetryRouteOf(host),
                })}
              </Badge>
            ) : (
              <Badge tone="info">
                {t("hosts.components.installed.routeDirect")}
              </Badge>
            )}
          </p>
          {!routeEditing && !routeAction && (
            <div className="argus-form-actions">
              <Button
                onClick={() => {
                  setRouteEditing(true);
                  setRouteTested(false);
                  setRouteChoice(currentRoute?.gateway_collector_id ?? "direct_argus");
                }}
                variant="secondary"
              >
                {t("hosts.components.installed.changeRoute")}
              </Button>
              <Button loading={routeTesting} onClick={() => void testCurrentRoute()} variant="secondary">
                {t("hosts.components.installed.routeTest")}
              </Button>
              {routeTested && <StatusBadge tone="success">{t("hosts.components.installed.routeOk")}</StatusBadge>}
            </div>
          )}
          {routeEditing && !routeAction && (
            <>
              <Field requirement="optional" label={t("hosts.components.installed.route")}>
                <Select
                  onValueChange={(value) => {
                    setRouteChoice(value);
                    setRouteTested(false);
                  }}
                  options={[
                    {
                      value: "direct_argus",
                      label: t("hosts.components.installed.routeDirect"),
                    },
                    ...gatewayScopes.flatMap((gatewayScope) => {
                      const gateway = collectorsQuery.data?.find((entry) => entry.resource_id === gatewayScope.connector_host_id && entry.role === "edge_gateway");
                      return gateway ? [{ value: gateway.id, label: t("hosts.components.installed.routeViaGateway", { route: gatewayScope.name }) }] : [];
                    }),
                  ]}
                  value={routeChoice}
                />
              </Field>
              <div className="argus-form-actions">
                <Button
                  loading={previewing}
                  onClick={() => void previewRoute()}
                  variant="primary"
                >
                  {t("hosts.components.installed.previewChanges")}
                </Button>
                <Button onClick={() => setRouteEditing(false)} variant="ghost">
                  {t("hosts.cancel")}
                </Button>
              </div>
            </>
          )}
          {routeAction && (
            <PendingActionConfirm
              action={routeAction}
              onCancel={() => setRouteAction(null)}
              onDone={() => {
                setRouteAction(null);
                setRouteEditing(false);
                onChanged();
              }}
            />
          )}
        </section>
        <section className="argus-detail-section">
          <h3 className="argus-detail-section__title">{t("hosts.components.installed.lifecycle")}</h3>
          {lifecycleAction ? (
            <PendingActionConfirm action={lifecycleAction} onCancel={() => setLifecycleAction(null)} onDone={() => { setLifecycleAction(null); onChanged(); }} />
          ) : (
            <div className="argus-form-actions">
              <Button loading={previewing} onClick={() => void previewLifecycle("upgrade")} variant="secondary">{t("hosts.components.installed.upgrade")}</Button>
              <Button loading={previewing} onClick={() => void previewLifecycle("repair")} variant="secondary">{t("hosts.components.installed.repair")}</Button>
              <Button loading={previewing} onClick={() => void previewLifecycle("uninstall")} variant="danger">{t("hosts.components.installed.uninstall")}</Button>
            </div>
          )}
        </section>
      </CardContent>
    </Card>
  );
}

/** 详情页「已安装组件」Tab。 */
export function ComponentsTab({
  host,
  scopes,
  onChanged,
}: {
  host: Host;
  scopes: BastionScope[];
  onChanged: () => void;
}) {
  return (
    <div className="argus-hosts-stack">
      {host.connection_mode === "connector_local" && (
        <ConnectorCard host={host} onChanged={onChanged} />
      )}
      <CollectorCard host={host} onChanged={onChanged} scopes={scopes} />
    </div>
  );
}
