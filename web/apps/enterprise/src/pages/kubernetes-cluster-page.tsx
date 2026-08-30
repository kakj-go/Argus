import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import {
  Alert,
  Badge,
  EmptyState,
  PageShell,
  Spinner,
  StatusBadge,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@argus/ui";
import { CollectorStatusPanel } from "../components/kubernetes/collector-status";
import { CollectorWizard } from "../components/kubernetes/collector-wizard";
import { connectionStatusTone } from "../components/kubernetes/status";
import { WorkloadExplorer } from "../components/kubernetes/workload-explorer";
import { ResourceTelemetry } from "../components/telemetry/resource-telemetry";
import "../styles/kubernetes.css";

/** Kubernetes 集群详情：资源查询 + Collector 安装/管理。 */
export function KubernetesClusterPage() {
  const { t } = useTranslation();
  const api = useApi();
  const { clusterId } = useParams({ strict: false });
  const id = clusterId ?? "";

  const clusterQuery = useQuery({
    queryKey: ["kubernetes", "clusters", id],
    queryFn: () => api.kubernetes.getCluster(id),
    enabled: id.length > 0,
  });
  const collectorQuery = useQuery({
    queryKey: ["kubernetes", "collector", id],
    queryFn: () => api.kubernetes.getCollector(id),
    enabled: id.length > 0,
    refetchInterval: (query) =>
      query.state.data &&
      ["pending_install", "installing", "uninstalling"].includes(
        query.state.data.status,
      )
        ? 2000
        : false,
  });

  const cluster = clusterQuery.data;
  const initialTab =
    typeof window !== "undefined" && window.location.hash === "#otlp-collector"
      ? "collector"
      : "resources";

  if (clusterQuery.isLoading) {
    return (
      <PageShell title={t("shell.nav.clusterDetail")}>
        <Spinner label={t("common.loading")} />
      </PageShell>
    );
  }

  if (!cluster) {
    return (
      <PageShell
        breadcrumbs={[
          { label: t("shell.nav.kubernetes"), href: "/kubernetes" },
          { label: id },
        ]}
        title={t("shell.nav.clusterDetail")}
      >
        <EmptyState
          description={t("kubernetes.loadFailed")}
          kind="error"
          title={id}
        />
      </PageShell>
    );
  }

  return (
    <PageShell
      breadcrumbs={[
        { label: t("shell.nav.kubernetes"), href: "/kubernetes" },
        { label: cluster.name },
      ]}
      description={cluster.api_server}
      title={
        <span className="argus-k8s-cluster-card__head">
          {cluster.name}
          <Badge tone="accent">
            {t(`kubernetes.environment.${cluster.environment}`)}
          </Badge>
          <StatusBadge tone={connectionStatusTone(cluster.connection_status)}>
            {t(`kubernetes.status.${cluster.connection_status}`)}
          </StatusBadge>
          <Badge tone="neutral">{cluster.kubernetes_version}</Badge>
        </span>
      }
    >
      <Tabs defaultValue={initialTab}>
        <TabsList>
          <TabsTrigger value="resources">
            {t("kubernetes.detail.resourcesTab")}
          </TabsTrigger>
          <TabsTrigger value="collector">
            {t("kubernetes.detail.collectorTab")}
          </TabsTrigger>
          <TabsTrigger value="metrics">{t("telemetry.metrics")}</TabsTrigger>
          <TabsTrigger value="logs">{t("telemetry.logs")}</TabsTrigger>
          <TabsTrigger value="traces">{t("telemetry.traces")}</TabsTrigger>
        </TabsList>
        <TabsContent value="resources">
          <WorkloadExplorer cluster={cluster} />
        </TabsContent>
        <TabsContent value="collector">
          <div id="otlp-collector">
            {collectorQuery.isLoading ? (
              <Spinner label={t("common.loading")} />
            ) : collectorQuery.data ? (
              <CollectorStatusPanel
                cluster={cluster}
                collector={collectorQuery.data}
              />
            ) : (
              <div className="argus-k8s-stack">
                <Alert
                  description={t("kubernetes.collector.notInstalledHint")}
                  title={t("kubernetes.collector.notInstalled")}
                  tone="info"
                />
                <CollectorWizard cluster={cluster} />
              </div>
            )}
          </div>
        </TabsContent>
        <TabsContent value="metrics">
          <ResourceTelemetry resourceId={cluster.id} signal="metrics" />
        </TabsContent>
        <TabsContent value="logs">
          <ResourceTelemetry resourceId={cluster.id} signal="logs" />
        </TabsContent>
        <TabsContent value="traces">
          <ResourceTelemetry resourceId={cluster.id} signal="traces" />
        </TabsContent>
      </Tabs>
    </PageShell>
  );
}
