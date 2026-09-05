import type { TaskViewModel } from "@argus/api-client/provisional";

type Translate = (key: string, options?: Record<string, unknown>) => string;

export function taskDisplayTitle(task: TaskViewModel, t: Translate): string {
  const type = t(`governance.tasks.type.${task.type}`);
  const resource = task.relatedResources.find((item) => item.name)?.name;
  return resource ? `${type} · ${resource}` : type;
}

export function taskStepDisplayTitle(index: number, t: Translate): string {
  return t("governance.tasks.detail.stepTitle", { index: index + 1 });
}
