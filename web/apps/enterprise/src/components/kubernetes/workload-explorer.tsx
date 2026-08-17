import { useQuery } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import {
  useApi,
  type KubernetesCluster,
  type KubernetesResource,
} from "@argus/api-client";
import {
  Button,
  DataTable,
  Dialog,
  EmptyState,
  FormDrawer,
  KeyValueGrid,
  LogViewer,
  Spinner,
  StatusBadge,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  type Column,
  type LogLine,
} from "@argus/ui";

type KindKey = KubernetesResource["resource_type"];

const KINDS: KindKey[] = [
  "namespace",
  "node",
  "pod",
  "deployment",
  "statefulset",
  "daemonset",
  "service",
];

function displayValue(value: unknown): string {
  if (value === null || value === undefined || value === "") return "—";
  if (typeof value === "string" || typeof value === "number") {
    return String(value);
  }
  if (typeof value === "boolean") return value ? "true" : "false";
  return JSON.stringify(value);
}

function summaryValue(resource: KubernetesResource, key: string): string {
  return displayValue(resource.summary[key]);
}

function statusTone(
  value: string,
): "neutral" | "success" | "warning" | "danger" {
  const normalized = value.toLowerCase();
  if (
    ["healthy", "ready", "running", "verified", "active"].includes(normalized)
  ) {
    return "success";
  }
  if (["degraded", "pending", "proposed", "notready"].includes(normalized)) {
    return "warning";
  }
  if (["failed", "error", "rejected", "offline"].includes(normalized)) {
    return "danger";
  }
  return "neutral";
}

function ResourceDetailDrawer({
  resource,
  onClose,
}: {
  resource: KubernetesResource | null;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  if (!resource) return null;

  const metadata = [
    { label: t("kubernetes.table.name"), value: resource.name },
    {
      label: t("kubernetes.table.kind"),
      value: t(`kubernetes.kinds.${resource.resource_type}`),
    },
    ...(resource.namespace
      ? [
          {
            label: t("kubernetes.table.namespace"),
            value: resource.namespace,
          },
        ]
      : []),
    ...Object.entries(resource.labels).map(([key, value]) => ({
      label: key,
      value,
    })),
  ];
  const summary = Object.entries(resource.summary).map(([key, value]) => ({
    label: key,
    value: displayValue(value),
  }));

  return (
    <FormDrawer
      footer={
        <Button onClick={onClose} variant="secondary">
          {t("kubernetes.drawer.close")}
        </Button>
      }
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      open
      title={`${t("kubernetes.drawer.title")} · ${resource.name}`}
      width={520}
    >
      <div className="argus-k8s-drawer-section">
        <h3 className="argus-k8s-section-title">
          {t("kubernetes.drawer.metadata")}
        </h3>
        <KeyValueGrid columns={2} items={metadata} />
      </div>
      {summary.length > 0 && (
        <div className="argus-k8s-drawer-section">
          <h3 className="argus-k8s-section-title">
            {t("kubernetes.drawer.status")}
          </h3>
          <KeyValueGrid columns={2} items={summary} />
        </div>
      )}
    </FormDrawer>
  );
}

function PodLogsDialog({
  clusterId,
  pod,
  onClose,
}: {
  clusterId: string;
  pod: KubernetesResource | null;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const logsQuery = useQuery({
    queryKey: ["kubernetes", "pod-logs", clusterId, pod?.namespace, pod?.name],
    queryFn: () =>
      api.kubernetes.getPodLogs(clusterId, {
        namespace: pod?.namespace ?? "",
        pod: pod?.name ?? "",
        tail_lines: 500,
      }),
    enabled: Boolean(pod?.namespace && pod?.name),
  });
  if (!pod) return null;

  const lines: LogLine[] = (logsQuery.data?.content ?? "")
    .split("\n")
    .filter(Boolean)
    .map((content) => ({ content }));
  const detail = logsQuery.data
    ? `${logsQuery.data.bytes} bytes${
        logsQuery.data.truncated ? " · truncated" : ""
      }`
    : `${pod.namespace ?? ""} · ${t("common.loading")}`;

  return (
    <Dialog
      description={detail}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      open
      title={`${t("kubernetes.logs.title")} · ${pod.name}`}
    >
      {logsQuery.isLoading ? (
        <Spinner label={t("common.loading")} />
      ) : logsQuery.isError ? (
        <EmptyState
          description={t("kubernetes.emptyResources.description")}
          kind="error"
          title={t("kubernetes.loadFailed")}
        />
      ) : (
        <LogViewer fileName={`${pod.name}.log`} height={360} lines={lines} />
      )}
    </Dialog>
  );
}

/** Bounded M3 resource query and Pod logs, shared by mock and real adapters. */
export function WorkloadExplorer({ cluster }: { cluster: KubernetesCluster }) {
  const { t } = useTranslation();
  const api = useApi();
  const [kind, setKind] = useState<KindKey>("deployment");
  const [detail, setDetail] = useState<KubernetesResource | null>(null);
  const [logPod, setLogPod] = useState<KubernetesResource | null>(null);

  const resourcesQuery = useQuery({
    queryKey: ["kubernetes", "resources", cluster.id, kind],
    queryFn: () =>
      api.kubernetes.listResources(cluster.id, {
        resource_type: kind,
        limit: 100,
      }),
  });

  const columns: Column<KubernetesResource>[] = [
    {
      key: "name",
      header: t("kubernetes.table.name"),
      render: (row) => (
        <button
          className="argus-k8s-link-button"
          onClick={() => setDetail(row)}
          type="button"
        >
          {row.name}
        </button>
      ),
    },
    {
      key: "namespace",
      header: t("kubernetes.table.namespace"),
      render: (row) => row.namespace ?? "—",
    },
    {
      key: "ready",
      header: t("kubernetes.table.ready"),
      render: (row) => summaryValue(row, "ready"),
    },
    {
      key: "status",
      header: t("kubernetes.table.status"),
      render: (row) => {
        const status = summaryValue(row, "status");
        return status === "—" ? (
          status
        ) : (
          <StatusBadge tone={statusTone(status)}>{status}</StatusBadge>
        );
      },
    },
    {
      key: "actions",
      header: t("kubernetes.table.actions"),
      render: (row) => (
        <div className="argus-inline-actions">
          <Button onClick={() => setDetail(row)} size="sm" variant="ghost">
            {t("kubernetes.table.detail")}
          </Button>
          {row.resource_type === "pod" && row.namespace && (
            <Button onClick={() => setLogPod(row)} size="sm" variant="ghost">
              {t("kubernetes.table.logs")}
            </Button>
          )}
        </div>
      ),
    },
  ];

  let content: ReactNode;
  if (resourcesQuery.isLoading) {
    content = <Spinner label={t("common.loading")} />;
  } else if (resourcesQuery.isError) {
    content = (
      <EmptyState
        description={t("kubernetes.emptyResources.description")}
        kind="error"
        title={t("kubernetes.loadFailed")}
      />
    );
  } else if ((resourcesQuery.data?.items.length ?? 0) === 0) {
    content = (
      <EmptyState
        description={t("kubernetes.emptyResources.description")}
        title={t("kubernetes.emptyResources.title")}
      />
    );
  } else {
    content = (
      <DataTable
        columns={columns}
        data={resourcesQuery.data?.items ?? []}
        getRowKey={(row) =>
          `${row.resource_type}/${row.namespace ?? ""}/${row.name}`
        }
      />
    );
  }

  return (
    <>
      <Tabs onValueChange={(value) => setKind(value as KindKey)} value={kind}>
        <TabsList>
          {KINDS.map((item) => (
            <TabsTrigger key={item} value={item}>
              {t(`kubernetes.kinds.${item}`)}
            </TabsTrigger>
          ))}
        </TabsList>
        {KINDS.map((item) => (
          <TabsContent key={item} value={item}>
            {item === kind ? content : null}
          </TabsContent>
        ))}
      </Tabs>
      <ResourceDetailDrawer onClose={() => setDetail(null)} resource={detail} />
      <PodLogsDialog
        clusterId={cluster.id}
        onClose={() => setLogPod(null)}
        pod={logPod}
      />
    </>
  );
}
