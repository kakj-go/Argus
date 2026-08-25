import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useMemo, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import {
  presentApiFormError,
  useApi,
  type CreatedApiKey,
  type CreateServiceAccountInput,
  type DataScope,
  type ServiceAccount,
} from "@argus/api-client";
import {
  Alert,
  Badge,
  Button,
  CodeBlock,
  ConfirmDialog,
  DataTable,
  DateTimePicker,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  Spinner,
  StatusBadge,
  Textarea,
} from "@argus/ui";
import { formatDateTime } from "./shared";
import { CheckList } from "./org-users-tab";

type ServiceAccountRow = {
  id: string;
  name: string;
  description: string;
  allowed_tool_ids: string[];
  data_scope_ids: string[];
  status: ServiceAccount["status"];
  updated_at: string;
};

function parseList(text: string): string[] {
  return [
    ...new Set(
      text
        .split(/[\n,]+/)
        .map((item) => item.trim())
        .filter(Boolean),
    ),
  ];
}

function toDateTimeLocal(iso?: string): string {
  return iso ? iso.slice(0, 16) : "";
}

export function OrgAccessTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const serviceAccounts = useQuery({
    queryKey: ["org", "serviceAccounts"],
    queryFn: () => api.org.listServiceAccounts(),
  });
  const dataScopes = useQuery({
    queryKey: ["org", "dataScopes"],
    queryFn: () => api.org.listDataScopes(),
  });
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editing, setEditing] = useState<ServiceAccount | null>(null);
  const [keysFor, setKeysFor] = useState<ServiceAccount | null>(null);

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["org", "serviceAccounts"] });

  const save = useMutation({
    mutationFn: ({
      account,
      input,
    }: {
      account: ServiceAccount | null;
      input: CreateServiceAccountInput;
    }) =>
      account
        ? api.org.updateServiceAccount(account.id, {
            description: input.description,
            allowed_tool_ids: input.allowed_tool_ids,
            data_scope_ids: input.data_scope_ids,
          })
        : api.org.createServiceAccount(input),
    onSuccess: () => {
      setDrawerOpen(false);
      setEditing(null);
      void invalidate();
    },
  });

  const toggleStatus = useMutation({
    mutationFn: (account: ServiceAccount) =>
      api.org.updateServiceAccount(account.id, {
        status: account.status === "active" ? "disabled" : "active",
      }),
    onSuccess: () => void invalidate(),
  });

  const scopeName = (id: string) =>
    dataScopes.data?.find((scope) => scope.id === id)?.name ?? id;

  const rows: ServiceAccountRow[] = (serviceAccounts.data ?? []).map(
    (account) => ({
      id: account.id,
      name: account.name,
      description: account.description ?? "",
      allowed_tool_ids: account.allowed_tool_ids ?? [],
      data_scope_ids: account.data_scope_ids ?? [],
      status: account.status,
      updated_at: account.updated_at,
    }),
  );

  return (
    <div className="argus-settings-section">
      <div className="argus-settings-section__head">
        <div>
          <h2 className="argus-settings-section__title">
            {t("settings.org.tabs.access")}
          </h2>
          <p className="argus-settings-section__hint">
            {t("settings.org.accessTab.saDescription")}
          </p>
        </div>
        <Button
          onClick={() => {
            setEditing(null);
            setDrawerOpen(true);
          }}
          size="sm"
          variant="primary"
        >
          {t("settings.org.accessTab.createSa")}
        </Button>
      </div>
      {serviceAccounts.isPending ? (
        <Spinner />
      ) : rows.length === 0 ? (
        <EmptyState description="" title={t("settings.org.accessTab.empty")} />
      ) : (
        <DataTable<ServiceAccountRow>
          columns={[
            { key: "name", header: t("settings.common.name") },
            { key: "description", header: t("settings.common.description") },
            {
              key: "allowed_tool_ids",
              header: t("settings.org.accessTab.allowedTools"),
              render: (row) =>
                row.allowed_tool_ids.length === 0 ? (
                  "—"
                ) : (
                  <span className="argus-settings-inline-actions">
                    {row.allowed_tool_ids.map((id) => (
                      <Badge key={id}>{id}</Badge>
                    ))}
                  </span>
                ),
            },
            {
              key: "data_scope_ids",
              header: t("settings.org.accessTab.dataScopes"),
              render: (row) =>
                row.data_scope_ids.length === 0 ? (
                  t("settings.common.all")
                ) : (
                  <span className="argus-settings-inline-actions">
                    {row.data_scope_ids.map((id) => (
                      <Badge key={id}>{scopeName(id)}</Badge>
                    ))}
                  </span>
                ),
            },
            {
              key: "status",
              header: t("settings.common.status"),
              render: (row) => (
                <StatusBadge
                  tone={row.status === "active" ? "success" : "neutral"}
                >
                  {row.status === "active"
                    ? t("settings.common.active")
                    : t("settings.common.disabled")}
                </StatusBadge>
              ),
            },
            {
              key: "updated_at",
              header: t("settings.org.accessTab.updatedAt"),
              render: (row) => formatDateTime(row.updated_at),
            },
            {
              key: "actions",
              header: t("settings.common.actions"),
              render: (row) => (
                <span className="argus-settings-inline-actions">
                  <Button
                    onClick={() => {
                      setEditing(
                        serviceAccounts.data?.find(
                          (account) => account.id === row.id,
                        ) ?? null,
                      );
                      setDrawerOpen(true);
                    }}
                    size="sm"
                    variant="ghost"
                  >
                    {t("settings.common.edit")}
                  </Button>
                  <Button
                    onClick={() =>
                      setKeysFor(
                        serviceAccounts.data?.find(
                          (account) => account.id === row.id,
                        ) ?? null,
                      )
                    }
                    size="sm"
                    variant="ghost"
                  >
                    {t("settings.org.accessTab.apiKeys")}
                  </Button>
                  <Button
                    onClick={() => {
                      const account = serviceAccounts.data?.find(
                        (entry) => entry.id === row.id,
                      );
                      if (account) toggleStatus.mutate(account);
                    }}
                    size="sm"
                    variant="ghost"
                  >
                    {row.status === "active"
                      ? t("settings.org.accessTab.disableSa")
                      : t("settings.org.accessTab.enableSa")}
                  </Button>
                </span>
              ),
            },
          ]}
          data={rows}
          getRowKey={(row) => row.id}
        />
      )}

      <ServiceAccountDrawer
        account={editing}
        dataScopes={dataScopes.data ?? []}
        loading={save.isPending}
        onOpenChange={(open) => {
          setDrawerOpen(open);
          if (!open) setEditing(null);
        }}
        onSubmit={(input) => save.mutateAsync({ account: editing, input })}
        open={drawerOpen}
      />

      {keysFor && (
        <ApiKeysDrawer
          account={keysFor}
          onOpenChange={(open) => {
            if (!open) setKeysFor(null);
          }}
        />
      )}
    </div>
  );
}

function ServiceAccountDrawer({
  open,
  onOpenChange,
  onSubmit,
  loading,
  account,
  dataScopes,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (input: CreateServiceAccountInput) => Promise<unknown>;
  loading: boolean;
  account: ServiceAccount | null;
  dataScopes: DataScope[];
}) {
  const { t } = useTranslation();
  const serviceAccountSchema = useMemo(
    () =>
      z.object({
        name: z.string().trim().min(1, t("settings.common.required")),
        description: z.string().trim(),
        allowed_tools: z.string(),
        data_scope_ids: z.array(z.string()),
      }),
    [t],
  );
  type ServiceAccountForm = z.infer<typeof serviceAccountSchema>;
  const {
    clearErrors,
    control,
    register,
    reset,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<ServiceAccountForm>({
    resolver: zodResolver(serviceAccountSchema),
    defaultValues: {
      name: "",
      description: "",
      allowed_tools: "",
      data_scope_ids: [],
    },
  });

  useEffect(() => {
    if (!open) return;
    reset({
      name: account?.name ?? "",
      description: account?.description ?? "",
      allowed_tools: (account?.allowed_tool_ids ?? []).join("\n"),
      data_scope_ids: account?.data_scope_ids ?? [],
    });
  }, [account, open, reset]);
  const submit = handleSubmit(async (values) => {
    clearErrors();
    try {
      await onSubmit({
        name: values.name,
        description: values.description || undefined,
        allowed_tool_ids: parseList(values.allowed_tools),
        data_scope_ids: values.data_scope_ids,
      });
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("settings.common.saveFailed"),
        fieldMap: {
          allowed_tool_ids: "allowed_tools",
          data_scope_ids: "data_scope_ids",
          description: "description",
          name: "name",
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
          ? t("settings.org.accessTab.editSaTitle")
          : t("settings.org.accessTab.createSaTitle")
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
          <Input {...register("name")} disabled={Boolean(account)} required />
        </Field>
        <Field requirement="optional" label={t("settings.common.description")}>
          <Input {...register("description")} />
        </Field>
        <Field
          requirement="optional"
          hint={t("settings.org.accessTab.allowedToolsHint")}
          label={t("settings.org.accessTab.allowedTools")}
        >
          <Textarea {...register("allowed_tools")} rows={4} />
        </Field>
        <Field
          requirement="optional"
          label={t("settings.org.accessTab.dataScopes")}
        >
          <Controller
            control={control}
            name="data_scope_ids"
            render={({ field }) => (
              <CheckList
                onChange={field.onChange}
                options={dataScopes.map((scope) => ({
                  id: scope.id,
                  label: scope.name,
                }))}
                value={field.value}
              />
            )}
          />
        </Field>
      </div>
    </FormDrawer>
  );
}

function ApiKeysDrawer({
  account,
  onOpenChange,
}: {
  account: ServiceAccount;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const keys = useQuery({
    queryKey: ["org", "apiKeys", account.id],
    queryFn: () => api.org.listApiKeys(account.id),
  });
  const [created, setCreated] = useState<CreatedApiKey | null>(null);
  const [rotating, setRotating] = useState<string | null>(null);
  const [revoking, setRevoking] = useState<string | null>(null);
  const keySchema = useMemo(
    () =>
      z.object({
        name: z.string().trim().min(1, t("settings.common.required")),
        expires_at: z.string(),
      }),
    [t],
  );
  type KeyForm = z.infer<typeof keySchema>;
  const {
    clearErrors,
    control,
    register,
    reset,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<KeyForm>({
    resolver: zodResolver(keySchema),
    defaultValues: { name: "", expires_at: "" },
  });

  const invalidate = () =>
    queryClient.invalidateQueries({
      queryKey: ["org", "apiKeys", account.id],
    });

  const create = useMutation({
    mutationFn: (input: KeyForm) =>
      api.org.createApiKey(account.id, {
        name: input.name,
        expires_at: input.expires_at
          ? new Date(input.expires_at).toISOString()
          : undefined,
      }),
    onSuccess: (result) => {
      setCreated(result);
      reset();
      void invalidate();
    },
  });

  const revoke = useMutation({
    mutationFn: (id: string) => api.org.revokeApiKey(id),
    onSuccess: () => {
      setRevoking(null);
      void invalidate();
    },
  });
  const rotate = useMutation({
    mutationFn: (id: string) => api.org.rotateApiKey(id),
    onSuccess: (result) => {
      setRotating(null);
      setCreated(result);
      void invalidate();
    },
  });

  const activeKeys = (keys.data ?? []).filter((key) => key.status === "active");
  const submitKey = handleSubmit(async (values) => {
    clearErrors();
    try {
      await create.mutateAsync(values);
    } catch (error) {
      presentApiFormError(error, {
        fallback: t("settings.common.saveFailed"),
        fieldMap: { expires_at: "expires_at", name: "name" },
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
      footer={<></>}
      onOpenChange={onOpenChange}
      onSubmit={submitKey}
      open
      title={`${t("settings.org.accessTab.apiKeysTitle")} · ${account.name}`}
    >
      <div className="argus-settings-form">
        {errors.root?.message && (
          <Alert
            description={errors.root.message}
            title={t("settings.common.saveFailed")}
            tone="danger"
          />
        )}
        {created && (
          <div className="argus-settings-section">
            <Alert
              description={t("settings.org.accessTab.keyCreatedWarning")}
              title={`${t("settings.org.accessTab.keyCreatedTitle")} · ${created.api_key.prefix}`}
              tone="warning"
            />
            <CodeBlock code={created.secret} language="apikey" />
          </div>
        )}
        {keys.isPending ? (
          <Spinner />
        ) : activeKeys.length === 0 ? (
          <p className="argus-settings-section__hint">
            {t("settings.org.accessTab.noKeys")}
          </p>
        ) : (
          <div className="argus-settings-key-list">
            {activeKeys.map((key) => (
              <div className="argus-settings-key-row" key={key.id}>
                <div className="argus-settings-key-row__meta">
                  <span>
                    {key.name} <Badge tone="accent">{key.prefix}…</Badge>
                  </span>
                  <small>
                    {t("settings.org.accessTab.lastUsedAt")}:{" "}
                    {key.last_used_at
                      ? formatDateTime(key.last_used_at)
                      : t("settings.common.never")}
                    {key.expires_at
                      ? ` · ${t("settings.org.accessTab.expiresAt")}: ${formatDateTime(key.expires_at)}`
                      : ""}
                  </small>
                </div>
                <Button
                  onClick={() => setRotating(key.id)}
                  size="sm"
                  variant="ghost"
                >
                  {t("settings.org.accessTab.rotate")}
                </Button>
                <Button
                  onClick={() => setRevoking(key.id)}
                  size="sm"
                  variant="ghost"
                >
                  {t("settings.org.accessTab.revoke")}
                </Button>
              </div>
            ))}
          </div>
        )}
        <Field
          requirement="required"
          error={errors.name?.message}
          label={t("settings.org.accessTab.keyName")}
        >
          <Input {...register("name")} />
        </Field>
        <Field
          requirement="optional"
          label={t("settings.org.accessTab.expiresAt")}
        >
          <Controller
            control={control}
            name="expires_at"
            render={({ field }) => (
              <DateTimePicker
                aria-label={t("settings.org.accessTab.expiresAt")}
                min={toDateTimeLocal(new Date().toISOString())}
                onBlur={field.onBlur}
                onChange={field.onChange}
                ref={field.ref}
                type="datetime-local"
                value={field.value}
              />
            )}
          />
        </Field>
        <Button loading={create.isPending} type="submit" variant="primary">
          {t("settings.org.accessTab.createKey")}
        </Button>
      </div>

      <ConfirmDialog
        description={t("settings.org.accessTab.rotateDescription")}
        loading={rotate.isPending}
        onConfirm={() => rotating && rotate.mutate(rotating)}
        onOpenChange={(open) => {
          if (!open) setRotating(null);
        }}
        open={rotating !== null}
        title={t("settings.org.accessTab.rotateTitle")}
      />
      <ConfirmDialog
        danger
        description={t("settings.org.accessTab.revokeDescription")}
        loading={revoke.isPending}
        onConfirm={() => revoking && revoke.mutate(revoking)}
        onOpenChange={(open) => {
          if (!open) setRevoking(null);
        }}
        open={revoking !== null}
        title={t("settings.org.accessTab.revokeTitle")}
      />
    </FormDrawer>
  );
}
