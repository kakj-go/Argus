import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useMemo, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import { formConstraint, presentApiFormError, useApi } from "@argus/api-client";
import type { ApprovalPolicy } from "@argus/api-client";
import {
  Alert,
  Badge,
  Button,
  CheckItem,
  DataTable,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  RowAction,
  Spinner,
  StatusBadge,
  Switch,
} from "@argus/ui";
import { roleDisplayName } from "../../lib/role-presentation";
import { useOrgRoles } from "./org-users-tab";

type ApprovalRiskLevel = ApprovalPolicy["matchRiskLevels"][number];

const RISK_LEVELS: ApprovalRiskLevel[] = ["write", "dangerous", "critical"];
const policyConstraints = {
  name: formConstraint("ApprovalPolicyWrite", "name"),
  approverRoleIds: formConstraint("ApprovalPolicyWrite", "approver_role_ids"),
  minimumApprovers: formConstraint("ApprovalPolicyWrite", "minimum_approvers"),
  risks: formConstraint("ApprovalPolicyWrite", "risks"),
};

type PolicyRow = {
  id: string;
  name: string;
  riskLevels: ApprovalRiskLevel[];
  minApprovers: number;
  approverRoleIds: string[];
  separationOfDuty: boolean;
  enabled: boolean;
};

export function OrgPoliciesTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const policies = useQuery({
    queryKey: ["org", "approvalPolicies"],
    queryFn: () => api.org.listApprovalPolicies(),
  });
  const roles = useOrgRoles();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editing, setEditing] = useState<ApprovalPolicy | null>(null);

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["org", "approvalPolicies"] });

  const save = useMutation({
    mutationFn: (
      input: Omit<ApprovalPolicy, "id" | "enterpriseId" | "createdAt"> & {
        id?: string;
      },
    ) => api.org.saveApprovalPolicy(input),
    onSuccess: () => {
      setDrawerOpen(false);
      setEditing(null);
      void invalidate();
    },
  });

  const roleName = (id: string) => {
    const role = roles.data?.find((item) => item.id === id);
    return role ? roleDisplayName(role, t) : id;
  };

  const rows: PolicyRow[] = (policies.data ?? []).map((policy) => ({
    id: policy.id,
    name: policy.name,
    riskLevels: policy.matchRiskLevels,
    minApprovers: policy.minApprovers,
    approverRoleIds: policy.approverRoleIds,
    separationOfDuty: policy.separationOfDuty,
    enabled: policy.enabled,
  }));

  return (
    <div className="argus-settings-section">
      <div className="argus-settings-section__head">
        <h2 className="argus-settings-section__title">
          {t("settings.org.tabs.policies")}
        </h2>
        <Button
          onClick={() => {
            setEditing(null);
            setDrawerOpen(true);
          }}
          size="sm"
          variant="primary"
        >
          {t("settings.org.policiesTab.create")}
        </Button>
      </div>
      {policies.isPending ? (
        <Spinner />
      ) : rows.length === 0 ? (
        <EmptyState
          description=""
          title={t("settings.org.policiesTab.empty")}
        />
      ) : (
        <DataTable<PolicyRow>
          columns={[
            { key: "name", header: t("settings.common.name") },
            {
              key: "riskLevels",
              header: t("settings.org.policiesTab.matchRiskLevels"),
              render: (row) =>
                row.riskLevels.length === 0 ? (
                  t("settings.common.all")
                ) : (
                  <span className="argus-settings-inline-actions">
                    {row.riskLevels.map((level) => (
                      <Badge
                        key={level}
                        tone={
                          level === "critical"
                            ? "danger"
                            : level === "dangerous"
                              ? "warning"
                              : "neutral"
                        }
                      >
                        {t(`settings.org.policiesTab.riskLevels.${level}`)}
                      </Badge>
                    ))}
                  </span>
                ),
            },
            {
              key: "minApprovers",
              header: t("settings.org.policiesTab.minApprovers"),
              render: (row) => String(row.minApprovers),
            },
            {
              key: "approverRoleIds",
              header: t("settings.org.policiesTab.approverRoles"),
              render: (row) =>
                row.approverRoleIds.map(roleName).join(", ") || "—",
            },
            {
              key: "enabled",
              header: t("settings.common.status"),
              render: (row) => (
                <StatusBadge tone={row.enabled ? "success" : "neutral"}>
                  {row.enabled
                    ? t("settings.common.enabled")
                    : t("settings.common.disabled")}
                </StatusBadge>
              ),
            },
            {
              key: "actions",
              header: t("settings.common.actions"),
              render: (row) => (
                <RowAction
                  onClick={() => {
                    setEditing(
                      policies.data?.find((policy) => policy.id === row.id) ??
                        null,
                    );
                    setDrawerOpen(true);
                  }}
                >
                  {t("settings.common.edit")}
                </RowAction>
              ),
            },
          ]}
          data={rows}
          getRowKey={(row) => row.id}
        />
      )}

      <PolicyDrawer
        loading={save.isPending}
        onOpenChange={(open) => {
          setDrawerOpen(open);
          if (!open) setEditing(null);
        }}
        onSubmit={(input) => save.mutateAsync({ ...input, id: editing?.id })}
        open={drawerOpen}
        policy={editing}
        roles={(roles.data ?? []).map((role) => ({
          id: role.id,
          label: roleDisplayName(role, t),
        }))}
      />
    </div>
  );
}

function toggleInList<T>(list: T[], item: T): T[] {
  return list.includes(item)
    ? list.filter((entry) => entry !== item)
    : [...list, item];
}

function CheckRow<T>({
  list,
  onToggle,
  options,
}: {
  list: T[];
  onToggle: (value: T) => void;
  options: Array<{ value: T; label: string }>;
}) {
  return (
    <div className="argus-settings-check-row">
      {options.map((option) => (
        <span
          aria-checked={list.includes(option.value)}
          key={String(option.value)}
          onClick={() => onToggle(option.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              onToggle(option.value);
            }
          }}
          role="checkbox"
          tabIndex={0}
        >
          <CheckItem checked={list.includes(option.value)}>
            {option.label}
          </CheckItem>
        </span>
      ))}
    </div>
  );
}

function PolicyDrawer({
  open,
  onOpenChange,
  onSubmit,
  loading,
  policy,
  roles,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (
    input: Omit<ApprovalPolicy, "id" | "enterpriseId" | "createdAt">,
  ) => Promise<unknown>;
  loading: boolean;
  policy: ApprovalPolicy | null;
  roles: Array<{ id: string; label: string }>;
}) {
  const { t } = useTranslation();
  const schema = useMemo(
    () =>
      z.object({
        name: z
          .string()
          .trim()
          .min(1, t("settings.common.required"))
          .max(policyConstraints.name.maxLength ?? 128),
        riskLevels: z
          .array(z.enum(["write", "dangerous", "critical"]))
          .min(
            policyConstraints.risks.minItems ?? 1,
            t("settings.common.required"),
          ),
        minApprovers: z
          .number()
          .int()
          .min(policyConstraints.minimumApprovers.minimum ?? 1)
          .max(policyConstraints.minimumApprovers.maximum ?? 10),
        approverRoleIds: z
          .array(z.string())
          .min(
            policyConstraints.approverRoleIds.minItems ?? 1,
            t("settings.common.required"),
          )
          .max(policyConstraints.approverRoleIds.maxItems ?? 32),
        separationOfDuty: z.boolean(),
        enabled: z.boolean(),
      }),
    [t],
  );
  type PolicyForm = z.infer<typeof schema>;
  const {
    control,
    clearErrors,
    register,
    reset,
    setValue,
    watch,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<PolicyForm>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: "",
      riskLevels: [],
      minApprovers: 1,
      approverRoleIds: [],
      separationOfDuty: false,
      enabled: true,
    },
  });
  useEffect(() => {
    if (!open) return;
    reset({
      name: policy?.name ?? "",
      riskLevels: policy?.matchRiskLevels ?? [],
      minApprovers: policy?.minApprovers ?? 1,
      approverRoleIds: policy?.approverRoleIds ?? [],
      separationOfDuty: policy?.separationOfDuty ?? false,
      enabled: policy?.enabled ?? true,
    });
  }, [open, policy, reset]);
  const riskLevels = watch("riskLevels");
  const approverRoleIds = watch("approverRoleIds");
  const submit = handleSubmit(async (values) => {
    clearErrors();
    try {
      await onSubmit({
        name: values.name,
        matchRiskLevels: values.riskLevels,
        minApprovers: values.minApprovers,
        approverRoleIds: values.approverRoleIds,
        separationOfDuty: values.separationOfDuty,
        enabled: values.enabled,
      });
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("settings.common.saveFailed"),
        fieldMap: {
          approver_role_ids: "approverRoleIds",
          minimum_approvers: "minApprovers",
          name: "name",
          risks: "riskLevels",
        },
        requestReference: (requestId) =>
          t("common.requestReference", { requestId }),
        setFieldError: (field, message) =>
          setError(field, { message, type: "server" }, { shouldFocus: true }),
        setFormError: (message) =>
          setError("root", { message, type: "server" }),
      });
    }
  });

  return (
    <FormDrawer
      loading={loading}
      onOpenChange={onOpenChange}
      onSubmit={submit}
      open={open}
      title={
        policy
          ? t("settings.org.policiesTab.editTitle")
          : t("settings.org.policiesTab.createTitle")
      }
    >
      <div className="argus-settings-form">
        {errors.root?.message && (
          <Alert
            description={errors.root.message}
            title={t("settings.common.saveFailed")}
            tone="danger"
          />
        )}
        <Field
          requirement="required"
          error={errors.name?.message}
          label={t("settings.common.name")}
        >
          <Input
            {...register("name")}
            maxLength={policyConstraints.name.maxLength}
            required
          />
        </Field>
        <Field
          controlMode="group"
          requirement="required"
          error={errors.riskLevels?.message}
          label={t("settings.org.policiesTab.matchRiskLevels")}
        >
          <CheckRow
            list={riskLevels}
            onToggle={(value) =>
              setValue("riskLevels", toggleInList(riskLevels, value), {
                shouldValidate: true,
              })
            }
            options={RISK_LEVELS.map((level) => ({
              value: level,
              label: t(`settings.org.policiesTab.riskLevels.${level}`),
            }))}
          />
        </Field>
        <Field
          requirement="required"
          error={errors.minApprovers?.message}
          label={t("settings.org.policiesTab.minApprovers")}
        >
          <Input
            {...register("minApprovers", { valueAsNumber: true })}
            max={policyConstraints.minimumApprovers.maximum}
            min={policyConstraints.minimumApprovers.minimum}
            type="number"
          />
        </Field>
        <Field
          controlMode="group"
          requirement="required"
          error={errors.approverRoleIds?.message}
          label={t("settings.org.policiesTab.approverRoles")}
        >
          <CheckRow
            list={approverRoleIds}
            onToggle={(value) =>
              setValue(
                "approverRoleIds",
                toggleInList(approverRoleIds, value),
                { shouldValidate: true },
              )
            }
            options={roles.map((role) => ({
              value: role.id,
              label: role.label,
            }))}
          />
        </Field>
        <Field
          requirement="required"
          label={t("settings.org.policiesTab.separationOfDuty")}
        >
          <Controller
            control={control}
            name="separationOfDuty"
            render={({ field }) => (
              <Switch
                checked={field.value}
                label={t("settings.org.policiesTab.separationOfDuty")}
                onChange={field.onChange}
              />
            )}
          />
        </Field>
        <Field requirement="optional" label={t("settings.common.enabled")}>
          <Controller
            control={control}
            name="enabled"
            render={({ field }) => (
              <Switch
                checked={field.value}
                label={t("settings.common.enabled")}
                onChange={field.onChange}
              />
            )}
          />
        </Field>
      </div>
    </FormDrawer>
  );
}
