import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";
import { z } from "zod";
import { useApi, type Automation, type AutomationWrite } from "@argus/api-client";
import {
  Button,
  DataTable,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  PageShell,
  Select,
  StatusBadge,
  Textarea,
} from "@argus/ui";
import { formatDateTime } from "../components/settings/shared";
import "../styles/ai-settings.css";

const toolIds = [
  "host.list",
  "host.get",
  "kubernetes.cluster.list",
  "kubernetes.cluster.get",
  "connector.list",
  "connector.get",
  "host.create.preview",
  "host.update.preview",
  "host.delete.preview",
  "kubernetes.cluster.create.preview",
  "kubernetes.cluster.update.preview",
  "kubernetes.cluster.delete.preview",
] as const;

const automationSchema = z.object({
  name: z.string().trim().min(1).max(128),
  service_account_id: z.string().uuid(),
  tool_id: z.enum(toolIds),
  tool_input: z.string().refine((value) => {
    try {
      const parsed: unknown = JSON.parse(value);
      return Boolean(parsed) && typeof parsed === "object" && !Array.isArray(parsed);
    } catch {
      return false;
    }
  }),
  cron: z
    .string()
    .trim()
    .refine((value) => value.split(/\s+/).length === 5),
  timezone: z.string().trim().min(1).max(128),
});

type AutomationForm = z.infer<typeof automationSchema>;

export function AutomationsPage() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<Automation | null | undefined>();
  const [selected, setSelected] = useState<Automation | null>(null);
  const automations = useQuery({
    queryKey: ["automations"],
    queryFn: () => api.automations.list(),
  });
  const runs = useQuery({
    queryKey: ["automations", selected?.id, "runs"],
    queryFn: () => api.automations.listRuns(selected!.id),
    enabled: Boolean(selected),
  });
  const changeState = useMutation({
    mutationFn: (automation: Automation) =>
      automation.status === "enabled"
        ? api.automations.disable(automation.id, automation.version)
        : api.automations.enable(automation.id, automation.version),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["automations"] }),
  });

  return (
    <PageShell
      description={t("automations.description")}
      title={t("automations.title")}
    >
      <div className="argus-ai-stack">
        <div className="argus-ai-toolbar">
          <span />
          <Button onClick={() => setEditing(null)} variant="primary">
            <Plus size={15} />
            {t("automations.create")}
          </Button>
        </div>
        {(automations.data ?? []).length === 0 ? (
          <EmptyState description="" title={t("automations.empty")} />
        ) : (
          <DataTable<Automation & Record<string, unknown>>
            columns={[
              { key: "name", header: t("automations.name") },
              { key: "tool_id", header: t("automations.tool") },
              { key: "cron", header: t("automations.schedule") },
              { key: "timezone", header: t("automations.timezone") },
              {
                key: "next_run_at",
                header: t("automations.nextRun"),
                render: (item) => formatDateTime(item.next_run_at),
              },
              {
                key: "status",
                header: t("automations.status"),
                render: (item) => (
                  <StatusBadge tone={item.status === "enabled" ? "success" : "neutral"}>
                    {t(`automations.states.${item.status}`)}
                  </StatusBadge>
                ),
              },
              {
                key: "actions",
                header: t("automations.actions"),
                render: (item) => (
                  <span className="argus-ai-actions">
                    <Button onClick={() => setSelected(item)} size="sm" variant="ghost">
                      {t("automations.runs")}
                    </Button>
                    <Button onClick={() => setEditing(item)} size="sm" variant="ghost">
                      {t("automations.edit")}
                    </Button>
                    <Button
                      onClick={() => changeState.mutate(item)}
                      size="sm"
                      variant="ghost"
                    >
                      {item.status === "enabled"
                        ? t("automations.disable")
                        : t("automations.enable")}
                    </Button>
                  </span>
                ),
              },
            ]}
            data={(automations.data ?? []) as Array<Automation & Record<string, unknown>>}
            getRowKey={(item) => item.id}
          />
        )}
        {selected && (
          <DataTable<Record<string, unknown>>
            columns={[
              { key: "scheduled_for", header: t("automations.scheduledFor") },
              { key: "status", header: t("automations.status") },
              { key: "pending_action_ref", header: t("automations.actionRef") },
              { key: "result_ref", header: t("automations.resultRef") },
              { key: "error_code", header: t("automations.errorCode") },
            ]}
            data={(runs.data ?? []) as Array<Record<string, unknown>>}
            getRowKey={(item) => String(item.id)}
          />
        )}
      </div>
      {editing !== undefined && (
        <AutomationDrawer
          automation={editing}
          onClose={() => setEditing(undefined)}
        />
      )}
    </PageShell>
  );
}

function AutomationDrawer({
  automation,
  onClose,
}: {
  automation: Automation | null;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const accounts = useQuery({
    queryKey: ["org", "serviceAccounts"],
    queryFn: () => api.org.listServiceAccounts(),
  });
  const form = useForm<AutomationForm>({
    resolver: zodResolver(automationSchema),
    defaultValues: {
      name: automation?.name ?? "",
      service_account_id: automation?.service_account_id ?? "",
      tool_id: (automation?.tool_id as AutomationForm["tool_id"]) ?? "host.list",
      tool_input: JSON.stringify(automation?.tool_input ?? {}, null, 2),
      cron: automation?.cron ?? "0 * * * *",
      timezone: automation?.timezone ?? "Asia/Shanghai",
    },
  });
  const save = useMutation({
    mutationFn: (value: AutomationForm) => {
      const input: AutomationWrite = {
        name: value.name,
        service_account_id: value.service_account_id,
        tool_id: value.tool_id,
        tool_input: JSON.parse(value.tool_input) as Record<string, unknown>,
        cron: value.cron,
        timezone: value.timezone,
        expected_version: automation?.version,
      };
      return automation
        ? api.automations.update(automation.id, input)
        : api.automations.create(input);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["automations"] });
      onClose();
    },
  });
  const errors = form.formState.errors;

  return (
    <FormDrawer
      loading={save.isPending}
      onOpenChange={(open) => !open && onClose()}
      onSubmit={form.handleSubmit((value) => save.mutate(value))}
      open
      submitLabel={t("automations.save")}
      title={automation ? t("automations.editTitle") : t("automations.createTitle")}
    >
      <div className="argus-settings-form">
        <Field error={errors.name?.message} label={t("automations.name")}>
          <Input {...form.register("name")} />
        </Field>
        <Field
          error={errors.service_account_id?.message}
          label={t("automations.serviceAccount")}
        >
          <Select
            onValueChange={(value) =>
              form.setValue("service_account_id", value, { shouldValidate: true })
            }
            options={[
              { value: "", label: t("automations.selectAccount") },
              ...(accounts.data ?? [])
              .filter((item) => item.status === "active")
              .map((item) => ({ value: item.id, label: item.name })),
            ]}
            value={form.watch("service_account_id")}
          />
        </Field>
        <Field error={errors.tool_id?.message} label={t("automations.tool")}>
          <Select
            onValueChange={(value) =>
              form.setValue("tool_id", value as AutomationForm["tool_id"], {
                shouldValidate: true,
              })
            }
            options={toolIds.map((tool) => ({ value: tool, label: tool }))}
            value={form.watch("tool_id")}
          />
        </Field>
        <Field error={errors.tool_input?.message} label={t("automations.input")}>
          <Textarea rows={8} {...form.register("tool_input")} />
        </Field>
        <div className="argus-form-row">
          <Field error={errors.cron?.message} label={t("automations.schedule")}>
            <Input {...form.register("cron")} />
          </Field>
          <Field error={errors.timezone?.message} label={t("automations.timezone")}>
            <Input {...form.register("timezone")} />
          </Field>
        </div>
      </div>
    </FormDrawer>
  );
}
