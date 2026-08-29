import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import type {
  RemoteAccessGrant,
  RemoteAccessGrantUpdate,
  RemoteAccessGrantWrite,
} from "@argus/api-client";
import { formConstraint, presentApiFormError, useApi } from "@argus/api-client";
import {
  Alert,
  Button,
  CheckItem,
  DateTimePicker,
  EmptyState,
  Field,
  FormDrawer,
  Select,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@argus/ui";
import { SessionProfilesTab } from "./remote-access/session-profiles-tab";
import { RulesTab } from "./remote-access/rules-tab";
import { WorkflowsTab } from "./remote-access/workflows-tab";
import { GovernanceList } from "./remote-access/governance-list";

type Option = { value: string; label: string };
type GrantForm = {
  subject_type: "user" | "department";
  subject_id: string;
  host_ids: string[];
  account_ids: string[];
  protocol: "ssh" | "winrs";
  valid_until: string;
};

const grantConstraints = {
  accounts: formConstraint("RemoteAccessGrantWrite", "managed_account_ids"),
  hosts: formConstraint("RemoteAccessGrantWrite", "host_ids"),
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

export function GrantsTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const grants = useQuery({
    queryKey: ["remote-access", "grants"],
    queryFn: () => api.remoteAccess.listGrants(),
  });
  const users = useQuery({
    queryKey: ["org", "users"],
    queryFn: () => api.org.listUsers(),
  });
  const departments = useQuery({
    queryKey: ["org", "departments"],
    queryFn: () => api.org.listDepartments(),
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
  const [selectedGrant, setSelectedGrant] = useState<RemoteAccessGrant | null>(null);
  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["remote-access"] });
  const createGrant = useMutation({
    mutationFn: (input: RemoteAccessGrantWrite | RemoteAccessGrantUpdate) =>
      selectedGrant
        ? api.remoteAccess.updateGrant(selectedGrant.id, input as RemoteAccessGrantUpdate)
        : api.remoteAccess.createGrant(input as RemoteAccessGrantWrite),
    onSuccess: () => {
      setGrantOpen(false);
      setSelectedGrant(null);
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
  const items = (grants.data?.items ?? []).map((row) => ({
    ...row,
    name: lookup(
      row.subject_type === "user" ? userOptions : departmentOptions,
      row.subject_id,
    ),
  }));
  const lifecycle = async (operation: (id: string) => Promise<unknown>, id: string) => {
    await operation(id);
    await invalidate();
  };

  return (
    <div className="argus-settings-section">
      <div className="argus-settings-section__head">
        <h2 className="argus-settings-section__title">
          {t("remoteAccess.grants")}
        </h2>
        <Button onClick={() => { setSelectedGrant(null); setGrantOpen(true); }} size="sm" variant="primary">
          {t("remoteAccess.newGrant")}
        </Button>
      </div>
      {(grants.data?.items.length ?? 0) === 0 ? (
        <EmptyState description="" title={t("remoteAccess.noGrants")} />
      ) : (
        <GovernanceList
          extraColumns={[
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
          ]}
          items={items}
          onArchive={(id) => lifecycle(api.remoteAccess.archiveGrant, id)}
          onDisable={(id) => lifecycle(api.remoteAccess.disableGrant, id)}
          onEdit={(row) => { setSelectedGrant(row); setGrantOpen(true); }}
          onEnable={(id) => lifecycle(api.remoteAccess.enableGrant, id)}
          onRestore={(id) => lifecycle(api.remoteAccess.restoreGrant, id)}
          references={api.remoteAccess.getGrantReferences}
        />
      )}

      <GrantDrawer
        accountOptions={accountOptions}
        departmentOptions={departmentOptions}
        hostOptions={hostOptions}
        loading={createGrant.isPending}
        onOpenChange={(open) => { setGrantOpen(open); if (!open) setSelectedGrant(null); }}
        onSubmit={(value) => createGrant.mutateAsync(value)}
        open={grantOpen}
        grant={selectedGrant}
        userOptions={userOptions}
      />
    </div>
  );
}

export function OrgRemoteAccessTab() {
  const { t } = useTranslation();
  const [tab, setTab] = useState("grants");
  return <Tabs onValueChange={setTab} value={tab}>
    <TabsList>
      <TabsTrigger value="grants">{t("remoteAccess.tabs.grants")}</TabsTrigger>
      <TabsTrigger value="rules">{t("remoteAccess.tabs.rules")}</TabsTrigger>
      <TabsTrigger value="workflows">{t("remoteAccess.tabs.workflows")}</TabsTrigger>
      <TabsTrigger value="profiles">{t("remoteAccess.tabs.profiles")}</TabsTrigger>
    </TabsList>
    <TabsContent value="grants"><GrantsTab /></TabsContent>
    <TabsContent value="rules"><RulesTab /></TabsContent>
    <TabsContent value="workflows"><WorkflowsTab /></TabsContent>
    <TabsContent value="profiles"><SessionProfilesTab /></TabsContent>
  </Tabs>;
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
  grant,
}: {
  open: boolean;
  onOpenChange(open: boolean): void;
  onSubmit(input: RemoteAccessGrantWrite | RemoteAccessGrantUpdate): Promise<unknown>;
  loading: boolean;
  userOptions: Option[];
  departmentOptions: Option[];
  hostOptions: Option[];
  accountOptions: Option[];
  grant: RemoteAccessGrant | null;
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
        subject_type: grant?.subject_type ?? "user",
        subject_id: grant?.subject_id ?? "",
        host_ids: grant?.host_ids ?? [],
        account_ids: grant?.managed_account_ids ?? [],
        protocol: grant?.protocols[0] ?? "ssh",
        valid_until: grant ? new Date(grant.valid_until).toISOString().slice(0, 16) : "",
      });
  }, [grant, open, reset]);
  const submit = handleSubmit(async (formValue) => {
    clearErrors();
    try {
      const input = {
        subject_type: formValue.subject_type,
        subject_id: formValue.subject_id,
        host_ids: formValue.host_ids,
        managed_account_ids: formValue.account_ids,
        protocols: [formValue.protocol],
        actions: ["terminal" as const],
        valid_from: grant?.valid_from ?? new Date().toISOString(),
        valid_until: new Date(formValue.valid_until).toISOString(),
      };
      await onSubmit(
        grant
          ? { ...input, expected_version: grant.version }
          : { ...input, status: "draft" } satisfies RemoteAccessGrantWrite,
      );
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
      submitLabel={grant ? t("remoteAccess.save") : t("remoteAccess.create")}
      title={grant ? t("remoteAccess.edit") : t("remoteAccess.newGrant")}
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
            <DateTimePicker
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
