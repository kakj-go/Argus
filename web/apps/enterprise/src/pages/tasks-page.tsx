import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { TaskStatus, TaskType, TaskViewModel } from "@argus/api-client/provisional";
import {
  useApi,
  type ActionOneTimeResult,
  type Execution,
} from "@argus/api-client";
import {
  Alert,
  Badge,
  Button,
  CodeBlock,
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
const realMode = import.meta.env.VITE_API_MODE === "real";

export function TasksPage() {
  return realMode ? <ExecutionsPage /> : <LegacyTasksPage />;
}

function ExecutionsPage() {
  const { t, i18n } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const [claimingExecution, setClaimingExecution] = useState<string | null>(null);
  const [oneTimeResult, setOneTimeResult] =
    useState<ActionOneTimeResult | null>(null);
  const [claimError, setClaimError] = useState("");
  const executions = useQuery({
    queryKey: ["executions"],
    queryFn: () => api.executions.list(),
  });
  const items = executions.data?.items ?? [];
  const terminal = items.filter(
    (item) => item.status === "succeeded" || item.status === "failed",
  );
  const successRate =
    terminal.length === 0
      ? null
      : Math.round(
          (terminal.filter((item) => item.status === "succeeded").length /
            terminal.length) *
            100,
        );
  const claimOneTimeResult = async (execution: Execution) => {
    if (claimingExecution) return;
    setClaimError("");
    setClaimingExecution(execution.execution_id);
    try {
      setOneTimeResult(
        await api.executions.claimOneTimeResult(execution.execution_id),
      );
      await queryClient.invalidateQueries({ queryKey: ["executions"] });
    } catch (error) {
      setClaimError(
        error instanceof Error ? error.message : t("governance.tasks.claimFailed"),
      );
    } finally {
      setClaimingExecution(null);
    }
  };
  return (
    <PageShell
      description={t("governance.tasks.description")}
      title={t("governance.tasks.title")}
    >
      <div className="argus-gov-stats">
        <StatCard label={t("governance.tasks.stats.today")} value={items.length} />
        <StatCard
          label={t("governance.tasks.stats.successRate")}
          value={successRate === null ? "—" : `${successRate}%`}
        />
        <StatCard
          label={t("governance.tasks.stats.running")}
          value={items.filter((item) => item.status === "running").length}
        />
        <StatCard
          label={t("governance.tasks.stats.failed")}
          value={items.filter((item) => item.status === "failed").length}
        />
      </div>
      {executions.isPending ? (
        <Spinner label={t("common.loading")} />
      ) : items.length === 0 ? (
        <EmptyState
          description={t("governance.tasks.emptyDescription")}
          title={t("governance.tasks.emptyTitle")}
        />
      ) : (
        <DataTable<Execution & Record<string, unknown>>
          columns={[
            { key: "execution_id", header: "Execution ID" },
            { key: "action_ref", header: "Action Ref" },
            {
              key: "status",
              header: t("governance.tasks.columns.status"),
              render: (item) => (
                <StatusBadge
                  pulse={item.status === "running"}
                  tone={executionTone(item.status)}
                >
                  {t(`governance.tasks.status.${item.status}`)}
                </StatusBadge>
              ),
            },
            { key: "result_ref", header: "Result Ref" },
            { key: "error_code", header: t("automations.errorCode") },
            {
              key: "one_time_result_available",
              header: t("governance.tasks.columns.oneTimeResult"),
              render: (item) =>
                item.one_time_result_available ? (
                  <Button
                    loading={claimingExecution === item.execution_id}
                    onClick={() => void claimOneTimeResult(item)}
                    variant="secondary"
                  >
                    {t("governance.tasks.claimOneTimeResult")}
                  </Button>
                ) : (
                  "—"
                ),
            },
            {
              key: "updated_at",
              header: t("governance.tasks.columns.startedAt"),
              render: (item) => formatDateTime(item.updated_at, i18n.language),
            },
          ]}
          data={items as Array<Execution & Record<string, unknown>>}
          getRowKey={(item) => item.execution_id}
        />
      )}
      {claimError && (
        <Alert
          description={claimError}
          title={t("governance.tasks.claimFailed")}
          tone="danger"
        />
      )}
      {oneTimeResult?.enrollment.install_command && (
        <section className="argus-gov-one-time-result">
          <Alert
            description={t("governance.tasks.oneTimeResultWarning")}
            title={t("governance.tasks.oneTimeResultTitle")}
            tone="warning"
          />
          <CodeBlock
            code={oneTimeResult.enrollment.install_command}
            language="bash"
          />
          <Button onClick={() => setOneTimeResult(null)} variant="secondary">
            {t("governance.tasks.dismissOneTimeResult")}
          </Button>
        </section>
      )}
      <Button
        onClick={() => queryClient.invalidateQueries({ queryKey: ["executions"] })}
        variant="secondary"
      >
        {t("governance.tasks.refresh")}
      </Button>
    </PageShell>
  );
}

function executionTone(status: Execution["status"]) {
  switch (status) {
    case "succeeded":
      return "success" as const;
    case "failed":
      return "danger" as const;
    case "result_unknown":
      return "warning" as const;
    case "running":
      return "info" as const;
    default:
      return "neutral" as const;
  }
}

/** 任务记录（路由 /tasks）：筛选列表 + 实时进度 + 详情抽屉。 */
function LegacyTasksPage() {
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
