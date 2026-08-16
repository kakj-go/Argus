import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { TaskEvent, TaskViewModel } from "@argus/api-client/provisional";
import { useApi } from "@argus/api-client";
import {
  Button,
  ConfirmDialog,
  FormDrawer,
  KeyValueGrid,
  LogViewer,
  Progress,
  Spinner,
  StatusBadge,
  Timeline,
} from "@argus/ui";
import {
  formatDateTimeFull,
  formatDuration,
  isTaskActive,
  stepTimelineStatus,
  taskStatusTone,
  useNow,
} from "./utils";

/**
 * 任务详情抽屉：概要 + 步骤 + 日志。
 * 打开期间通过 tasks.subscribeTask 订阅推送，运行中任务原地推进。
 */
export function TaskDetailDrawer({
  taskId,
  open,
  onOpenChange,
}: {
  taskId: string | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t, i18n } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const [cancelConfirmOpen, setCancelConfirmOpen] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const now = useNow();
  const locale = i18n.resolvedLanguage === "en-US" ? "en-US" : "zh-CN";

  const detailQueryKey = ["tasks", "detail", taskId];
  const { data: task } = useQuery({
    queryKey: detailQueryKey,
    queryFn: () => api.tasks.get(taskId as string),
    enabled: open && taskId !== null,
  });

  useEffect(() => {
    if (!open || taskId === null) return;
    return api.tasks.subscribeTask(taskId, (event: TaskEvent) => {
      queryClient.setQueryData<TaskViewModel>(detailQueryKey, (current) => {
        if (!current) return current;
        switch (event.type) {
          case "task_updated":
            return event.task;
          case "step_updated":
            return {
              ...current,
              steps: current.steps.map((step) =>
                step.id === event.step.id ? event.step : step,
              ),
            };
          case "log_appended":
            return { ...current, logs: [...current.logs, event.entry] };
          default:
            return current;
        }
      });
      // 同步刷新外壳列表数据。
      void queryClient.invalidateQueries({ queryKey: ["tasks"] });
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [api, queryClient, open, taskId]);

  const cancelMutation = useMutation({
    mutationFn: (id: string) => api.tasks.cancel(id),
    onSuccess: () => {
      setCancelConfirmOpen(false);
      setActionError(null);
      void queryClient.invalidateQueries({ queryKey: ["tasks"] });
    },
    onError: () => setActionError(t("governance.tasks.actionFailed")),
  });

  const active = task ? isTaskActive(task.status) : false;
  const failedStep = task?.steps.find((step) => step.status === "failed");

  return (
    <FormDrawer
      description={task ? t(`governance.tasks.type.${task.type}`) : undefined}
      footer={
        <>
          {active && (
            <Button onClick={() => setCancelConfirmOpen(true)} variant="danger">
              {t("governance.tasks.detail.cancelTask")}
            </Button>
          )}
          <Button onClick={() => onOpenChange(false)} variant="secondary">
            {t("governance.tasks.detail.close")}
          </Button>
        </>
      }
      onOpenChange={onOpenChange}
      open={open}
      title={task?.title ?? t("governance.tasks.title")}
      width={560}
    >
      {!task ? (
        <Spinner label={t("common.loading")} />
      ) : (
        <div className="argus-task-detail">
          {task.error && (
            <div className="argus-task-detail__error" role="alert">
              {task.error}
            </div>
          )}

          <section>
            <h3 className="argus-gov-section-title">
              {t("governance.tasks.detail.overview")}
            </h3>
            <KeyValueGrid
              columns={2}
              items={[
                {
                  label: t("governance.tasks.columns.status"),
                  value: (
                    <StatusBadge
                      pulse={task.status === "running"}
                      tone={taskStatusTone(task.status)}
                    >
                      {t(`governance.tasks.status.${task.status}`)}
                    </StatusBadge>
                  ),
                },
                {
                  label: t("governance.tasks.columns.progress"),
                  value: (
                    <Progress
                      tone={
                        task.status === "failed"
                          ? "danger"
                          : task.status === "succeeded"
                            ? "success"
                            : "accent"
                      }
                      value={task.progress}
                    />
                  ),
                },
                {
                  label: t("governance.tasks.columns.createdBy"),
                  value: task.createdByName ?? task.createdBy,
                },
                {
                  label: t("governance.tasks.columns.origin"),
                  value: t(`governance.tasks.origin.${task.origin}`),
                },
                {
                  label: t("governance.tasks.detail.createdAt"),
                  value: formatDateTimeFull(task.createdAt, locale),
                },
                {
                  label: t("governance.tasks.detail.duration"),
                  value: active
                    ? formatDuration(
                        task.startedAt,
                        new Date(now).toISOString(),
                      )
                    : formatDuration(task.startedAt, task.finishedAt),
                },
                {
                  label: t("governance.tasks.detail.relatedResources"),
                  value:
                    task.relatedResources.length > 0
                      ? task.relatedResources
                          .map((resource) => resource.name ?? resource.id)
                          .join("、")
                      : t("governance.tasks.noResources"),
                },
                ...(task.pendingActionId
                  ? [
                      {
                        label: t("governance.tasks.detail.pendingAction"),
                        value: <code>{task.pendingActionId}</code>,
                      },
                    ]
                  : []),
              ]}
            />
          </section>

          <section>
            <h3 className="argus-gov-section-title">
              {t("governance.tasks.detail.steps")}
            </h3>
            <Timeline
              items={task.steps.map((step) => ({
                title: step.name,
                status: stepTimelineStatus(step.status),
                meta: (
                  <>
                    <span>{t(`governance.tasks.step.${step.status}`)}</span>
                    {step.startedAt && (
                      <span>
                        {" · "}
                        {formatDuration(
                          step.startedAt,
                          step.finishedAt ??
                            (step.status === "running"
                              ? new Date(now).toISOString()
                              : undefined),
                        )}
                      </span>
                    )}
                    {step.detail && (
                      <span
                        className={
                          step.status === "failed"
                            ? "argus-task-step-error"
                            : "argus-task-step-detail"
                        }
                      >
                        {" · "}
                        {step.detail}
                      </span>
                    )}
                  </>
                ),
              }))}
            />
            {failedStep?.detail && !task.error && (
              <div className="argus-task-detail__error" role="alert">
                {failedStep.name}: {failedStep.detail}
              </div>
            )}
          </section>

          <section>
            <h3 className="argus-gov-section-title">
              {t("governance.tasks.detail.logs")}
            </h3>
            <LogViewer
              autoScroll={active}
              fileName={`${task.id}.log`}
              height={240}
              lines={task.logs.map((entry) => ({
                timestamp: formatDateTimeFull(entry.timestamp, locale),
                level: entry.level,
                content: entry.message,
              }))}
            />
          </section>

          {actionError && (
            <p className="argus-approval-actions__error" role="alert">
              {actionError}
            </p>
          )}
        </div>
      )}

      <ConfirmDialog
        danger
        confirmLabel={t("governance.tasks.cancelDialog.confirm")}
        description={t("governance.tasks.cancelDialog.description")}
        loading={cancelMutation.isPending}
        onConfirm={() => taskId && cancelMutation.mutate(taskId)}
        onOpenChange={setCancelConfirmOpen}
        open={cancelConfirmOpen}
        title={t("governance.tasks.cancelDialog.title")}
      />
    </FormDrawer>
  );
}
