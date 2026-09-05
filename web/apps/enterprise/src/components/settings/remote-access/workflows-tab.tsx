import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { z } from "zod";
import type {
  ApprovalWorkflow,
  ApprovalWorkflowWrite,
} from "@argus/api-client";
import { useApi } from "@argus/api-client";
import {
  Alert,
  Button,
  CheckItem,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  Select,
  Switch,
} from "@argus/ui";
import { GovernanceList } from "./governance-list";

const empty: ApprovalWorkflowWrite = {
  name: "",
  description: "",
  approver_role_ids: [],
  minimum_approvals: 1,
  separation_of_duties: true,
  approval_timeout_seconds: 3600,
  escalation_after_seconds: 1800,
  timeout_effect: "reject",
  escalation_role_ids: [],
  status: "draft",
};

function RoleChecks({
  options,
  value,
  onChange,
}: {
  options: { id: string; name: string }[];
  value: string[];
  onChange(value: string[]): void;
}) {
  return (
    <div className="argus-governance-checks">
      {options.map((role) => {
        const checked = value.includes(role.id);
        return (
          <button
            aria-pressed={checked}
            key={role.id}
            onClick={() =>
              onChange(
                checked
                  ? value.filter((id) => id !== role.id)
                  : [...value, role.id],
              )
            }
            type="button"
          >
            <CheckItem checked={checked}>{role.name}</CheckItem>
          </button>
        );
      })}
    </div>
  );
}

export function WorkflowsTab() {
  const { t } = useTranslation();
  const api = useApi();
  const qc = useQueryClient();
  const query = useQuery({
    queryKey: ["remote-access", "workflows"],
    queryFn: () => api.remoteAccess.listApprovalWorkflows({ limit: 200 }),
  });
  const roles = useQuery({
    queryKey: ["org", "roles"],
    queryFn: () => api.org.listRoles(),
  });
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<ApprovalWorkflow | null>(null);
  const [form, setForm] = useState<ApprovalWorkflowWrite>(empty);
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
          approver_role_ids: z
            .array(z.string().min(1))
            .min(1, t("remoteAccess.validation.selectOne"))
            .max(64),
          minimum_approvals: z
            .number()
            .int()
            .min(1, t("remoteAccess.validation.range", { min: 1, max: 16 }))
            .max(16, t("remoteAccess.validation.range", { min: 1, max: 16 })),
          separation_of_duties: z.boolean(),
          approval_timeout_seconds: z
            .number()
            .int()
            .min(
              60,
              t("remoteAccess.validation.range", { min: 60, max: 604800 }),
            )
            .max(
              604800,
              t("remoteAccess.validation.range", { min: 60, max: 604800 }),
            ),
          escalation_after_seconds: z
            .number()
            .int()
            .min(
              30,
              t("remoteAccess.validation.range", { min: 30, max: 604799 }),
            )
            .max(
              604799,
              t("remoteAccess.validation.range", { min: 30, max: 604799 }),
            ),
          timeout_effect: z.enum(["reject", "expire"]),
          escalation_role_ids: z.array(z.string().min(1)).max(64),
          status: z.literal("draft"),
        })
        .superRefine((value, ctx) => {
          if (value.minimum_approvals > value.approver_role_ids.length)
            ctx.addIssue({
              code: "custom",
              path: ["minimum_approvals"],
              message: t("remoteAccess.validation.approvalCount"),
            });
          if (value.escalation_after_seconds >= value.approval_timeout_seconds)
            ctx.addIssue({
              code: "custom",
              path: ["escalation_after_seconds"],
              message: t("remoteAccess.validation.range", {
                min: 30,
                max: value.approval_timeout_seconds - 1,
              }),
            });
          if (
            value.escalation_role_ids.some((id) =>
              value.approver_role_ids.includes(id),
            )
          )
            ctx.addIssue({
              code: "custom",
              path: ["escalation_role_ids"],
              message: t("remoteAccess.validation.selectOne"),
            });
        }),
    [t],
  );
  const validatedForm = useForm<ApprovalWorkflowWrite>({
    resolver: zodResolver(schema),
    defaultValues: empty,
  });
  useEffect(() => {
    if (!open) return;
    const value = editing
      ? {
          name: editing.name,
          description: editing.description,
          approver_role_ids: editing.approver_role_ids,
          minimum_approvals: editing.minimum_approvals,
          separation_of_duties: editing.separation_of_duties,
          approval_timeout_seconds: editing.approval_timeout_seconds,
          escalation_after_seconds: editing.escalation_after_seconds,
          timeout_effect: editing.timeout_effect,
          escalation_role_ids: editing.escalation_role_ids,
          status: "draft" as const,
        }
      : empty;
    setForm(value);
    validatedForm.reset(value);
  }, [editing, open, validatedForm]);
  const invalidate = async () => {
    await qc.invalidateQueries({ queryKey: ["remote-access", "workflows"] });
  };
  const save = useMutation({
    mutationFn: () => {
      if (!editing) return api.remoteAccess.createApprovalWorkflow(form);
      const { status: _status, ...input } = form;
      void _status;
      return api.remoteAccess.updateApprovalWorkflow(editing.id, {
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
  const set = <K extends keyof ApprovalWorkflowWrite>(
    key: K,
    value: ApprovalWorkflowWrite[K],
  ) =>
    setForm((current) => {
      const next = { ...current, [key]: value };
      validatedForm.reset(next, { keepErrors: true });
      return next;
    });
  const items = query.data?.items ?? [];
  const roleOptions = (roles.data ?? []).map((role) => ({
    id: role.id,
    name: role.name,
  }));
  return (
    <div className="argus-settings-section">
      <div className="argus-settings-section__head">
        <h2 className="argus-settings-section__title">
          {t("remoteAccess.workflows")}
        </h2>
        <Button
          onClick={() => {
            setEditing(null);
            setOpen(true);
          }}
          size="sm"
          variant="primary"
        >
          {t("remoteAccess.newWorkflow")}
        </Button>
      </div>
      {items.length === 0 ? (
        <EmptyState description="" title={t("remoteAccess.noWorkflows")} />
      ) : (
        <GovernanceList
          extraColumns={[
            {
              key: "approvals",
              header: t("remoteAccess.minimumApprovals"),
              render: (row) => row.minimum_approvals,
            },
            {
              key: "timeout",
              header: t("remoteAccess.approvalTimeout"),
              render: (row) =>
                `${row.approval_timeout_seconds}s / ${row.timeout_effect}`,
            },
          ]}
          items={items}
          onArchive={(id) =>
            lifecycle(api.remoteAccess.archiveApprovalWorkflow, id)
          }
          onDisable={(id) =>
            lifecycle(api.remoteAccess.disableApprovalWorkflow, id)
          }
          onEdit={(item) => {
            setEditing(item);
            setOpen(true);
          }}
          onEnable={(id) =>
            lifecycle(api.remoteAccess.enableApprovalWorkflow, id)
          }
          onRestore={(id) =>
            lifecycle(api.remoteAccess.restoreApprovalWorkflow, id)
          }
          references={api.remoteAccess.getApprovalWorkflowReferences}
        />
      )}
      <FormDrawer
        description={t("remoteAccess.workflowDescription")}
        loading={save.isPending}
        onOpenChange={setOpen}
        onSubmit={validatedForm.handleSubmit(() => save.mutate())}
        open={open}
        submitLabel={t("remoteAccess.save")}
        title={
          editing
            ? t("remoteAccess.editWorkflow")
            : t("remoteAccess.newWorkflow")
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
          controlMode="group"
          error={validatedForm.formState.errors.approver_role_ids?.message}
          label={t("remoteAccess.approverRoles")}
          requirement="required"
        >
          <RoleChecks
            onChange={(value) => set("approver_role_ids", value)}
            options={roleOptions}
            value={form.approver_role_ids}
          />
        </Field>
        <Field
          error={validatedForm.formState.errors.minimum_approvals?.message}
          label={t("remoteAccess.minimumApprovals")}
          requirement="required"
        >
          <Input
            max={16}
            min={1}
            onChange={(event) =>
              set("minimum_approvals", Number(event.target.value))
            }
            type="number"
            value={form.minimum_approvals}
          />
        </Field>
        <div className="argus-profile-switch">
          <div>
            <strong>{t("remoteAccess.separationOfDuties")}</strong>
            <p>{t("remoteAccess.separationHelp")}</p>
            <small>
              {t("remoteAccess.currentValue", {
                value: form.separation_of_duties
                  ? t("remoteAccess.enabled")
                  : t("remoteAccess.disabled"),
              })}
            </small>
          </div>
          <Switch
            checked={form.separation_of_duties}
            label={t("remoteAccess.separationOfDuties")}
            onChange={(value) => set("separation_of_duties", value)}
          />
        </div>
        <Field
          error={
            validatedForm.formState.errors.approval_timeout_seconds?.message
          }
          label={t("remoteAccess.approvalTimeout")}
          requirement="required"
        >
          <Input
            max={604800}
            min={60}
            onChange={(event) =>
              set("approval_timeout_seconds", Number(event.target.value))
            }
            type="number"
            value={form.approval_timeout_seconds}
          />
        </Field>
        <Field
          error={
            validatedForm.formState.errors.escalation_after_seconds?.message
          }
          label={t("remoteAccess.escalationAfter")}
          requirement="required"
        >
          <Input
            max={604799}
            min={30}
            onChange={(event) =>
              set("escalation_after_seconds", Number(event.target.value))
            }
            type="number"
            value={form.escalation_after_seconds}
          />
        </Field>
        <Field
          controlMode="group"
          error={validatedForm.formState.errors.escalation_role_ids?.message}
          label={t("remoteAccess.escalationRoles")}
          requirement="optional"
        >
          <RoleChecks
            onChange={(value) => set("escalation_role_ids", value)}
            options={roleOptions.filter(
              (role) => !form.approver_role_ids.includes(role.id),
            )}
            value={form.escalation_role_ids}
          />
        </Field>
        <Field label={t("remoteAccess.timeoutEffect")} requirement="required">
          <Select
            ariaLabel={t("remoteAccess.timeoutEffect")}
            onValueChange={(value) =>
              set("timeout_effect", value as "reject" | "expire")
            }
            options={[
              { value: "reject", label: t("remoteAccess.timeout.reject") },
              { value: "expire", label: t("remoteAccess.timeout.expire") },
            ]}
            value={form.timeout_effect}
          />
        </Field>
      </FormDrawer>
    </div>
  );
}
