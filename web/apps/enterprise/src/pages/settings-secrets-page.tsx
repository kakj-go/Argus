import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useMemo, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { ChevronDown, Plus } from "lucide-react";
import { z } from "zod";
import {
  apiErrorField,
  formConstraint,
  formatApiError,
  useApi,
} from "@argus/api-client";
import type { Secret, SecretType } from "@argus/api-client";
import type { Credential } from "@argus/api-client/contracts";
import {
  Alert,
  Badge,
  Button,
  ConfirmDialog,
  DataTable,
  Dropdown,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  PageShell,
  Select,
  Spinner,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Textarea,
} from "@argus/ui";
import "../styles/settings.css";
import { formatDateTime } from "../components/settings/shared";
import { ManagedAccountsSection } from "../components/settings/managed-accounts-section";

const SECRET_TYPES: SecretType[] = [
  "ssh_password",
  "ssh_private_key",
  "winrm_password",
  "kubeconfig",
  "api_token",
  "basic_auth",
];
const CREDENTIAL_PROTOCOLS: Credential["protocol"][] = [
  "ssh",
  "winrm",
  "kubernetes",
  "http",
];

const credentialConstraints = {
  name: formConstraint("CredentialCreate", "name"),
  username: formConstraint("CredentialCreate", "username"),
};
const secretConstraints = {
  name: formConstraint("SecretCreate", "name"),
  description: formConstraint("SecretCreate", "description"),
  value: formConstraint("SecretCreate", "value"),
};

type SecretRow = {
  id: string;
  name: string;
  type: SecretType;
  description: string;
  reference_count: number;
  createdBy: string;
  updated_at: string;
};

type CredentialTab = "secrets" | "credentials" | "managed_accounts";

/** Secret 管理：只展示元数据，值只写入不回显。 */
export function SettingsSecretsPage() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const secrets = useQuery({
    queryKey: ["secrets"],
    queryFn: () => api.secrets.list(),
  });
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editing, setEditing] = useState<Secret | null>(null);
  const [deleting, setDeleting] = useState<Secret | null>(null);
  const [credentialDrawerOpen, setCredentialDrawerOpen] = useState(false);
  const [managedAccountDrawerOpen, setManagedAccountDrawerOpen] =
    useState(false);
  const [editingCredential, setEditingCredential] = useState<Credential | null>(
    null,
  );
  const [tab, setTab] = useState<CredentialTab>("secrets");

  const credentials = useQuery({
    queryKey: ["credentials"],
    queryFn: () => api.secrets.listCredentials(),
  });
  const users = useQuery({
    queryKey: ["org", "users", "secret-creators"],
    queryFn: () => api.org.listUsers(),
  });
  const userLabel = (id: string) => {
    const user = users.data?.find((item) => item.id === id);
    return user
      ? user.displayName === user.username
        ? user.displayName
        : `${user.displayName} (${user.username})`
      : t("settings.secrets.unknownCreator");
  };
  const secretLabel = (id: string) =>
    secrets.data?.items.find((item) => item.id === id)?.name ??
    t("settings.secrets.unknownSecret");

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["secrets"] });

  const save = useMutation({
    mutationFn: async (input: {
      id?: string;
      name: string;
      type: SecretType;
      description?: string;
      value?: string;
    }) => {
      if (!input.id) {
        return api.secrets.create({
          name: input.name,
          type: input.type,
          description: input.description,
          value: input.value ?? "",
        });
      }
      const current = secrets.data?.items.find(
        (secret) => secret.id === input.id,
      );
      if (!current) throw new Error("secret metadata is unavailable");
      let updated = await api.secrets.update(input.id, {
        name: input.name,
        description: input.description,
        expected_version: current.version,
      });
      if (input.value) {
        updated = await api.secrets.rotate(
          input.id,
          input.value,
          updated.version,
        );
      }
      return updated;
    },
    onSuccess: () => {
      setDrawerOpen(false);
      setEditing(null);
      void invalidate();
    },
  });

  const remove = useMutation({
    mutationFn: (id: string) => api.secrets.delete(id),
    onSuccess: () => {
      setDeleting(null);
      void invalidate();
    },
  });
  const saveCredential = useMutation({
    mutationFn: (input: {
      id?: string;
      name: string;
      protocol: Credential["protocol"];
      username?: string;
      secret_id: string;
      expected_version?: number;
    }) =>
      input.id
        ? api.secrets.updateCredential(input.id, {
            name: input.name,
            username: input.username,
            secret_id: input.secret_id,
            expected_version: input.expected_version ?? 1,
          })
        : api.secrets.createCredential({
            name: input.name,
            protocol: input.protocol,
            username: input.username,
            secret_id: input.secret_id,
          }),
    onSuccess: () => {
      setCredentialDrawerOpen(false);
      setEditingCredential(null);
      void queryClient.invalidateQueries({ queryKey: ["credentials"] });
    },
  });

  const rows: SecretRow[] = (secrets.data?.items ?? []).map((secret) => ({
    id: secret.id,
    name: secret.name,
    type: secret.type,
    description: secret.description ?? "",
    reference_count: secret.reference_count,
    createdBy: userLabel(secret.created_by),
    updated_at: secret.updated_at,
  }));

  const openSecretDrawer = () => {
    setTab("secrets");
    setEditing(null);
    setDrawerOpen(true);
  };
  const openCredentialDrawer = () => {
    setTab("credentials");
    setEditingCredential(null);
    setCredentialDrawerOpen(true);
  };
  const openManagedAccountDrawer = () => {
    setTab("managed_accounts");
    setManagedAccountDrawerOpen(true);
  };
  const createActions: Record<
    CredentialTab,
    { label: string; onSelect: () => void }
  > = {
    secrets: {
      label: t("settings.secrets.create"),
      onSelect: openSecretDrawer,
    },
    credentials: {
      label: t("settings.secrets.createCredential"),
      onSelect: openCredentialDrawer,
    },
    managed_accounts: {
      label: t("settings.secrets.createManagedAccount"),
      onSelect: openManagedAccountDrawer,
    },
  };
  const currentCreateAction = createActions[tab];

  return (
    <PageShell
      description={t("settings.secrets.description")}
      title={t("settings.secrets.title")}
      actions={
        <div className="argus-settings-create-actions">
          <Button
            onClick={currentCreateAction.onSelect}
            size="sm"
            variant="primary"
          >
            <Plus aria-hidden size={15} />
            {currentCreateAction.label}
          </Button>
          <Dropdown
            items={Object.values(createActions).map((action) => ({
              label: action.label,
              onSelect: action.onSelect,
            }))}
            trigger={
              <Button
                aria-label={t("settings.secrets.createMenu")}
                size="icon"
                variant="secondary"
              >
                <ChevronDown aria-hidden size={16} />
              </Button>
            }
          />
        </div>
      }
    >
      <Tabs
        onValueChange={(value) => setTab(value as CredentialTab)}
        value={tab}
      >
        <TabsList className="argus-settings-resource-tabs">
          <TabsTrigger value="secrets">
            {t("settings.secrets.tabs.secrets")}
          </TabsTrigger>
          <TabsTrigger value="credentials">
            {t("settings.secrets.tabs.credentials")}
          </TabsTrigger>
          <TabsTrigger value="managed_accounts">
            {t("settings.secrets.tabs.managedAccounts")}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="secrets">
          <div className="argus-settings-stack">
            <Alert
              description={t("settings.secrets.alertDescription")}
              title={t("settings.secrets.alertTitle")}
              tone="info"
            />
            {secrets.isPending ? (
              <Spinner />
            ) : rows.length === 0 ? (
              <EmptyState description="" title={t("settings.secrets.empty")} />
            ) : (
              <DataTable<SecretRow>
                columns={[
                  { key: "name", header: t("settings.secrets.name") },
                  {
                    key: "type",
                    header: t("settings.secrets.type"),
                    render: (row) => (
                      <Badge tone="info">
                        {t(`settings.secrets.types.${row.type}`)}
                      </Badge>
                    ),
                  },
                  {
                    key: "reference_count",
                    header: t("settings.secrets.referenceCount"),
                    render: (row) =>
                      row.reference_count > 0 ? (
                        <Badge tone="warning">{row.reference_count}</Badge>
                      ) : (
                        "0"
                      ),
                  },
                  {
                    key: "createdBy",
                    header: t("settings.secrets.createdBy"),
                  },
                  {
                    key: "updated_at",
                    header: t("settings.common.updatedAt"),
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
                              secrets.data?.items.find(
                                (secret) => secret.id === row.id,
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
                            setDeleting(
                              secrets.data?.items.find(
                                (secret) => secret.id === row.id,
                              ) ?? null,
                            )
                          }
                          size="sm"
                          variant="ghost"
                        >
                          {t("settings.common.delete")}
                        </Button>
                      </span>
                    ),
                  },
                ]}
                data={rows}
                getRowKey={(row) => row.id}
              />
            )}
          </div>
        </TabsContent>

        <TabsContent value="credentials">
          <section
            aria-label={t("settings.secrets.credentialsTitle")}
            className="argus-settings-section"
          >
            <p className="argus-settings-section__hint">
              {t("settings.secrets.credentialsDescription")}
            </p>
            {credentials.isPending ? (
              <Spinner />
            ) : (credentials.data ?? []).length === 0 ? (
              <EmptyState
                description=""
                title={t("settings.secrets.credentialsEmpty")}
              />
            ) : (
              <DataTable<Credential>
                columns={[
                  { key: "name", header: t("settings.secrets.name") },
                  {
                    key: "protocol",
                    header: t("settings.secrets.protocol"),
                    render: (row) => <Badge tone="info">{row.protocol}</Badge>,
                  },
                  {
                    key: "username",
                    header: t("settings.secrets.username"),
                    render: (row) => row.username ?? "-",
                  },
                  {
                    key: "secret_id",
                    header: t("settings.secrets.secretRef"),
                    render: (row) => secretLabel(row.secret_id),
                  },
                  {
                    key: "actions",
                    header: t("settings.common.actions"),
                    render: (row) => (
                      <Button
                        onClick={() => {
                          setEditingCredential(row);
                          setCredentialDrawerOpen(true);
                        }}
                        size="sm"
                        variant="ghost"
                      >
                        {t("settings.common.edit")}
                      </Button>
                    ),
                  },
                ]}
                data={credentials.data ?? []}
                getRowKey={(row) => row.id}
              />
            )}
          </section>
        </TabsContent>

        <TabsContent value="managed_accounts">
          <ManagedAccountsSection
            createOpen={managedAccountDrawerOpen}
            onCreateOpenChange={setManagedAccountDrawerOpen}
          />
        </TabsContent>
      </Tabs>

      <SecretDrawer
        loading={save.isPending}
        onOpenChange={(open) => {
          setDrawerOpen(open);
          if (!open) setEditing(null);
        }}
        onSubmit={(input) =>
          save.mutateAsync({ ...input, id: editing?.id })
        }
        open={drawerOpen}
        secret={editing}
      />

      <ConfirmDialog
        danger
        description={t("settings.secrets.deleteDescription")}
        loading={remove.isPending}
        onConfirm={() => deleting && remove.mutate(deleting.id)}
        onOpenChange={(open) => {
          if (!open) setDeleting(null);
        }}
        open={deleting !== null}
        title={`${t("settings.secrets.deleteTitle")} · ${deleting?.name ?? ""}`}
      >
        {deleting && deleting.reference_count > 0 && (
          <p className="argus-settings-warning" role="alert">
            {t("settings.secrets.deleteReferencedWarning", {
              count: deleting.reference_count,
            })}
          </p>
        )}
      </ConfirmDialog>
      <CredentialDrawer
        credential={editingCredential}
        loading={saveCredential.isPending}
        onOpenChange={(open) => {
          setCredentialDrawerOpen(open);
          if (!open) setEditingCredential(null);
        }}
        onSubmit={(input) =>
          saveCredential.mutateAsync({
            ...input,
            id: editingCredential?.id,
            expected_version: editingCredential?.version,
          })
        }
        open={credentialDrawerOpen}
        secrets={secrets.data?.items ?? []}
      />
    </PageShell>
  );
}
function CredentialDrawer({
  open,
  onOpenChange,
  onSubmit,
  loading,
  credential,
  secrets,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (input: {
    name: string;
    protocol: Credential["protocol"];
    username?: string;
    secret_id: string;
  }) => Promise<unknown>;
  loading: boolean;
  credential: Credential | null;
  secrets: Secret[];
}) {
  const { t } = useTranslation();
  const schema = useMemo(
    () =>
      z.object({
        name: z
          .string()
          .trim()
          .min(1, t("settings.common.required"))
          .max(credentialConstraints.name.maxLength ?? 128),
        protocol: z.enum(["ssh", "winrm", "kubernetes", "http"]),
        username: z
          .string()
          .trim()
          .max(credentialConstraints.username.maxLength ?? 256),
        secretId: z.string().min(1, t("settings.common.required")),
      }),
    [t],
  );
  type CredentialForm = z.infer<typeof schema>;
  const {
    control,
    register,
    reset,
    setError,
    setValue,
    watch,
    handleSubmit,
    formState: { errors },
  } = useForm<CredentialForm>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: "",
      protocol: "ssh",
      username: "",
      secretId: "",
    },
  });
  useEffect(() => {
    if (!open) return;
    reset({
      name: credential?.name ?? "",
      protocol: credential?.protocol ?? "ssh",
      username: credential?.username ?? "",
      secretId: credential?.secret_id ?? "",
    });
  }, [credential, open, reset]);
  const protocol = watch("protocol");
  const compatibleSecrets = secrets.filter((secret) => {
    if (protocol === "kubernetes") return secret.type === "kubeconfig";
    if (protocol === "winrm") return secret.type === "winrm_password";
    if (protocol === "ssh") {
      return ["ssh_password", "ssh_private_key"].includes(secret.type);
    }
    return ["api_token", "basic_auth"].includes(secret.type);
  });
  return (
    <FormDrawer
      loading={loading}
      onOpenChange={onOpenChange}
      onSubmit={handleSubmit(async (values) => {
        try {
          await onSubmit({
            name: values.name,
            protocol: values.protocol,
            username: values.username || undefined,
            secret_id: values.secretId,
          });
        } catch (error) {
          const field = apiErrorField(error);
          const formField =
            field === "name"
              ? "name"
              : field === "username"
                ? "username"
                : field === "secret_id"
                  ? "secretId"
                  : undefined;
          const message = formatApiError(
            error,
            t("settings.secrets.saveFailed"),
            (requestId) => t("common.requestReference", { requestId }),
          );
          if (formField) {
            setError(formField, { message, type: "server" }, { shouldFocus: true });
          } else {
            setError("root", { message, type: "server" });
          }
        }
      })}
      open={open}
      title={t("settings.secrets.credentialTitle")}
    >
      <div className="argus-settings-form">
        {errors.root?.message && (
          <Alert
            description={errors.root.message}
            title={t("settings.secrets.saveFailed")}
            tone="danger"
          />
        )}
        <Field requirement="required" error={errors.name?.message} label={t("settings.secrets.name")}>
          <Input {...register("name")} maxLength={credentialConstraints.name.maxLength} required />
        </Field>
        <Field requirement="required" label={t("settings.secrets.protocol")}>
          <Controller control={control} name="protocol" render={({ field }) => (
            <Select
              disabled={credential !== null}
              onValueChange={(value) => {
                field.onChange(value as Credential["protocol"]);
                setValue("secretId", "", { shouldValidate: true });
              }}
              options={CREDENTIAL_PROTOCOLS.map((item) => ({ value: item, label: item }))}
              value={field.value}
            />
          )} />
        </Field>
        <Field requirement="optional" error={errors.username?.message} label={t("settings.secrets.username")}>
          <Input {...register("username")} maxLength={credentialConstraints.username.maxLength} />
        </Field>
        <Field requirement="required" error={errors.secretId?.message} label={t("settings.secrets.secretRef")}>
          <Controller control={control} name="secretId" render={({ field }) => (
            <Select
              onValueChange={field.onChange}
              options={compatibleSecrets.map((secret) => ({ value: secret.id, label: secret.name }))}
              value={field.value}
            />
          )} />
        </Field>
      </div>
    </FormDrawer>
  );
}

function SecretDrawer({
  open,
  onOpenChange,
  onSubmit,
  loading,
  secret,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (input: {
    name: string;
    type: SecretType;
    description?: string;
    value?: string;
  }) => Promise<unknown>;
  loading: boolean;
  secret: Secret | null;
}) {
  const { t } = useTranslation();
  const schema = useMemo(
    () =>
      z.object({
        name: z
          .string()
          .trim()
          .min(1, t("settings.common.required"))
          .max(secretConstraints.name.maxLength ?? 128),
        type: z.enum([
          "ssh_password",
          "ssh_private_key",
          "winrm_password",
          "kubeconfig",
          "api_token",
          "basic_auth",
        ]),
        description: z
          .string()
          .trim()
          .max(secretConstraints.description.maxLength ?? 2048),
        value: secret
          ? z.string().max(secretConstraints.value.maxLength ?? 1048576)
          : z
              .string()
              .min(1, t("settings.common.required"))
              .max(secretConstraints.value.maxLength ?? 1048576),
      }),
    [secret, t],
  );
  type SecretForm = z.infer<typeof schema>;
  const {
    control,
    register,
    reset,
    setError,
    handleSubmit,
    formState: { errors },
  } = useForm<SecretForm>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: "",
      type: "ssh_password",
      description: "",
      value: "",
    },
  });
  useEffect(() => {
    if (!open) return;
    reset({
      name: secret?.name ?? "",
      type: secret?.type ?? "ssh_password",
      description: secret?.description ?? "",
      value: "",
    });
  }, [open, reset, secret]);

  return (
    <FormDrawer
      loading={loading}
      onOpenChange={onOpenChange}
      onSubmit={handleSubmit(async (values) => {
        try {
          await onSubmit({
            name: values.name,
            type: values.type,
            description: values.description || undefined,
            value: values.value || undefined,
          });
        } catch (error) {
          const field = apiErrorField(error);
          const formField =
            field === "name" || field === "description" || field === "value"
              ? field
              : undefined;
          const message = formatApiError(
            error,
            t("settings.secrets.saveFailed"),
            (requestId) => t("common.requestReference", { requestId }),
          );
          if (formField) {
            setError(formField, { message, type: "server" }, { shouldFocus: true });
          } else {
            setError("root", { message, type: "server" });
          }
        }
      })}
      open={open}
      title={
        secret
          ? t("settings.secrets.editTitle")
          : t("settings.secrets.createTitle")
      }
    >
      <div className="argus-settings-form">
        {errors.root?.message && (
          <Alert
            description={errors.root.message}
            title={t("settings.secrets.saveFailed")}
            tone="danger"
          />
        )}
        <Field requirement="required" error={errors.name?.message} label={t("settings.secrets.name")}>
          <Input
            {...register("name")}
            autoComplete="off"
            maxLength={secretConstraints.name.maxLength}
            required
          />
        </Field>
        <Field requirement="required" label={t("settings.secrets.type")}>
          <Controller control={control} name="type" render={({ field }) => (
            <Select
              disabled={secret !== null}
              onValueChange={field.onChange}
              options={SECRET_TYPES.map((item) => ({ value: item, label: t(`settings.secrets.types.${item}`) }))}
              value={field.value}
            />
          )} />
        </Field>
        <Field requirement="optional" error={errors.description?.message} label={t("settings.common.description")}>
          <Input
            {...register("description")}
            maxLength={secretConstraints.description.maxLength}
          />
        </Field>
        <Field requirement={secret ? "optional" : "required"}
          error={errors.value?.message}
          hint={
            secret
              ? t("settings.secrets.valueHintEdit")
              : t("settings.secrets.valueHintCreate")
          }
          label={t("settings.secrets.value")}
        >
          <Textarea
            {...register("value")}
            autoComplete="new-password"
            required={secret === null}
            rows={4}
            style={{ fontFamily: "var(--font-mono)" }}
          />
        </Field>
      </div>
    </FormDrawer>
  );
}
