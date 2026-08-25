import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { Cpu, HardDrive, MemoryStick, Timer } from "lucide-react";
import { useApi, type Host } from "@argus/api-client";
import {
  Badge,
  Card,
  CardContent,
  CardHeader,
  EmptyState,
  KeyValueGrid,
  MetricChart,
  PageShell,
  Spinner,
  StatCard,
  StatusBadge,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@argus/ui";
import "../styles/hosts.css";
import { ComponentsTab } from "../components/hosts/components-tab";
import { TasksTab } from "../components/hosts/tasks-tab";
import { RealTerminalTab } from "../components/hosts/real-terminal-tab";
import { ResourceTelemetry } from "../components/telemetry/resource-telemetry";
import {
  collectorTone,
  collectorStatusOf,
  connectionPathKey,
  environmentTone,
  formatDateTime,
  formatUptime,
  hostStatusTone,
  scopeOf,
  seededNumber,
  seededSeries,
  telemetryRouteOf,
} from "../components/hosts/host-utils";

const realMode = import.meta.env.VITE_API_MODE === "real";

/** 概览 Tab：StatCard 行 + KeyValueGrid + 24h 指标面积图（演示数据按主机 ID 确定性生成）。 */
function OverviewTab({ host }: { host: Host }) {
  const { t } = useTranslation();
  const api = useApi();

  const scopesQuery = useQuery({
    queryKey: ["bastion-scopes"],
    queryFn: () => api.connectors.listBastionScopes(),
  });
  const scopes = scopesQuery.data?.items ?? [];
  const scope = scopeOf(host, scopes);
  const accountsQuery = useQuery({
    queryKey: ["managed-accounts"],
    queryFn: () => api.secrets.listManagedAccounts(),
  });
  const managedAccounts = (accountsQuery.data ?? []).filter(
    (account) => account.host_id === host.id && account.status === "active",
  );

  const metrics = useMemo(() => {
    const labels: string[] = [];
    const now = new Date();
    for (let index = 23; index >= 0; index -= 1) {
      const point = new Date(now.getTime() - index * 3_600_000);
      labels.push(`${String(point.getHours()).padStart(2, "0")}:00`);
    }
    return {
      labels,
      cpu: seededSeries(`${host.id}:cpu`, 24, 34, 18),
      memory: seededSeries(`${host.id}:mem`, 24, 52, 10),
    };
  }, [host.id]);

  const path = t(`hosts.path.${connectionPathKey(host)}`, {
    scope: scope?.name ?? host.bastion_scope_id ?? "",
    address: `${host.address}:${host.port}`,
  });

  return (
    <div className="argus-hosts-stack">
      {!realMode && <div className="argus-stat-row">
        <StatCard
          detail={t("hosts.overview.metrics24h")}
          icon={<Cpu aria-hidden size={16} />}
          label={t("hosts.overview.cpu")}
          tone="accent"
          value={`${Math.round(seededNumber(`${host.id}:cpu:now`, 18, 82))}%`}
        />
        <StatCard
          icon={<MemoryStick aria-hidden size={16} />}
          label={t("hosts.overview.memory")}
          tone="info"
          value={`${Math.round(seededNumber(`${host.id}:mem:now`, 30, 88))}%`}
        />
        <StatCard
          icon={<HardDrive aria-hidden size={16} />}
          label={t("hosts.overview.disk")}
          tone="warning"
          value={`${Math.round(seededNumber(`${host.id}:disk:now`, 35, 92))}%`}
        />
        <StatCard
          icon={<Timer aria-hidden size={16} />}
          label={t("hosts.overview.uptime")}
          value={formatUptime(host.created_at)}
        />
      </div>}

      {!realMode && <Card>
        <CardHeader title={t("hosts.overview.basicInfo")} />
        <CardContent>
          <KeyValueGrid
            columns={3}
            items={[
              {
                label: t("hosts.overview.kv.hostname"),
                value: <span className="argus-mono">{host.hostname}</span>,
              },
              {
                label: t("hosts.overview.kv.address"),
                value: (
                  <span className="argus-mono">
                    {host.address}:{host.port}
                  </span>
                ),
              },
              {
                label: t("hosts.overview.kv.platform"),
                value: host.platform === "linux" ? "Linux" : "Windows",
              },
              {
                label: t("hosts.overview.kv.environment"),
                value: (
                  <Badge tone={environmentTone(host.environment)}>
                    {t(`hosts.env.${host.environment}`)}
                  </Badge>
                ),
              },
              {
                label: t("hosts.overview.kv.connectionMode"),
                value: t(`hosts.connectionMode.${host.connection_mode}`),
              },
              { label: t("hosts.overview.kv.connectionPath"), value: path },
              {
                label: t("hosts.overview.kv.credential"),
                value: managedAccounts.length > 0 ? (
                  <span className="argus-mono">
                    {managedAccounts.map((account) => account.username).join(", ")}
                  </span>
                ) : (
                  "—"
                ),
              },
              {
                label: t("hosts.overview.kv.telemetryRoute"),
                value: telemetryRouteOf(host) ? (
                  <span className="argus-mono">{telemetryRouteOf(host)}</span>
                ) : (
                  "—"
                ),
              },
              {
                label: t("hosts.overview.kv.lastSeen"),
                value: formatDateTime(host.last_seen_at ?? host.updated_at),
              },
              {
                label: t("hosts.overview.kv.createdAt"),
                value: formatDateTime(host.created_at),
              },
              {
                label: t("hosts.overview.kv.labels"),
                value:
                  Object.keys(host.labels).length > 0 ? (
                    <span className="argus-host-tile__tags">
                      {Object.entries(host.labels).map(([key, value]) => (
                        <Badge key={key} tone="neutral">
                          {key}={value}
                        </Badge>
                      ))}
                    </span>
                  ) : (
                    "—"
                  ),
              },
            ]}
          />
        </CardContent>
      </Card>}

      {!realMode && <Card>
        <CardHeader title={t("hosts.overview.metrics24h")} />
        <CardContent>
          <MetricChart
            formatValue={(value) => `${Math.round(value)}%`}
            height={240}
            labels={metrics.labels}
            series={[
              { name: "CPU", points: metrics.cpu },
              { name: t("hosts.overview.memory"), points: metrics.memory },
            ]}
            showLegend
            type="area"
          />
        </CardContent>
      </Card>}
    </div>
  );
}
/** 主机详情页：概览 | 终端与会话 | 已安装组件 | 任务与审计。 */
export function HostDetailPage() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const { hostId } = useParams({ strict: false });

  const hostQuery = useQuery({
    queryKey: ["hosts", "detail", hostId],
    queryFn: () => api.hosts.get(hostId ?? ""),
    enabled: Boolean(hostId),
    retry: false,
  });
  const scopesQuery = useQuery({
    queryKey: ["bastion-scopes"],
    queryFn: () => api.connectors.listBastionScopes(),
  });

  const host = hostQuery.data;
  const scopes = scopesQuery.data?.items ?? [];
  const initialTab =
    typeof window !== "undefined" && window.location.hash === "#otlp-collector"
      ? "components"
      : "overview";

  const invalidateAll = () => {
    void queryClient.invalidateQueries({ queryKey: ["hosts"] });
    void queryClient.invalidateQueries({ queryKey: ["bastion-scopes"] });
    void queryClient.invalidateQueries({ queryKey: ["connectors"] });
    void queryClient.invalidateQueries({ queryKey: ["host-collector"] });
    void queryClient.invalidateQueries({ queryKey: ["remote-sessions"] });
    void queryClient.invalidateQueries({ queryKey: ["tasks"] });
    void queryClient.invalidateQueries({ queryKey: ["audit"] });
  };

  return (
    <PageShell
      className="argus-host-detail-page"
      breadcrumbs={[
        { label: t("hosts.detail.backToList"), href: "/hosts" },
        { label: host?.name ?? hostId ?? "" },
      ]}
      title={
        host ? (
          <>
            {host.name}{" "}
            <StatusBadge
              pulse={host.connection_status === "online"}
              tone={hostStatusTone(host.connection_status)}
            >
              {t(`hosts.status.${host.connection_status}`)}
            </StatusBadge>{" "}
            {!realMode && <StatusBadge tone={collectorTone(collectorStatusOf(host))}>
              {t(`hosts.collectorStatus.${collectorStatusOf(host)}`)}
            </StatusBadge>}
          </>
        ) : (
          (hostId ?? "")
        )
      }
    >
      {hostQuery.isLoading && <Spinner />}
      {hostQuery.isError && (
        <EmptyState
          description=""
          kind="error"
          title={t("hosts.detail.notFound")}
        />
      )}
      {host && (
        <Tabs defaultValue={initialTab}>
          <TabsList>
            <TabsTrigger value="overview">
              {t("hosts.detail.tabOverview")}
            </TabsTrigger>
            <TabsTrigger value="terminal">
              {t("hosts.detail.tabTerminal")}
            </TabsTrigger>
            <TabsTrigger value="components">
              {t("hosts.detail.tabComponents")}
            </TabsTrigger>
            <TabsTrigger value="metrics">{t("telemetry.metrics")}</TabsTrigger>
            <TabsTrigger value="logs">{t("telemetry.logs")}</TabsTrigger>
            <TabsTrigger value="traces">{t("telemetry.traces")}</TabsTrigger>
            {!realMode && <TabsTrigger value="tasks">
              {t("hosts.detail.tabTasks")}
            </TabsTrigger>}
          </TabsList>
          <TabsContent value="overview">
            <OverviewTab host={host} />
          </TabsContent>
          <TabsContent value="terminal">
            {realMode ? <RealTerminalTab host={host} /> : <EmptyState description={t("hosts.terminal.realOnlyDesc")} title={t("hosts.terminal.realOnly")} />}
          </TabsContent>
          <TabsContent value="components">
            <div id="otlp-collector">
              <ComponentsTab
                host={host}
                onChanged={invalidateAll}
                scopes={scopes}
              />
            </div>
          </TabsContent>
          <TabsContent value="metrics">
            <ResourceTelemetry resourceId={host.id} signal="metrics" />
          </TabsContent>
          <TabsContent value="logs">
            <ResourceTelemetry resourceId={host.id} signal="logs" />
          </TabsContent>
          <TabsContent value="traces">
            <ResourceTelemetry resourceId={host.id} signal="traces" />
          </TabsContent>
          {!realMode && <TabsContent value="tasks">
            <TasksTab host={host} />
          </TabsContent>}
        </Tabs>
      )}
    </PageShell>
  );
}
