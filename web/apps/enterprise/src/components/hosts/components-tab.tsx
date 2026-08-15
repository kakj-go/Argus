import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import {
  useApi,
  type BastionScope,
  type CollectorInstallState,
  type Host,
  type PendingAction,
} from "@argus/api-client";
import {
  Badge,
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
import { collectorTone, formatDateTime, scopeOf } from "./host-utils";

/** 采集能力开关键（docs/09 §5.1 的 Collection Profile 草稿开关）。 */
const CAPABILITY_KEYS = [
  "hostBasic",
  "systemLogs",
  "fileLogs",
  "docker",
  "prometheus",
  "otlp",
] as const;
type CapabilityKey = (typeof CAPABILITY_KEYS)[number];

function capabilitiesFromProfile(
  profile: string,
): Record<CapabilityKey, boolean> {
  const parts = profile.split(",").map((part) => part.trim());
  return {
    hostBasic: parts.includes("host-basic") || parts.includes("host-full"),
    systemLogs: parts.includes("system-logs") || parts.includes("host-full"),
    fileLogs: parts.includes("file-logs"),
    docker: parts.includes("docker"),
    prometheus: parts.includes("prometheus"),
    otlp: parts.includes("otlp"),
  };
}

function profileFromCapabilities(caps: Record<CapabilityKey, boolean>): string {
  const parts: string[] = [];
  if (caps.hostBasic) parts.push("host-basic");
  if (caps.systemLogs) parts.push("system-logs");
  if (caps.fileLogs) parts.push("file-logs");
  if (caps.docker) parts.push("docker");
  if (caps.prometheus) parts.push("prometheus");
  if (caps.otlp) parts.push("otlp");
  return parts.join(",") || "host-basic";
}

/** 前端模拟的路由测试（真实实现由源 Collector 所在主机执行）。 */
function simulateRouteTest(): Promise<boolean> {
  return new Promise((resolve) => {
    window.setTimeout(() => resolve(true), 800);
  });
}

const COLLECTOR_PROFILES = [
  "host-basic",
  "host-basic,system-logs",
  "host-full",
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
  const [confirming, setConfirming] = useState(false);
  const [rotateStatus, setRotateStatus] =
    useState<PreviewCommitStatus>("pending");

  const connectorsQuery = useQuery({
    queryKey: ["connectors"],
    queryFn: () => api.connectors.list(),
  });
  const connector = (connectorsQuery.data?.items ?? []).find(
    (entry) => entry.id === host.connectorId || entry.hostId === host.id,
  );
  if (!connector) return null;

  const rotate = async () => {
    if (confirming) return;
    setConfirming(true);
    try {
      // rotateCertificate 为单步写操作（无独立 PendingAction），
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

  return (
    <Card>
      <CardHeader
        action={
          !rotating && (
            <Button
              onClick={() => {
                setRotating(true);
                setRotateStatus("pending");
              }}
              variant="secondary"
            >
              {t("hosts.components.rotateCert")}
            </Button>
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
                  epoch {connector.connectionEpoch}
                </span>
              ),
            },
            {
              label: t("hosts.components.certExpires", {
                time: formatDateTime(connector.certificateExpiresAt),
              }),
              value: formatDateTime(connector.lastHeartbeatAt),
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
                ? `${t("hosts.components.rotated")} · ${t("hosts.components.certExpires", { time: formatDateTime(connector.certificateExpiresAt) })}`
                : undefined
            }
            risk="write"
            status={rotateStatus}
            title={t("hosts.components.rotateTitle")}
          >
            <p className="argus-muted">{t("hosts.components.rotateDesc")}</p>
          </PreviewCommitCard>
        )}
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
  const [routeTested, setRouteTested] = useState(false);
  const [routeTesting, setRouteTesting] = useState(false);
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(
    null,
  );
  const [submitting, setSubmitting] = useState(false);

  const scope = scopeOf(host, scopes);
  const gatewayScopes =
    host.connectionMode === "connector_local"
      ? scopes.filter(
          (entry) =>
            entry.status === "active" &&
            entry.id !== host.bastionScopeId &&
            Boolean(entry.connectorHostId),
        )
      : scope
        ? [scope]
        : [];

  const close = (next: boolean) => {
    if (!next) {
      setStep(0);
      setProfile(COLLECTOR_PROFILES[0]!);
      setRoute("direct_argus");
      setRouteTested(false);
      setPendingAction(null);
    }
    onOpenChange(next);
  };

  const runRouteTest = async () => {
    setRouteTesting(true);
    await simulateRouteTest();
    setRouteTested(true);
    setRouteTesting(false);
  };

  const submit = async () => {
    if (submitting) return;
    setSubmitting(true);
    try {
      setPendingAction(
        await api.hosts.previewCollectorInstall(host.id, {
          profile,
          telemetryRoute: route,
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
          canNext={step === 0 ? Boolean(profile) : routeTested}
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
                  setRouteTested(false);
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
                const value =
                  gatewayScope.defaultTelemetryRoute ?? gatewayScope.name;
                return (
                  <button
                    className={`argus-choice ${route === value ? "is-selected" : ""}`}
                    key={gatewayScope.id}
                    onClick={() => {
                      setRoute(value);
                      setRouteTested(false);
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
              <div className="argus-form-actions">
                <Button
                  loading={routeTesting}
                  onClick={() => void runRouteTest()}
                  variant="secondary"
                >
                  {routeTesting
                    ? t("hosts.components.installWizard.routeTesting")
                    : t("hosts.components.installWizard.routeTest")}
                </Button>
                {routeTested ? (
                  <StatusBadge tone="success">
                    {t("hosts.components.installWizard.routeOk")}
                  </StatusBadge>
                ) : (
                  <span className="argus-muted">
                    {t("hosts.components.installWizard.needRouteTest")}
                  </span>
                )}
              </div>
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
  const [configAction, setConfigAction] = useState<PendingAction | null>(null);
  const [routeAction, setRouteAction] = useState<PendingAction | null>(null);
  const [routeChoice, setRouteChoice] = useState("direct_argus");
  const [routeTested, setRouteTested] = useState(false);
  const [routeTesting, setRouteTesting] = useState(false);
  const [routeEditing, setRouteEditing] = useState(false);
  const [previewing, setPreviewing] = useState(false);

  const collectorQuery = useQuery({
    queryKey: ["host-collector", host.id],
    queryFn: () => api.hosts.getCollector(host.id),
  });
  const collector: CollectorInstallState | null = collectorQuery.data ?? null;

  const scope = scopeOf(host, scopes);
  const gatewayScopes =
    host.connectionMode === "connector_local"
      ? scopes.filter(
          (entry) =>
            entry.status === "active" &&
            entry.id !== host.bastionScopeId &&
            Boolean(entry.connectorHostId),
        )
      : scope
        ? [scope]
        : [];

  const current = useMemo(
    () => (collector ? capabilitiesFromProfile(collector.profile) : null),
    [collector],
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
    if (!effective || previewing) return;
    setPreviewing(true);
    try {
      setConfigAction(
        await api.approvals.preview({
          tool: "telemetry.collector.configure",
          title: t("hosts.components.installed.configureTitle", {
            name: host.name,
          }),
          params: {
            hostId: host.id,
            profile: profileFromCapabilities(effective),
          },
        }),
      );
    } finally {
      setPreviewing(false);
    }
  };

  const previewRoute = async () => {
    if (previewing) return;
    setPreviewing(true);
    try {
      setRouteAction(
        await api.approvals.preview({
          tool: "telemetry.collector.route",
          title: t("hosts.components.installed.routeTitle", {
            name: host.name,
          }),
          params: {
            hostId: host.id,
            route: routeChoice,
          },
        }),
      );
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
              tone={collectorTone(
                collector.status === "converged"
                  ? "converged"
                  : host.collectorStatus,
              )}
            >
              {t(`hosts.collectorStatus.${host.collectorStatus}`)}
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
              value: <span className="argus-mono">{collector.profile}</span>,
            },
            {
              label: t("hosts.components.version", {
                version: collector.version,
              }),
              value: collector.role,
            },
            {
              label: t("hosts.components.installed.revision"),
              value: t("hosts.components.installed.revisionValue", {
                effective: collector.effectiveRevision,
                desired: collector.desiredRevision,
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
            {host.telemetryRoute && host.telemetryRoute !== "direct_argus" ? (
              <Badge tone="accent">
                {t("hosts.components.installed.routeViaGateway", {
                  route: host.telemetryRoute,
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
                }}
                variant="secondary"
              >
                {t("hosts.components.installed.changeRoute")}
              </Button>
            </div>
          )}
          {routeEditing && !routeAction && (
            <>
              <Field label={t("hosts.components.installed.route")}>
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
                    ...gatewayScopes.map((gatewayScope) => ({
                      value:
                        gatewayScope.defaultTelemetryRoute ?? gatewayScope.name,
                      label: t("hosts.components.installed.routeViaGateway", {
                        route: gatewayScope.name,
                      }),
                    })),
                  ]}
                  value={routeChoice}
                />
              </Field>
              <div className="argus-form-actions">
                <Button
                  loading={routeTesting}
                  onClick={() => {
                    setRouteTesting(true);
                    void simulateRouteTest().then(() => {
                      setRouteTested(true);
                      setRouteTesting(false);
                    });
                  }}
                  variant="secondary"
                >
                  {t("hosts.components.installed.routeTest")}
                </Button>
                {routeTested && (
                  <StatusBadge tone="success">
                    {t("hosts.components.installed.routeOk")}
                  </StatusBadge>
                )}
                <Button
                  disabled={!routeTested}
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
      {host.connectionMode === "connector_local" && (
        <ConnectorCard host={host} onChanged={onChanged} />
      )}
      <CollectorCard host={host} onChanged={onChanged} scopes={scopes} />
    </div>
  );
}
