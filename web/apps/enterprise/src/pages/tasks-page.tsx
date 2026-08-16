import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { TaskStatus, TaskType, TaskViewModel } from "@argus/api-client/provisional";
import { useApi } from "@argus/api-client";
import {
  Badge,
  DataTable,
  EmptyState,
  FilterBar,
  PageShell,
  Progress,
  Spinner,
  StatCard,
  StatusBadge,
} from "@argus/ui";
import "../i18n/governance";
import "../styles/governance.css";
import { TaskDetailDrawer } from "../components/governance/task-detail-drawer";
import {
  formatDateTime,
  formatDuration,
  isToday,
  taskStatusTone,
  useNow,
} from "../components/governance/utils";

const STATUS_FILTERS: TaskStatus[] = [
  "succeeded",
  "failed",
  "running",
  "pending",
  "cancelled",
];

const TYPE_FILTERS: TaskType[] = [
  "host_onboard",
  "host_command",
  "collector_install",
  "collector_upgrade",
  "kubernetes_change",
  "certificate_rotation",
  "generic",
];

/** 任务记录（路由 /tasks）：筛选列表 + 实时进度 + 详情抽屉。 */
export function TasksPage() {
  const { t, i18n } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const locale = i18n.resolvedLanguage === "en-US" ? "en-US" : "zh-CN";

  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [type, setType] = useState("");
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);

  const filter = useMemo(
    () => ({
      ...(search.trim() ? { query: search.trim() } : {}),
      ...(status ? { status: [status as TaskStatus] } : {}),
      ...(type ? { type: [type as TaskType] } : {}),
    }),
    [search, status, type],
  );

  const listQuery = useQuery({
    queryKey: ["tasks", "list", filter],
    queryFn: () => api.tasks.list(filter),
  });
  // 统计卡片基于全量列表派生，不受筛选影响。
  const statsQuery = useQuery({
    queryKey: ["tasks", "stats"],
    queryFn: () => api.tasks.list(),
  });

  // 全局任务推送：列表与统计原地刷新。
  useEffect(() => {
    return api.tasks.subscribe(() => {
      void queryClient.invalidateQueries({ queryKey: ["tasks"] });
    });
  }, [api, queryClient]);

  const tasks = listQuery.data?.items ?? [];
  const allTasks = statsQuery.data?.items ?? [];
  const now = useNow();

  const todayTasks = allTasks.filter((task) => isToday(task.createdAt));
  const finishedToday = todayTasks.filter(
    (task) => task.status === "succeeded" || task.status === "failed",
  );
  const successRate =
    finishedToday.length > 0
      ? Math.round(
          (todayTasks.filter((task) => task.status === "succeeded").length /
            finishedToday.length) *
            100,
        )
      : null;
  const runningCount = allTasks.filter(
    (task) => task.status === "running",
  ).length;
  const failedCount = allTasks.filter(
    (task) => task.status === "failed",
  ).length;

  return (
    <PageShell
      description={t("governance.tasks.description")}
      title={t("governance.tasks.title")}
    >
      <div className="argus-gov-stats">
        <StatCard
          label={t("governance.tasks.stats.today")}
          value={todayTasks.length}
        />
        <StatCard
          label={t("governance.tasks.stats.successRate")}
          tone={
            successRate !== null && successRate < 80 ? "warning" : "success"
          }
          value={successRate !== null ? `${successRate}%` : "—"}
        />
        <StatCard
          label={t("governance.tasks.stats.running")}
          tone={runningCount > 0 ? "info" : "neutral"}
          value={runningCount}
        />
        <StatCard
          label={t("governance.tasks.stats.failed")}
          tone={failedCount > 0 ? "danger" : "neutral"}
          value={failedCount}
        />
      </div>

      <FilterBar
        filters={[
          {
            key: "status",
            value: status,
            allLabel: t("governance.tasks.allStatuses"),
            options: STATUS_FILTERS.map((value) => ({
              value,
              label: t(`governance.tasks.status.${value}`),
            })),
            onChange: setStatus,
          },
          {
            key: "type",
            value: type,
            allLabel: t("governance.tasks.allTypes"),
            options: TYPE_FILTERS.map((value) => ({
              value,
              label: t(`governance.tasks.type.${value}`),
            })),
            onChange: setType,
          },
        ]}
        onRefresh={() => {
          void queryClient.invalidateQueries({ queryKey: ["tasks"] });
        }}
        refreshing={listQuery.isFetching}
        refreshLabel={t("governance.tasks.refresh")}
        search={{
          value: search,
          onChange: setSearch,
          placeholder: t("governance.tasks.searchPlaceholder"),
        }}
      />

      {listQuery.isPending ? (
        <Spinner label={t("common.loading")} />
      ) : tasks.length === 0 ? (
        <EmptyState
          description={t("governance.tasks.emptyDescription")}
          title={t("governance.tasks.emptyTitle")}
        />
      ) : (
        <DataTable
          columns={[
            {
              key: "name",
              header: t("governance.tasks.columns.name"),
              render: (row) => (
                <button
                  className="argus-task-name"
                  onClick={() => setSelectedTaskId(row.task.id)}
                  type="button"
                >
                  <span>{row.task.title}</span>
                  <span className="argus-task-name__type">
                    {t(`governance.tasks.type.${row.task.type}`)}
                  </span>
                </button>
              ),
            },
            {
              key: "resources",
              header: t("governance.tasks.columns.resources"),
              render: (row) =>
                row.task.relatedResources.length > 0 ? (
                  <span className="argus-task-resources">
                    {row.task.relatedResources.map((resource) => (
                      <Badge key={resource.id} tone="neutral">
                        {resource.name ?? resource.id}
                      </Badge>
                    ))}
                  </span>
                ) : (
                  <span>{t("governance.tasks.noResources")}</span>
                ),
            },
            {
              key: "status",
              header: t("governance.tasks.columns.status"),
              render: (row) => (
                <StatusBadge
                  pulse={row.task.status === "running"}
                  tone={taskStatusTone(row.task.status)}
                >
                  {t(`governance.tasks.status.${row.task.status}`)}
                </StatusBadge>
              ),
            },
            {
              key: "progress",
              header: t("governance.tasks.columns.progress"),
              render: (row) => (
                <Progress
                  tone={
                    row.task.status === "failed"
                      ? "danger"
                      : row.task.status === "succeeded"
                        ? "success"
                        : "accent"
                  }
                  value={row.task.progress}
                />
              ),
            },
            {
              key: "createdBy",
              header: t("governance.tasks.columns.createdBy"),
              render: (row) => row.task.createdByName ?? row.task.createdBy,
            },
            {
              key: "origin",
              header: t("governance.tasks.columns.origin"),
              render: (row) => (
                <Badge tone="neutral">
                  {t(`governance.tasks.origin.${row.task.origin}`)}
                </Badge>
              ),
            },
            {
              key: "startedAt",
              header: t("governance.tasks.columns.startedAt"),
              render: (row) =>
                row.task.startedAt
                  ? formatDateTime(row.task.startedAt, locale)
                  : t("governance.tasks.notStarted"),
            },
            {
              key: "duration",
              header: t("governance.tasks.columns.duration"),
              align: "right",
              render: (row) =>
                row.task.status === "running"
                  ? formatDuration(
                      row.task.startedAt,
                      new Date(now).toISOString(),
                    )
                  : formatDuration(row.task.startedAt, row.task.finishedAt),
            },
          ]}
          data={tasks.map((task: TaskViewModel) => ({ task }))}
          getRowKey={(row) => row.task.id}
        />
      )}

      <TaskDetailDrawer
        onOpenChange={(open) => {
          if (!open) setSelectedTaskId(null);
        }}
        open={selectedTaskId !== null}
        taskId={selectedTaskId}
      />
    </PageShell>
  );
}
