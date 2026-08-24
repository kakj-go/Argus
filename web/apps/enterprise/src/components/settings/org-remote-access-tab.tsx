import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import type {
  RemoteAccessGrantWrite,
  RemoteAccessPolicyWrite,
} from "@argus/api-client";
import { formConstraint, presentApiFormError, useApi } from "@argus/api-client";
import {
  Alert,
  Button,
  CheckItem,
  DataTable,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  Select,
  StatusBadge,
  Switch,
} from "@argus/ui";

type Option = { value: string; label: string };
type GrantForm = {
  subject_type: "user" | "department";
  subject_id: string;
  host_ids: string[];
  account_ids: string[];
  protocol: "ssh" | "winrs";
  valid_until: string;
};
type PolicyForm = {
  name: string;
  approver_role_ids: string[];
  minimum_approvals: number;
  require_mfa: boolean;
};

const grantConstraints = {
  accounts: formConstraint("RemoteAccessGrantWrite", "managed_account_ids"),
  hosts: formConstraint("RemoteAccessGrantWrite", "host_ids"),
};
const remotePolicyConstraints = {
  approverRoles: formConstraint("RemoteAccessPolicyWrite", "approver_role_ids"),
  minimumApprovals: formConstraint(
    "RemoteAccessPolicyWrite",
    "minimum_approvals",
  ),
  name: formConstraint("RemoteAccessPolicyWrite", "name"),
};

function OptionChecklist({
  options,
  value,
  onChange,
  emptyLabel,
}: {
  options: Option[];
  value: string[];
  onChange: (value: string[]) => void;
  emptyLabel: string;
}) {
  if (options.length === 0) {
    return <span className="argus-settings-section__hint">{emptyLabel}</span>;
  }
  return (
    <div className="argus-perm-matrix__cells">
      {options.map((option) => {
        const checked = value.includes(option.value);
        const toggle = () =>
          onChange(
            checked
              ? value.filter((item) => item !== option.value)
              : [...value, option.value],
          );
        return (
          <span
            aria-checked={checked}
            key={option.value}
            onClick={toggle}
            onKeyDown={(event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                toggle();
              }
            }}
            role="checkbox"
            tabIndex={0}
          >
            <CheckItem checked={checked}>{option.label}</CheckItem>
          </span>
        );
      })}
    </div>
  );
}

export function OrgRemoteAccessTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const grants = useQuery({
    queryKey: ["remote-access", "grants"],
    queryFn: () => api.remoteAccess.listGrants(),
  });
  const policies = useQuery({
    queryKey: ["remote-access", "policies"],
    queryFn: () => api.remoteAccess.listPolicies(),
  });
  const users = useQuery({
    queryKey: ["org", "users"],
    queryFn: () => api.org.listUsers(),
  });
  const departments = useQuery({
    queryKey: ["org", "departments"],
    queryFn: () => api.org.listDepartments(),
  });
  const roles = useQuery({
    queryKey: ["org", "roles"],
    queryFn: () => api.org.listRoles(),
  });
  const hosts = useQuery({
    queryKey: ["hosts", "remote-access-options"],
    queryFn: () => api.hosts.list(),
  });
  const accounts = useQuery({
    queryKey: ["managed-accounts", "remote-access-options"],
    queryFn: () => api.secrets.listManagedAccounts(),
  });
  const [grantOpen, setGrantOpen] = useState(false);
  const [policyOpen, setPolicyOpen] = useState(false);
  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["remote-access"] });
  const createGrant = useMutation({
    mutationFn: (input: RemoteAccessGrantWrite) =>
      api.remoteAccess.createGrant(input),
    onSuccess: () => {
      setGrantOpen(false);
      void invalidate();
    },
  });
  const createPolicy = useMutation({
    mutationFn: (input: RemoteAccessPolicyWrite) =>
      api.remoteAccess.createPolicy(input),
    onSuccess: () => {
      setPolicyOpen(false);
      void invalidate();
    },
  });

  const userOptions = (users.data ?? []).map((item) => ({
    value: item.id,
    label:
      item.displayName === item.username
        ? item.displayName
        : `${item.displayName} (${item.username})`,
  }));
  const departmentOptions = (departments.data ?? []).map((item) => ({
    value: item.id,
    label: item.name,
  }));
  const roleOptions = (roles.data ?? []).map((item) => ({
    value: item.id,
    label: item.name,
  }));
  const hostOptions = (hosts.data?.items ?? []).map((item) => ({
    value: item.id,
    label: item.name,
  }));
  const accountOptions = (accounts.data ?? []).map((item) => ({
    value: item.id,
    label: item.username,
  }));
  const lookup = (options: Option[], id: string) =>
    options.find((item) => item.value === id)?.label ??
    t("remoteAccess.unavailableReference");

  return (
    <div className="argus-settings-section">
      <div className="argus-settings-section__head">
        <h2 className="argus-settings-section__title">
          {t("remoteAccess.grants")}
        </h2>
        <Button onClick={() => setGrantOpen(true)} size="sm" variant="primary">
          {t("remoteAccess.newGrant")}
        </Button>
      </div>
      {(grants.data?.items.length ?? 0) === 0 ? (
        <EmptyState description="" title={t("remoteAccess.noGrants")} />
      ) : (
        <DataTable
          columns={[
            {
              key: "subject",
              header: t("remoteAccess.subject"),
              render: (row) =>
                lookup(
                  row.subject_type === "user" ? userOptions : departmentOptions,
                  row.subject_id,
                ),
            },
            {
              key: "hosts",
              header: t("remoteAccess.hosts"),
              render: (row) =>
                row.host_ids.map((id) => lookup(hostOptions, id)).join(", ") ||
                t("remoteAccess.labelSelector"),
            },
            {
              key: "accounts",
              header: t("remoteAccess.accounts"),
              render: (row) =>
                row.managed_account_ids
                  .map((id) => lookup(accountOptions, id))
                  .join(", "),
            },
            {
              key: "protocols",
              header: t("remoteAccess.protocols"),
              render: (row) => row.protocols.join(", "),
            },
            {
              key: "valid_until",
              header: t("remoteAccess.validUntil"),
              render: (row) => new Date(row.valid_until).toLocaleString(),
            },
            {
              key: "enabled",
              header: t("remoteAccess.status"),
              render: (row) => (
                <StatusBadge tone={row.enabled ? "success" : "neutral"}>
                  {t(
                    row.enabled
                      ? "remoteAccess.enabled"
                      : "remoteAccess.disabled",
                  )}
                </StatusBadge>
              ),
            },
            {
              key: "actions",
              header: t("remoteAccess.actions"),
              render: (row) =>
                row.enabled ? (
                  <Button
                    onClick={() =>
                      void api.remoteAccess
                        .disableGrant(row.id)
                        .then(invalidate)
                    }
                    size="sm"
                    variant="danger"
                  >
                    {t("remoteAccess.disable")}
                  </Button>
                ) : null,
            },
          ]}
          data={grants.data?.items ?? []}
          getRowKey={(row) => row.id}
        />
      )}

      <div className="argus-settings-section__head">
        <h2 className="argus-settings-section__title">
          {t("remoteAccess.policies")}
        </h2>
        <Button onClick={() => setPolicyOpen(true)} size="sm" variant="primary">
          {t("remoteAccess.newPolicy")}
        </Button>
      </div>
      {(policies.data?.items.length ?? 0) === 0 ? (
        <EmptyState description="" title={t("remoteAccess.noPolicies")} />
      ) : (
        <DataTable
          columns={[
            { key: "name", header: t("remoteAccess.name") },
            {
              key: "approver_role_ids",
              header: t("remoteAccess.approverRoles"),
              render: (row) =>
                (row.approver_role_ids ?? [])
                  .map((id) => lookup(roleOptions, id))
                  .join(", "),
            },
            { key: "minimum_approvals", header: t("remoteAccess.approvals") },
            {
              key: "require_mfa",
              header: t("remoteAccess.mfa"),
              render: (row) =>
                row.require_mfa
                  ? t("remoteAccess.mfaRequiredM8")
                  : t("remoteAccess.no"),
            },
            {
              key: "duration",
              header: t("remoteAccess.limits"),
              render: (row) =>
                `${row.idle_timeout_seconds}s / ${row.max_session_seconds}s`,
            },
            {
              key: "enabled",
              header: t("remoteAccess.status"),
              render: (row) => (
                <StatusBadge tone={row.enabled ? "success" : "neutral"}>
                  {t(
                    row.enabled
                      ? "remoteAccess.enabled"
                      : "remoteAccess.disabled",
                  )}
                </StatusBadge>
              ),
            },
            {
              key: "actions",
              header: t("remoteAccess.actions"),
              render: (row) =>
                row.enabled ? (
                  <Button
                    onClick={() =>
                      void api.remoteAccess
                        .disablePolicy(row.id)
                        .then(invalidate)
                    }
                    size="sm"
                    variant="danger"
                  >
                    {t("remoteAccess.disable")}
                  </Button>
                ) : null,
            },
          ]}
          data={policies.data?.items ?? []}
          getRowKey={(row) => row.id}
        />
      )}
      <GrantDrawer
        accountOptions={accountOptions}
        departmentOptions={departmentOptions}
        hostOptions={hostOptions}
        loading={createGrant.isPending}
        onOpenChange={setGrantOpen}
        onSubmit={(value) => createGrant.mutateAsync(value)}
        open={grantOpen}
        userOptions={userOptions}
      />
      <PolicyDrawer
        loading={createPolicy.isPending}
        onOpenChange={setPolicyOpen}
        onSubmit={(value) => createPolicy.mutateAsync(value)}
        open={policyOpen}
        roleOptions={roleOptions}
      />
    </div>
  );
}

function GrantDrawer({
  open,
  onOpenChange,
  onSubmit,
  loading,
  userOptions,
  departmentOptions,
  hostOptions,
  accountOptions,
}: {
  open: boolean;
  onOpenChange(open: boolean): void;
  onSubmit(input: RemoteAccessGrantWrite): Promise<unknown>;
  loading: boolean;
  userOptions: Option[];
  departmentOptions: Option[];
  hostOptions: Option[];
  accountOptions: Option[];
}) {
  const { t } = useTranslation();
  const schema = useMemo(
    () =>
      z.object({
        subject_type: z.enum(["user", "department"]),
        subject_id: z.string().min(1, t("remoteAccess.required")),
        host_ids: z
          .array(z.string())
          .min(1, t("remoteAccess.selectAtLeastOne"))
          .max(grantConstraints.hosts.maxItems ?? 256),
        account_ids: z
          .array(z.string())
          .min(
            grantConstraints.accounts.minItems ?? 1,
            t("remoteAccess.selectAtLeastOne"),
          )
          .max(grantConstraints.accounts.maxItems ?? 64),
        protocol: z.enum(["ssh", "winrs"]),
        valid_until: z.string().min(1, t("remoteAccess.required")),
      }),
    [t],
  );
  const {
    clearErrors,
    control,
    handleSubmit,
    reset,
    setError,
    setValue,
    watch,
    formState: { errors },
  } = useForm<GrantForm>({
    resolver: zodResolver(schema),
    defaultValues: {
      subject_type: "user",
      subject_id: "",
      host_ids: [],
      account_ids: [],
      protocol: "ssh",
      valid_until: "",
    },
  });
  const subjectType = watch("subject_type");
  useEffect(() => {
    if (open)
      reset({
        subject_type: "user",
        subject_id: "",
        host_ids: [],
        account_ids: [],
        protocol: "ssh",
        valid_until: "",
      });
  }, [open, reset]);
  const submit = handleSubmit(async (value) => {
    clearErrors();
    try {
      await onSubmit({
        subject_type: value.subject_type,
        subject_id: value.subject_id,
        host_ids: value.host_ids,
        managed_account_ids: value.account_ids,
        protocols: [value.protocol],
        actions: ["terminal"],
        valid_from: new Date().toISOString(),
        valid_until: new Date(value.valid_until).toISOString(),
        enabled: true,
      });
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("settings.common.saveFailed"),
        fieldMap: {
          host_ids: "host_ids",
          managed_account_ids: "account_ids",
          protocols: "protocol",
          subject_id: "subject_id",
          subject_type: "subject_type",
          valid_until: "valid_until",
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
      description={t("remoteAccess.grantDescription")}
      loading={loading}
      onOpenChange={onOpenChange}
      onSubmit={submit}
      open={open}
      submitLabel={t("remoteAccess.create")}
      title={t("remoteAccess.newGrant")}
    >
      {errors.root?.message && (
        <Alert
          description={errors.root.message}
          title={t("settings.common.saveFailed")}
          tone="danger"
        />
      )}
      <Field requirement="required" label={t("remoteAccess.subjectType")}>
        <Controller
          control={control}
          name="subject_type"
          render={({ field }) => (
            <Select
              ariaLabel={t("remoteAccess.subjectType")}
              onValueChange={(value) => {
                field.onChange(value);
                setValue("subject_id", "");
              }}
              options={[
                { value: "user", label: t("remoteAccess.user") },
                { value: "department", label: t("remoteAccess.department") },
              ]}
              value={field.value}
            />
          )}
        />
      </Field>
      <Field
        requirement="required"
        error={errors.subject_id?.message}
        label={t("remoteAccess.subject")}
      >
        <Controller
          control={control}
          name="subject_id"
          render={({ field }) => (
            <Select
              ariaLabel={t("remoteAccess.subject")}
              onValueChange={field.onChange}
              options={subjectType === "user" ? userOptions : departmentOptions}
              value={field.value}
            />
          )}
        />
      </Field>
      <Field
        requirement="required"
        error={errors.host_ids?.message}
        label={t("remoteAccess.hosts")}
      >
        <Controller
          control={control}
          name="host_ids"
          render={({ field }) => (
            <OptionChecklist
              emptyLabel={t("remoteAccess.noSelectableHosts")}
              onChange={field.onChange}
              options={hostOptions}
              value={field.value}
            />
          )}
        />
      </Field>
      <Field
        requirement="required"
        error={errors.account_ids?.message}
        label={t("remoteAccess.accounts")}
      >
        <Controller
          control={control}
          name="account_ids"
          render={({ field }) => (
            <OptionChecklist
              emptyLabel={t("remoteAccess.noSelectableAccounts")}
              onChange={field.onChange}
              options={accountOptions}
              value={field.value}
            />
          )}
        />
      </Field>
      <Field requirement="required" label={t("remoteAccess.protocol")}>
        <Controller
          control={control}
          name="protocol"
          render={({ field }) => (
            <Select
              ariaLabel={t("remoteAccess.protocol")}
              onValueChange={field.onChange}
              options={[
                { value: "ssh", label: "SSH PTY" },
                { value: "winrs", label: "WinRS PowerShell" },
              ]}
              value={field.value}
            />
          )}
        />
      </Field>
      <Field
        requirement="required"
        error={errors.valid_until?.message}
        label={t("remoteAccess.validUntil")}
      >
        <Controller
          control={control}
          name="valid_until"
          render={({ field }) => (
            <Input
              onChange={field.onChange}
              type="datetime-local"
              value={field.value}
            />
          )}
        />
      </Field>
    </FormDrawer>
  );
}

function PolicyDrawer({
  open,
  onOpenChange,
  onSubmit,
  loading,
  roleOptions,
}: {
  open: boolean;
  onOpenChange(open: boolean): void;
  onSubmit(input: RemoteAccessPolicyWrite): Promise<unknown>;
  loading: boolean;
  roleOptions: Option[];
}) {
  const { t } = useTranslation();
  const schema = useMemo(
    () =>
      z.object({
        name: z
          .string()
          .trim()
          .min(
            remotePolicyConstraints.name.minLength ?? 1,
            t("remoteAccess.required"),
          )
          .max(remotePolicyConstraints.name.maxLength ?? 128),
        approver_role_ids: z
          .array(z.string())
          .min(1, t("remoteAccess.selectAtLeastOne"))
          .max(remotePolicyConstraints.approverRoles.maxItems ?? 64),
        minimum_approvals: z
          .number()
          .int()
          .min(remotePolicyConstraints.minimumApprovals.minimum ?? 1)
          .max(remotePolicyConstraints.minimumApprovals.maximum ?? 16),
        require_mfa: z.boolean(),
      }),
    [t],
  );
  const {
    clearErrors,
    control,
    register,
    handleSubmit,
    reset,
    setError,
    formState: { errors },
  } = useForm<PolicyForm>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: "",
      approver_role_ids: [],
      minimum_approvals: 1,
      require_mfa: false,
    },
  });
  useEffect(() => {
    if (open)
      reset({
        name: "",
        approver_role_ids: [],
        minimum_approvals: 1,
        require_mfa: false,
      });
  }, [open, reset]);
  const submit = handleSubmit(async (value) => {
    clearErrors();
    try {
      await onSubmit({
        name: value.name.trim(),
        enabled: true,
        priority: 100,
        protocols: ["ssh", "winrs"],
        approver_role_ids: value.approver_role_ids,
        minimum_approvals: value.minimum_approvals,
        separation_of_duties: true,
        require_mfa: value.require_mfa,
        max_session_seconds: 3600,
        idle_timeout_seconds: 900,
      });
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("settings.common.saveFailed"),
        fieldMap: {
          approver_role_ids: "approver_role_ids",
          minimum_approvals: "minimum_approvals",
          name: "name",
          require_mfa: "require_mfa",
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
      description={t("remoteAccess.policyDescription")}
      loading={loading}
      onOpenChange={onOpenChange}
      onSubmit={submit}
      open={open}
      submitLabel={t("remoteAccess.create")}
      title={t("remoteAccess.newPolicy")}
    >
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
        label={t("remoteAccess.name")}
      >
        <Input
          {...register("name")}
          maxLength={remotePolicyConstraints.name.maxLength}
        />
      </Field>
      <Field
        requirement="required"
        error={errors.approver_role_ids?.message}
        label={t("remoteAccess.approverRoles")}
      >
        <Controller
          control={control}
          name="approver_role_ids"
          render={({ field }) => (
            <OptionChecklist
              emptyLabel={t("remoteAccess.noSelectableRoles")}
              onChange={field.onChange}
              options={roleOptions}
              value={field.value}
            />
          )}
        />
      </Field>
      <Field
        requirement="required"
        error={errors.minimum_approvals?.message}
        label={t("remoteAccess.minimumApprovals")}
      >
        <Input
          {...register("minimum_approvals", { valueAsNumber: true })}
          max={remotePolicyConstraints.minimumApprovals.maximum}
          min={remotePolicyConstraints.minimumApprovals.minimum}
          type="number"
        />
      </Field>
      <Controller
        control={control}
        name="require_mfa"
        render={({ field }) => (
          <Switch
            checked={field.value}
            label={t("remoteAccess.requireMfa")}
            onChange={field.onChange}
          />
        )}
      />
    </FormDrawer>
  );
}
