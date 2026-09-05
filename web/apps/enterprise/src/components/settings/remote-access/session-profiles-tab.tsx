import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { z } from "zod";
import type { SessionProfile, SessionProfileWrite } from "@argus/api-client";
import { useApi } from "@argus/api-client";
import {
  Alert,
  Button,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  Select,
  Switch,
} from "@argus/ui";
import { GovernanceList } from "./governance-list";

const defaults: SessionProfileWrite = {
  name: "",
  description: "",
  max_session_seconds: 3600,
  idle_timeout_seconds: 600,
  recording_mode: "required",
  command_audit_mode: "required",
  clipboard_mode: "disabled",
  file_upload_mode: "disabled",
  file_download_mode: "disabled",
  port_forward_mode: "disabled",
  session_share_mode: "disabled",
  retention_days: 180,
  status: "draft",
};
const advanced = [
  "clipboard_mode",
  "file_upload_mode",
  "file_download_mode",
  "port_forward_mode",
  "session_share_mode",
] as const;

export function SessionProfilesTab() {
  const { t } = useTranslation();
  const api = useApi();
  const qc = useQueryClient();
  const query = useQuery({
    queryKey: ["remote-access", "profiles"],
    queryFn: () => api.remoteAccess.listSessionProfiles({ limit: 200 }),
  });
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<SessionProfile | null>(null);
  const [form, setForm] = useState<SessionProfileWrite>(defaults);
  const schema = useMemo(
    () =>
      z
        .object({
          name: z
            .string()
            .trim()
            .min(1, t("remoteAccess.validation.required"))
            .max(128),
          description: z.string().max(2048),
          max_session_seconds: z
            .number()
            .int()
            .min(
              60,
              t("remoteAccess.validation.range", { min: 60, max: 86400 }),
            )
            .max(
              86400,
              t("remoteAccess.validation.range", { min: 60, max: 86400 }),
            ),
          idle_timeout_seconds: z
            .number()
            .int()
            .min(
              60,
              t("remoteAccess.validation.range", { min: 60, max: 86400 }),
            )
            .max(
              86400,
              t("remoteAccess.validation.range", { min: 60, max: 86400 }),
            ),
          recording_mode: z.enum(["required", "optional", "disabled"]),
          command_audit_mode: z.enum(["required", "optional", "disabled"]),
          clipboard_mode: z.enum(["enabled", "disabled"]),
          file_upload_mode: z.enum(["enabled", "disabled"]),
          file_download_mode: z.enum(["enabled", "disabled"]),
          port_forward_mode: z.enum(["enabled", "disabled"]),
          session_share_mode: z.enum(["enabled", "disabled"]),
          retention_days: z
            .number()
            .int()
            .min(1, t("remoteAccess.validation.range", { min: 1, max: 3650 }))
            .max(
              3650,
              t("remoteAccess.validation.range", { min: 1, max: 3650 }),
            ),
          status: z.literal("draft"),
        })
        .superRefine((value, ctx) => {
          if (value.idle_timeout_seconds > value.max_session_seconds)
            ctx.addIssue({
              code: "custom",
              path: ["idle_timeout_seconds"],
              message: t("remoteAccess.validation.range", {
                min: 60,
                max: value.max_session_seconds,
              }),
            });
          if (advanced.some((key) => value[key] === "enabled"))
            ctx.addIssue({
              code: "custom",
              path: ["clipboard_mode"],
              message: t("remoteAccess.advancedChannelUnavailable"),
            });
        }),
    [t],
  );
  const validatedForm = useForm<SessionProfileWrite>({
    resolver: zodResolver(schema),
    defaultValues: defaults,
  });
  useEffect(() => {
    if (!open) return;
    const value = editing
      ? {
          name: editing.name,
          description: editing.description,
          max_session_seconds: editing.max_session_seconds,
          idle_timeout_seconds: editing.idle_timeout_seconds,
          recording_mode: editing.recording_mode,
          command_audit_mode: editing.command_audit_mode,
          clipboard_mode: editing.clipboard_mode,
          file_upload_mode: editing.file_upload_mode,
          file_download_mode: editing.file_download_mode,
          port_forward_mode: editing.port_forward_mode,
          session_share_mode: editing.session_share_mode,
          retention_days: editing.retention_days,
          status: "draft" as const,
        }
      : defaults;
    setForm(value);
    validatedForm.reset(value);
  }, [editing, open, validatedForm]);
  const invalidate = async () => {
    await qc.invalidateQueries({ queryKey: ["remote-access", "profiles"] });
  };
  const save = useMutation({
    mutationFn: () => {
      if (!editing) return api.remoteAccess.createSessionProfile(form);
      const { status: _status, ...input } = form;
      void _status;
      return api.remoteAccess.updateSessionProfile(editing.id, {
        ...input,
        expected_version: editing.version,
      });
    },
    onSuccess: async () => {
      setOpen(false);
      setEditing(null);
      await invalidate();
    },
  });
  const lifecycle = async (
    fn: (id: string) => Promise<unknown>,
    id: string,
  ) => {
    await fn(id);
    await invalidate();
  };
  const set = <K extends keyof SessionProfileWrite>(
    key: K,
    value: SessionProfileWrite[K],
  ) =>
    setForm((current) => {
      const next = { ...current, [key]: value };
      validatedForm.reset(next, { keepErrors: true });
      return next;
    });
  const items = query.data?.items ?? [];
  return (
    <div className="argus-settings-section">
      <div className="argus-settings-section__head">
        <h2 className="argus-settings-section__title">
          {t("remoteAccess.sessionProfiles")}
        </h2>
        <Button
          onClick={() => {
            setEditing(null);
            setOpen(true);
          }}
          size="sm"
          variant="primary"
        >
          {t("remoteAccess.newSessionProfile")}
        </Button>
      </div>
      {items.length === 0 ? (
        <EmptyState
          description=""
          title={t("remoteAccess.noSessionProfiles")}
        />
      ) : (
        <GovernanceList
          extraColumns={[
            {
              key: "duration",
              header: t("remoteAccess.limits"),
              render: (row) =>
                `${row.max_session_seconds / 60}m / ${row.idle_timeout_seconds / 60}m`,
            },
            {
              key: "recording",
              header: t("remoteAccess.recording"),
              render: (row) =>
                `${row.recording_mode} · ${row.command_audit_mode}`,
            },
          ]}
          items={items}
          onArchive={(id) =>
            lifecycle(api.remoteAccess.archiveSessionProfile, id)
          }
          onDisable={(id) =>
            lifecycle(api.remoteAccess.disableSessionProfile, id)
          }
          onEdit={(item) => {
            setEditing(item);
            setOpen(true);
          }}
          onEnable={(id) =>
            lifecycle(api.remoteAccess.enableSessionProfile, id)
          }
          onRestore={(id) =>
            lifecycle(api.remoteAccess.restoreSessionProfile, id)
          }
          references={api.remoteAccess.getSessionProfileReferences}
        />
      )}
      <FormDrawer
        description={t("remoteAccess.sessionProfileDescription")}
        loading={save.isPending}
        onOpenChange={setOpen}
        onSubmit={validatedForm.handleSubmit(() => save.mutate())}
        open={open}
        submitLabel={t("remoteAccess.save")}
        title={
          editing
            ? t("remoteAccess.editSessionProfile")
            : t("remoteAccess.newSessionProfile")
        }
      >
        {save.isError && (
          <Alert
            description={t("remoteAccess.saveFailedDescription")}
            title={t("settings.common.saveFailed")}
            tone="danger"
          />
        )}
        <Field
          error={validatedForm.formState.errors.name?.message}
          label={t("remoteAccess.name")}
          requirement="required"
        >
          <Input
            onChange={(event) => set("name", event.target.value)}
            value={form.name}
          />
        </Field>
        <Field label={t("remoteAccess.description")} requirement="optional">
          <Input
            onChange={(event) => set("description", event.target.value)}
            value={form.description}
          />
        </Field>
        <Field
          error={validatedForm.formState.errors.max_session_seconds?.message}
          label={t("remoteAccess.maxSessionSeconds")}
          requirement="required"
        >
          <Input
            max={86400}
            min={60}
            onChange={(event) =>
              set("max_session_seconds", Number(event.target.value))
            }
            type="number"
            value={form.max_session_seconds}
          />
        </Field>
        <Field
          error={validatedForm.formState.errors.idle_timeout_seconds?.message}
          label={t("remoteAccess.idleTimeoutSeconds")}
          requirement="required"
        >
          <Input
            max={86400}
            min={60}
            onChange={(event) =>
              set("idle_timeout_seconds", Number(event.target.value))
            }
            type="number"
            value={form.idle_timeout_seconds}
          />
        </Field>
        {(["recording_mode", "command_audit_mode"] as const).map((key) => (
          <Field
            key={key}
            label={t(`remoteAccess.${key}`)}
            requirement="required"
          >
            <Select
              ariaLabel={t(`remoteAccess.${key}`)}
              onValueChange={(value) =>
                set(key, value as SessionProfileWrite[typeof key])
              }
              options={["required", "optional", "disabled"].map((value) => ({
                value,
                label: t(`remoteAccess.mode.${value}`),
              }))}
              value={form[key]}
            />
          </Field>
        ))}
        <Alert
          description={t("remoteAccess.advancedChannelRisk")}
          title={t("remoteAccess.advancedChannels")}
          tone="warning"
        />
        {advanced.map((key) => (
          <div className="argus-profile-switch" key={key}>
            <div>
              <strong>{t(`remoteAccess.${key}`)}</strong>
              <p>{t(`remoteAccess.channelHelp.${key}`)}</p>
              <small>{t("remoteAccess.advancedChannelUnavailable")}</small>
            </div>
            <Switch
              checked={false}
              disabled
              label={t(`remoteAccess.${key}`)}
              onChange={() => undefined}
            />
          </div>
        ))}
        <Field
          error={validatedForm.formState.errors.retention_days?.message}
          label={t("remoteAccess.retentionDays")}
          requirement="required"
        >
          <Input
            max={3650}
            min={1}
            onChange={(event) =>
              set("retention_days", Number(event.target.value))
            }
            type="number"
            value={form.retention_days}
          />
        </Field>
      </FormDrawer>
    </div>
  );
}
