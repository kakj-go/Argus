import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import { presentApiFormError, useApi } from "@argus/api-client";
import type { Credential, ManagedAccount } from "@argus/api-client/contracts";
import {
  Alert,
  Badge,
  Button,
  DataTable,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  Select,
  Spinner,
} from "@argus/ui";

type ManagedAccountForm = {
  host_id: string;
  username: string;
  privilege_level: ManagedAccount["privilege_level"];
  credential_id: string;
  status: ManagedAccount["status"];
};

export function ManagedAccountsSection({
  createOpen,
  onCreateOpenChange,
}: {
  createOpen: boolean;
  onCreateOpenChange: (open: boolean) => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<ManagedAccount | null>(null);
  const accounts = useQuery({
    queryKey: ["managed-accounts"],
    queryFn: () => api.secrets.listManagedAccounts(),
  });
  const hosts = useQuery({
    queryKey: ["hosts"],
    queryFn: () => api.hosts.list(),
  });
  const credentials = useQuery({
    queryKey: ["credentials"],
    queryFn: () => api.secrets.listCredentials(),
  });
  const save = useMutation({
    mutationFn: (values: ManagedAccountForm) => {
      const credential = credentials.data?.find(
        (item) => item.id === values.credential_id,
      );
      if (
        !credential ||
        (credential.protocol !== "ssh" && credential.protocol !== "winrm")
      ) {
        throw new Error("managed account credential must use ssh or winrm");
      }
      const allowed_protocols = [credential.protocol];
      return editing
        ? api.secrets.updateManagedAccount(editing.id, {
            username: values.username,
            privilege_level: values.privilege_level,
            credential_id: values.credential_id,
            allowed_protocols,
            status: values.status,
            expected_version: editing.version,
          })
        : api.secrets.createManagedAccount({
            host_id: values.host_id,
            username: values.username,
            privilege_level: values.privilege_level,
            credential_id: values.credential_id,
            allowed_protocols,
          });
    },
    onSuccess: () => {
      setEditing(null);
      onCreateOpenChange(false);
      void queryClient.invalidateQueries({ queryKey: ["managed-accounts"] });
    },
  });
  const hostNames = new Map(
    (hosts.data?.items ?? []).map((host) => [host.id, host.name]),
  );

  return (
    <section
      aria-label={t("settings.secrets.managedAccountsTitle")}
      className="argus-settings-section"
    >
      <p className="argus-settings-section__hint">
        {t("settings.secrets.managedAccountsDescription")}
      </p>
      {accounts.isPending ? (
        <Spinner />
      ) : (accounts.data ?? []).length === 0 ? (
        <EmptyState
          description=""
          title={t("settings.secrets.managedAccountsEmpty")}
        />
      ) : (
        <DataTable<ManagedAccount>
          columns={[
            {
              key: "host_id",
              header: t("settings.secrets.host"),
              render: (row) =>
                hostNames.get(row.host_id) ??
                t("settings.secrets.unavailableHost"),
            },
            { key: "username", header: t("settings.secrets.username") },
            {
              key: "privilege_level",
              header: t("settings.secrets.privilegeLevel"),
              render: (row) =>
                t(`settings.secrets.privileges.${row.privilege_level}`),
            },
            {
              key: "allowed_protocols",
              header: t("settings.secrets.protocol"),
              render: (row) =>
                row.allowed_protocols.map((value) => (
                  <Badge key={value} tone="info">
                    {value}
                  </Badge>
                )),
            },
            {
              key: "status",
              header: t("settings.common.status"),
              render: (row) => (
                <Badge tone={row.status === "active" ? "success" : "neutral"}>
                  {t(`settings.common.${row.status}`)}
                </Badge>
              ),
            },
            {
              key: "actions",
              header: t("settings.common.actions"),
              render: (row) => (
                <Button
                  onClick={() => {
                    onCreateOpenChange(false);
                    setEditing(row);
                  }}
                  size="sm"
                  variant="ghost"
                >
                  {t("settings.common.edit")}
                </Button>
              ),
            },
          ]}
          data={accounts.data ?? []}
          getRowKey={(row) => row.id}
        />
      )}
      <ManagedAccountDrawer
        account={editing}
        credentials={credentials.data ?? []}
        hosts={hosts.data?.items ?? []}
        loading={save.isPending}
        onOpenChange={(open) => {
          if (!open) {
            setEditing(null);
            onCreateOpenChange(false);
          }
        }}
        onSubmit={(values) => save.mutateAsync(values)}
        open={createOpen || editing !== null}
      />
    </section>
  );
}

function ManagedAccountDrawer({
  account,
  credentials,
  hosts,
  loading,
  onOpenChange,
  onSubmit,
  open,
}: {
  account: ManagedAccount | null;
  credentials: Credential[];
  hosts: Array<{ id: string; name: string }>;
  loading: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (values: ManagedAccountForm) => Promise<unknown>;
  open: boolean;
}) {
  const { t } = useTranslation();
  const schema = useMemo(
    () =>
      z.object({
        host_id: z.string().uuid(t("settings.common.required")),
        username: z.string().trim().min(1, t("settings.common.required")),
        privilege_level: z.enum(["standard", "sudo", "administrator"]),
        credential_id: z.string().uuid(t("settings.common.required")),
        status: z.enum(["active", "disabled"]),
      }),
    [t],
  );
  const {
    clearErrors,
    control,
    handleSubmit,
    register,
    reset,
    setError,
    formState: { errors },
  } = useForm<ManagedAccountForm>({
    resolver: zodResolver(schema),
    defaultValues: {
      host_id: "",
      username: "",
      privilege_level: "standard",
      credential_id: "",
      status: "active",
    },
  });
  useEffect(() => {
    if (!open) return;
    reset({
      host_id: account?.host_id ?? "",
      username: account?.username ?? "",
      privilege_level: account?.privilege_level ?? "standard",
      credential_id: account?.credential_id ?? "",
      status: account?.status ?? "active",
    });
  }, [account, open, reset]);
  const compatibleCredentials = credentials.filter(
    (credential) =>
      credential.status === "active" &&
      (credential.protocol === "ssh" || credential.protocol === "winrm"),
  );
  const submit = handleSubmit(async (values) => {
    clearErrors();
    try {
      await onSubmit(values);
    } catch (submitError) {
      presentApiFormError(submitError, {
        fallback: t("settings.common.saveFailed"),
        fieldMap: {
          credential_id: "credential_id",
          host_id: "host_id",
          privilege_level: "privilege_level",
          status: "status",
          username: "username",
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
        account
          ? t("settings.secrets.editManagedAccount")
          : t("settings.secrets.createManagedAccount")
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
          error={errors.host_id?.message}
          label={t("settings.secrets.host")}
        >
          <Controller
            control={control}
            name="host_id"
            render={({ field }) => (
              <Select
                disabled={account !== null}
                onValueChange={field.onChange}
                options={hosts.map((host) => ({
                  value: host.id,
                  label: host.name,
                }))}
                value={field.value}
              />
            )}
          />
        </Field>
        <Field
          requirement="required"
          error={errors.username?.message}
          label={t("settings.secrets.username")}
        >
          <Input {...register("username")} required />
        </Field>
        <Field
          requirement="required"
          label={t("settings.secrets.privilegeLevel")}
        >
          <Controller
            control={control}
            name="privilege_level"
            render={({ field }) => (
              <Select
                onValueChange={field.onChange}
                options={(["standard", "sudo", "administrator"] as const).map(
                  (value) => ({
                    value,
                    label: t(`settings.secrets.privileges.${value}`),
                  }),
                )}
                value={field.value}
              />
            )}
          />
        </Field>
        <Field
          requirement="required"
          error={errors.credential_id?.message}
          label={t("settings.secrets.credential")}
        >
          <Controller
            control={control}
            name="credential_id"
            render={({ field }) => (
              <Select
                onValueChange={field.onChange}
                options={compatibleCredentials.map((credential) => ({
                  value: credential.id,
                  label: `${credential.name} (${credential.protocol})`,
                }))}
                value={field.value}
              />
            )}
          />
        </Field>
        {account && (
          <Field requirement="required" label={t("settings.common.status")}>
            <Controller
              control={control}
              name="status"
              render={({ field }) => (
                <Select
                  onValueChange={field.onChange}
                  options={(["active", "disabled"] as const).map((value) => ({
                    value,
                    label: t(`settings.common.${value}`),
                  }))}
                  value={field.value}
                />
              )}
            />
          </Field>
        )}
      </div>
    </FormDrawer>
  );
}
