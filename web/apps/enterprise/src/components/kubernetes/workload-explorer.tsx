import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import {
  useApi,
  type K8sCluster,
  type K8sWorkload,
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
import { bindingStatusTone, workloadStatusTone } from "./status";

type KindKey =
  | "namespace"
  | "node"
  | "pod"
  | "deployment"
  | "statefulset"
  | "daemonset"
  | "service";

const KINDS: KindKey[] = [
  "namespace",
  "node",
  "pod",
  "deployment",
  "statefulset",
  "daemonset",
  "service",
];

const WORKLOAD_KIND_MAP: Partial<Record<KindKey, K8sWorkload["kind"]>> = {
  deployment: "Deployment",
  statefulset: "StatefulSet",
  daemonset: "DaemonSet",
};

type WorkloadRow = {
  id: string;
  name: string;
  namespace: string;
  kind: string;
  ready: string;
  restarts: number;
  status: K8sWorkload["status"];
};

type PodRow = {
  id: string;
  name: string;
  namespace: string;
  workload: string;
  ready: string;
  restarts: number;
  status: K8sWorkload["status"];
};

type NamespaceRow = {
  id: string;
  name: string;
  workloads: number;
  restarts: number;
};

type NodeRow = {
  id: string;
  name: string;
  host: string;
  status: string;
};

type DetailSelection =
  | { type: "workload"; row: WorkloadRow }
  | { type: "pod"; row: PodRow }
  | { type: "namespace"; row: NamespaceRow }
  | { type: "node"; row: NodeRow };

function podSuffix(seed: string): string {
  let hash = 0;
  for (let index = 0; index < seed.length; index += 1) {
    hash = (hash * 31 + seed.charCodeAt(index)) >>> 0;
  }
  return hash.toString(36).slice(0, 5).padStart(5, "0");
}

/** 由工作负载确定性推导出 Pod 行（mock 无 Pod 数据源，仅用于演示）。 */
function derivePods(workloads: K8sWorkload[]): PodRow[] {
  const pods: PodRow[] = [];
  for (const workload of workloads) {
    const [readyText, totalText] = workload.ready.split("/");
    const readyCount = Number.parseInt(readyText ?? "0", 10) || 0;
    const total = Math.min(Number.parseInt(totalText ?? "0", 10) || 0, 12);
    for (let index = 0; index < total; index += 1) {
      const name =
        workload.kind === "StatefulSet"
          ? `${workload.name}-${index}`
          : `${workload.name}-${podSuffix(`${workload.name}-${index}`)}`;
      pods.push({
        id: name,
        name,
        namespace: workload.namespace,
        workload: `${workload.kind}/${workload.name}`,
        ready: index < readyCount ? "Ready" : "NotReady",
        restarts: workload.restartCount,
        status: workload.status,
      });
    }
  }
  return pods;
}

const LOG_TEMPLATES: Array<Pick<LogLine, "level" | "content">> = [
  { level: "info", content: "GET /healthz 200 1ms" },
  { level: "info", content: "request handled path=/api/v1/orders duration=14ms" },
  { level: "debug", content: "cache hit ratio 0.87" },
  { level: "info", content: "otel export batch sent spans=64" },
  { level: "warn", content: "slow query detected duration=820ms" },
  { level: "info", content: "request handled path=/api/v1/cart duration=9ms" },
  { level: "error", content: "upstream timeout service=payment attempt=1" },
  { level: "info", content: "retry succeeded service=payment attempt=2" },
];

/** 模拟日志流弹层：打开后定时追加日志行。 */
function PodLogsDialog({
  pod,
  onClose,
}: {
  pod: PodRow | null;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [lines, setLines] = useState<LogLine[]>([]);

  useEffect(() => {
    if (!pod) return undefined;
    setLines([]);
    let tick = 0;
    const timer = window.setInterval(() => {
      const template = LOG_TEMPLATES[tick % LOG_TEMPLATES.length] ?? {
        level: "info" as const,
        content: "…",
      };
      tick += 1;
      setLines((current) =>
        [
          ...current,
          {
            timestamp: new Date().toISOString().slice(11, 19),
            level: template.level,
            content: template.content,
          },
        ].slice(-200),
      );
    }, 700);
    return () => window.clearInterval(timer);
  }, [pod]);

  if (!pod) return null;

  return (
    <Dialog
      description={`${pod.namespace} · ${pod.workload} · ${t("kubernetes.logs.simulated")}`}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      open
      title={`${t("kubernetes.logs.title")} · ${pod.name}`}
    >
      <LogViewer fileName={`${pod.name}.log`} height={360} lines={lines} />
    </Dialog>
  );
}

/** 只读详情抽屉：KeyValueGrid 元数据 + 状态 + 事件。 */
function ResourceDetailDrawer({
  selection,
  onClose,
}: {
  selection: DetailSelection | null;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  if (!selection) return null;
  const { row } = selection;

  const metadata = [
    { label: t("kubernetes.table.name"), value: row.name },
    ...("namespace" in row
      ? [{ label: t("kubernetes.table.namespace"), value: row.namespace }]
      : []),
    ...("kind" in row
      ? [{ label: t("kubernetes.table.kind"), value: row.kind }]
      : []),
    ...("workload" in row
      ? [{ label: t("kubernetes.table.workload"), value: row.workload }]
      : []),
    ...("host" in row
      ? [{ label: t("kubernetes.table.host"), value: row.host }]
      : []),
  ];

  let statusItems: Array<{ label: string; value: ReactNode }> = [];
  if (selection.type === "workload" || selection.type === "pod") {
    const current = selection.row;
    statusItems = [
      {
        label: t("kubernetes.table.status"),
        value: (
          <StatusBadge tone={workloadStatusTone(current.status)}>
            {t(`kubernetes.health.${current.status}`)}
          </StatusBadge>
        ),
      },
      { label: t("kubernetes.table.ready"), value: current.ready },
      {
        label: t("kubernetes.table.restarts"),
        value: String(current.restarts),
      },
    ];
  } else if (selection.type === "node") {
    const current = selection.row;
    statusItems = [
      {
        label: t("kubernetes.table.status"),
        value: (
          <StatusBadge
            tone={bindingStatusTone(
              current.status as "proposed" | "verified" | "rejected",
            )}
          >
            {t(`kubernetes.bindingStatus.${current.status}`)}
          </StatusBadge>
        ),
      },
    ];
  } else {
    const current = selection.row;
    statusItems = [
      {
        label: t("kubernetes.table.workloads"),
        value: String(current.workloads),
      },
      {
        label: t("kubernetes.table.restarts"),
        value: String(current.restarts),
      },
    ];
  }

  const events: Array<{ key: string; tone: "info" | "success" | "warning" | "danger" }> = [];
  if (selection.type === "workload" || selection.type === "pod") {
    events.push(
      { key: "scheduled", tone: "info" },
      { key: "pulled", tone: "info" },
      { key: "started", tone: "success" },
    );
    if ("restarts" in row && row.restarts > 0) {
      events.push({ key: "backoff", tone: "warning" });
    }
    if ("status" in row && row.status !== "healthy") {
      events.push({ key: "probeFailed", tone: "danger" });
    }
  }

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
      title={`${t("kubernetes.drawer.title")} · ${row.name}`}
      width={520}
    >
      <div className="argus-k8s-drawer-section">
        <h3 className="argus-k8s-section-title">
          {t("kubernetes.drawer.metadata")}
        </h3>
        <KeyValueGrid columns={2} items={metadata} />
      </div>
      {statusItems.length > 0 && (
        <div className="argus-k8s-drawer-section">
          <h3 className="argus-k8s-section-title">
            {t("kubernetes.drawer.status")}
          </h3>
          <KeyValueGrid columns={2} items={statusItems} />
        </div>
      )}
      {events.length > 0 && (
        <div className="argus-k8s-drawer-section">
          <h3 className="argus-k8s-section-title">
            {t("kubernetes.drawer.events")}
          </h3>
          <ul className="argus-k8s-events">
            {events.map((event) => (
              <li key={event.key}>
                <StatusBadge tone={event.tone}>
                  {t(`kubernetes.events.${event.key}`)}
                </StatusBadge>
              </li>
            ))}
          </ul>
        </div>
      )}
    </FormDrawer>
  );
}

/** 资源查询：按种类切换的只读列表 + 详情抽屉 + Pod 日志。 */
export function WorkloadExplorer({ cluster }: { cluster: K8sCluster }) {
  const { t } = useTranslation();
  const api = useApi();
  const [kind, setKind] = useState<KindKey>("deployment");
  const [detail, setDetail] = useState<DetailSelection | null>(null);
  const [logPod, setLogPod] = useState<PodRow | null>(null);

  const workloadsQuery = useQuery({
    queryKey: ["kubernetes", "workloads", cluster.id],
    queryFn: () => api.kubernetes.listWorkloads(cluster.id),
  });
  const bindingsQuery = useQuery({
    queryKey: ["kubernetes", "nodeBindings", cluster.id],
    queryFn: () => api.kubernetes.listNodeBindings(cluster.id),
  });

  const workloads = useMemo(
    () => workloadsQuery.data ?? [],
    [workloadsQuery.data],
  );

  const namespaceRows: NamespaceRow[] = useMemo(() => {
    const map = new Map<string, NamespaceRow>();
    for (const workload of workloads) {
      const row = map.get(workload.namespace) ?? {
        id: workload.namespace,
        name: workload.namespace,
        workloads: 0,
        restarts: 0,
      };
      row.workloads += 1;
      row.restarts += workload.restartCount;
      map.set(workload.namespace, row);
    }
    return [...map.values()];
  }, [workloads]);

  const podRows = useMemo(() => derivePods(workloads), [workloads]);

  const nodeRows: NodeRow[] = useMemo(
    () =>
      (bindingsQuery.data ?? []).map((binding) => ({
        id: binding.id,
        name: binding.nodeName,
        host: binding.hostId ?? "—",
        status: binding.status,
      })),
    [bindingsQuery.data],
  );

  const toWorkloadRow = (workload: K8sWorkload): WorkloadRow => ({
    id: `${workload.namespace}/${workload.kind}/${workload.name}`,
    name: workload.name,
    namespace: workload.namespace,
    kind: workload.kind,
    ready: workload.ready,
    restarts: workload.restartCount,
    status: workload.status,
  });

  const workloadColumns: Column<WorkloadRow>[] = [
    {
      key: "name",
      header: t("kubernetes.table.name"),
      render: (row) => (
        <button
          className="argus-k8s-link-button"
          onClick={() => setDetail({ type: "workload", row })}
          type="button"
        >
          {row.name}
        </button>
      ),
    },
    { key: "namespace", header: t("kubernetes.table.namespace") },
    { key: "ready", header: t("kubernetes.table.ready") },
    {
      key: "restarts",
      header: t("kubernetes.table.restarts"),
      align: "right",
    },
    {
      key: "status",
      header: t("kubernetes.table.status"),
      render: (row) => (
        <StatusBadge tone={workloadStatusTone(row.status)}>
          {t(`kubernetes.health.${row.status}`)}
        </StatusBadge>
      ),
    },
    {
      key: "actions",
      header: t("kubernetes.table.actions"),
      render: (row) => (
        <Button
          onClick={() => setDetail({ type: "workload", row })}
          size="sm"
          variant="ghost"
        >
          {t("kubernetes.table.detail")}
        </Button>
      ),
    },
  ];

  const podColumns: Column<PodRow>[] = [
    {
      key: "name",
      header: t("kubernetes.table.name"),
      render: (row) => (
        <button
          className="argus-k8s-link-button"
          onClick={() => setDetail({ type: "pod", row })}
          type="button"
        >
          {row.name}
        </button>
      ),
    },
    { key: "namespace", header: t("kubernetes.table.namespace") },
    { key: "workload", header: t("kubernetes.table.kind") },
    {
      key: "ready",
      header: t("kubernetes.table.ready"),
      render: (row) => (
        <StatusBadge tone={row.ready === "Ready" ? "success" : "warning"}>
          {row.ready}
        </StatusBadge>
      ),
    },
    {
      key: "restarts",
      header: t("kubernetes.table.restarts"),
      align: "right",
    },
    {
      key: "actions",
      header: t("kubernetes.table.actions"),
      render: (row) => (
        <Button onClick={() => setLogPod(row)} size="sm" variant="ghost">
          {t("kubernetes.table.logs")}
        </Button>
      ),
    },
  ];

  const namespaceColumns: Column<NamespaceRow>[] = [
    {
      key: "name",
      header: t("kubernetes.table.name"),
      render: (row) => (
        <button
          className="argus-k8s-link-button"
          onClick={() => setDetail({ type: "namespace", row })}
          type="button"
        >
          {row.name}
        </button>
      ),
    },
    {
      key: "workloads",
      header: t("kubernetes.table.workloads"),
      align: "right",
    },
    {
      key: "restarts",
      header: t("kubernetes.table.restarts"),
      align: "right",
    },
  ];

  const nodeColumns: Column<NodeRow>[] = [
    { key: "name", header: t("kubernetes.table.node") },
    { key: "host", header: t("kubernetes.table.host") },
    {
      key: "status",
      header: t("kubernetes.table.status"),
      render: (row) => (
        <StatusBadge
          tone={bindingStatusTone(row.status as "proposed" | "verified" | "rejected")}
        >
          {t(`kubernetes.bindingStatus.${row.status}`)}
        </StatusBadge>
      ),
    },
    {
      key: "actions",
      header: t("kubernetes.table.actions"),
      render: (row) => (
        <Button
          onClick={() => setDetail({ type: "node", row })}
          size="sm"
          variant="ghost"
        >
          {t("kubernetes.table.detail")}
        </Button>
      ),
    },
  ];

  const empty = (
    <EmptyState
      description={t("kubernetes.emptyResources.description")}
      title={t("kubernetes.emptyResources.title")}
    />
  );

  const renderContent = (current: KindKey) => {
    if (workloadsQuery.isLoading) {
      return <Spinner label={t("common.loading")} />;
    }
    const workloadKind = WORKLOAD_KIND_MAP[current];
    if (workloadKind) {
      const rows = workloads
        .filter((workload) => workload.kind === workloadKind)
        .map(toWorkloadRow);
      if (rows.length === 0) return empty;
      return (
        <DataTable
          columns={workloadColumns}
          data={rows}
          getRowKey={(row) => row.id}
        />
      );
    }
    if (current === "pod") {
      if (podRows.length === 0) return empty;
      return (
        <DataTable
          columns={podColumns}
          data={podRows}
          getRowKey={(row) => row.id}
        />
      );
    }
    if (current === "namespace") {
      if (namespaceRows.length === 0) return empty;
      return (
        <DataTable
          columns={namespaceColumns}
          data={namespaceRows}
          getRowKey={(row) => row.id}
        />
      );
    }
    if (current === "node") {
      if (nodeRows.length === 0) return empty;
      return (
        <DataTable
          columns={nodeColumns}
          data={nodeRows}
          getRowKey={(row) => row.id}
        />
      );
    }
    return empty;
  };

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
            {renderContent(item)}
          </TabsContent>
        ))}
      </Tabs>
      <ResourceDetailDrawer
        onClose={() => setDetail(null)}
        selection={detail}
      />
      <PodLogsDialog onClose={() => setLogPod(null)} pod={logPod} />
    </>
  );
}
