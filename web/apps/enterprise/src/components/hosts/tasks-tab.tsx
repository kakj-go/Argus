import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronUp } from "lucide-react";
import {
  auditPresentationKey,
  humanizeAuditCode,
  useApi,
  type AuditEvent,
  type Host,
} from "@argus/api-client";
import type { TaskStatus, TaskViewModel } from "@argus/api-client/provisional";
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  EmptyState,
  LogViewer,
  StatusBadge,
  Timeline,
  type Column,
} from "@argus/ui";
import { Table } from "./data-table";
import { formatDateTime } from "./host-utils";

const LIVE_STATUSES: TaskStatus[] = [
  "pending",
  "running",
  "waiting_input",
  "waiting_approval",
];

function taskTone(
  status: TaskStatus,
): "neutral" | "success" | "warning" | "danger" | "info" {
  switch (status) {
    case "succeeded":
      return "success";
    case "failed":
    case "timed_out":
      return "danger";
    case "running":
      return "info";
    case "waiting_input":
    case "waiting_approval":
      return "warning";
    default:
      return "neutral";
  }
}

/** 单个任务卡片：点击展开步骤 Timeline + LogViewer 日志。 */
function TaskCard({ task }: { task: TaskViewModel }) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const running = LIVE_STATUSES.includes(task.status);

  return (
    <Card>
      <CardHeader
        action={
          <Button
            onClick={() => setExpanded((value) => !value)}
            size="sm"
            variant="ghost"
          >
            {expanded ? (
              <>
                <ChevronUp aria-hidden size={14} />
                {t("hosts.tasks.collapse")}
              </>
            ) : (
              <>
                <ChevronDown aria-hidden size={14} />
                {t("hosts.tasks.expand")}
              </>
            )}
          </Button>
        }
        description={`${task.type} · ${task.createdByName ?? task.createdBy} · ${formatDateTime(task.createdAt)}`}
        title={
          <>
            {task.title}{" "}
            <StatusBadge pulse={running} tone={taskTone(task.status)}>
              {task.status}
            </StatusBadge>{" "}
            <Badge tone="neutral">{task.progress}%</Badge>
          </>
        }
      />
      {expanded && (
        <CardContent className="argus-task-detail">
          {task.steps.length > 0 && (
            <section className="argus-detail-section">
              <h3 className="argus-detail-section__title">
                {t("hosts.tasks.steps")}
              </h3>
              <Timeline
                items={task.steps.map((step) => ({
                  title: step.name,
                  meta: step.detail ?? step.status,
                  status:
                    step.status === "done"
                      ? "done"
                      : step.status === "failed"
                        ? "danger"
                        : step.status === "running"
                          ? "current"
                          : "pending",
                }))}
              />
            </section>
          )}
          {task.logs.length > 0 && (
            <section className="argus-detail-section">
              <h3 className="argus-detail-section__title">
                {t("hosts.tasks.logs")}
              </h3>
              <LogViewer
                fileName={`${task.id}.log`}
                height={220}
                lines={task.logs.map((entry) => ({
                  timestamp: new Date(entry.timestamp).toLocaleTimeString(),
                  level: entry.level,
                  content: entry.message,
                }))}
              />
            </section>
          )}
          {task.error && <p className="argus-muted">{task.error}</p>}
        </CardContent>
      )}
    </Card>
  );
}

/** 详情页「任务与审计」Tab：主机关联任务（实时订阅）+ 审计事件表。 */
export function TasksTab({ host }: { host: Host }) {
  const { t } = useTranslation();
  const api = useApi();
  // 订阅推送的实时任务快照，覆盖列表查询结果。
  const [liveTasks, setLiveTasks] = useState<Record<string, TaskViewModel>>({});

  const tasksQuery = useQuery({
    queryKey: ["tasks", "host", host.id],
    queryFn: () => api.tasks.list(undefined, { page: { limit: 50 } }),
  });
  const auditQuery = useQuery({
    queryKey: ["audit", "host", host.id],
    queryFn: () => api.audit.list({ resourceType: "host" }),
  });

  const hostTasks = useMemo(() => {
    const items = (tasksQuery.data?.items ?? []).filter((task) =>
      task.relatedResources.some(
        (ref) => ref.type === "host" && ref.id === host.id,
      ),
    );
    return items.map((task) => liveTasks[task.id] ?? task);
  }, [tasksQuery.data, liveTasks, host.id]);

  // 订阅进行中任务的实时更新（task_updated / step_updated / log_appended）。
  useEffect(() => {
    const running = hostTasks.filter((task) =>
      LIVE_STATUSES.includes(task.status),
    );
    if (running.length === 0) return;
    const unsubscribes = running.map((task) =>
      api.tasks.subscribeTask(task.id, () => {
        void api.tasks.get(task.id).then((fresh) => {
          setLiveTasks((previous) => ({ ...previous, [fresh.id]: fresh }));
        });
      }),
    );
    return () => {
      for (const unsubscribe of unsubscribes) unsubscribe();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [api, hostTasks.map((task) => `${task.id}:${task.status}`).join(",")]);

  const auditEvents: AuditEvent[] = (auditQuery.data?.items ?? []).filter(
    (event) => event.resourceId === host.id,
  );

  const auditColumns: Column<AuditEvent>[] = [
    {
      key: "createdAt",
      header: t("hosts.tasks.colCreatedAt"),
      render: (event) => formatDateTime(event.createdAt),
    },
    {
      key: "actorName",
      header: t("hosts.tasks.colActor"),
      render: (event) => event.actorName,
    },
    {
      key: "action",
      header: t("hosts.tasks.colAction"),
      render: (event) =>
        t(auditPresentationKey("settings.audit", "actions", event.action), {
          defaultValue: humanizeAuditCode(event.action),
        }),
    },
    {
      key: "summary",
      header: t("hosts.tasks.colSummary"),
      render: (event) =>
        t(auditPresentationKey("settings.audit", "actions", event.action), {
          defaultValue: humanizeAuditCode(event.action),
        }),
    },
    {
      key: "result",
      header: t("hosts.tasks.colResult"),
      render: (event) => (
        <StatusBadge
          tone={
            event.result === "success"
              ? "success"
              : event.result === "denied"
                ? "warning"
                : "danger"
          }
        >
          {t(`settings.audit.results.${event.result}`)}
        </StatusBadge>
      ),
    },
  ];

  return (
    <div className="argus-hosts-stack">
      <section className="argus-detail-section">
        <h3 className="argus-detail-section__title">
          {t("hosts.tasks.title")}
        </h3>
        {hostTasks.length > 0 ? (
          hostTasks.map((task) => <TaskCard key={task.id} task={task} />)
        ) : (
          <EmptyState description="" title={t("hosts.tasks.noTasks")} />
        )}
      </section>

      <Card>
        <CardHeader title={t("hosts.tasks.auditTitle")} />
        <CardContent>
          {auditEvents.length > 0 ? (
            <Table
              columns={auditColumns}
              data={auditEvents}
              getRowKey={(event) => event.id}
            />
          ) : (
            <EmptyState description="" title={t("hosts.tasks.noAudit")} />
          )}
        </CardContent>
      </Card>
    </div>
  );
}
