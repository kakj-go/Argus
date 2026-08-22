import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import type { Secret, SecretType } from "@argus/api-client";
import type { Credential } from "@argus/api-client/contracts";
import {
  Alert,
  Badge,
  Button,
  ConfirmDialog,
  DataTable,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  PageShell,
  Select,
  Spinner,
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

type SecretRow = {
  id: string;
  name: string;
  type: SecretType;
  description: string;
  reference_count: number;
  created_by: string;
  updated_at: string;
};

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
  const [editingCredential, setEditingCredential] =
    useState<Credential | null>(null);

  const credentials = useQuery({
    queryKey: ["credentials"],
    queryFn: () => api.secrets.listCredentials(),
  });

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
    created_by: secret.created_by,
    updated_at: secret.updated_at,
  }));

  return (
    <PageShell
      description={t("settings.secrets.description")}
      title={t("settings.secrets.title")}
      actions={
        <Button
          onClick={() => {
            setEditing(null);
            setDrawerOpen(true);
          }}
          size="sm"
          variant="primary"
        >
          {t("settings.secrets.create")}
        </Button>
      }
    >
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
                key: "created_by",
                header: t("settings.secrets.createdBy"),
                render: (row) => (
                  <code className="argus-mono">{row.created_by}</code>
                ),
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
        <section className="argus-settings-section">
          <div className="argus-settings-section__header">
            <div>
              <h2>{t("settings.secrets.credentialsTitle")}</h2>
              <p>{t("settings.secrets.credentialsDescription")}</p>
            </div>
            <Button
              onClick={() => {
                setEditingCredential(null);
                setCredentialDrawerOpen(true);
              }}
              size="sm"
              variant="secondary"
            >
              {t("settings.secrets.createCredential")}
            </Button>
          </div>
          {(credentials.data ?? []).length === 0 ? (
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
                  render: (row) => (
                    <code className="argus-mono">{row.secret_id}</code>
                  ),
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
        <ManagedAccountsSection />
      </div>

      <SecretDrawer
        loading={save.isPending}
        onOpenChange={(open) => {
          setDrawerOpen(open);
          if (!open) setEditing(null);
        }}
        onSubmit={(input) => save.mutate({ ...input, id: editing?.id })}
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
          saveCredential.mutate({
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
  }) => void;
  loading: boolean;
  credential: Credential | null;
  secrets: Secret[];
}) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [protocol, setProtocol] =
    useState<Credential["protocol"]>("ssh");
  const [username, setUsername] = useState("");
  const [secretId, setSecretId] = useState("");
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  const key = open ? (credential?.id ?? "__new_credential__") : null;
  if (key && loadedFor !== key) {
    setLoadedFor(key);
    setName(credential?.name ?? "");
    setProtocol(credential?.protocol ?? "ssh");
    setUsername(credential?.username ?? "");
    setSecretId(credential?.secret_id ?? "");
  }
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
      onSubmit={() =>
        onSubmit({
          name: name.trim(),
          protocol,
          username: username.trim() || undefined,
          secret_id: secretId,
        })
      }
      open={open}
      title={t("settings.secrets.credentialTitle")}
    >
      <div className="argus-settings-form">
        <Field label={t("settings.secrets.name")}>
          <Input onChange={(event) => setName(event.target.value)} value={name} />
        </Field>
        <Field label={t("settings.secrets.protocol")}>
          <Select
            disabled={credential !== null}
            onValueChange={(value) => {
              setProtocol(value as Credential["protocol"]);
              setSecretId("");
            }}
            options={CREDENTIAL_PROTOCOLS.map((item) => ({
              value: item,
              label: item,
            }))}
            value={protocol}
          />
        </Field>
        <Field label={t("settings.secrets.username")}>
          <Input
            onChange={(event) => setUsername(event.target.value)}
            value={username}
          />
        </Field>
        <Field label={t("settings.secrets.secretRef")}>
          <Select
            onValueChange={setSecretId}
            options={compatibleSecrets.map((secret) => ({
              value: secret.id,
              label: secret.name,
            }))}
            value={secretId}
          />
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
  }) => void;
  loading: boolean;
  secret: Secret | null;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [type, setType] = useState<SecretType>("ssh_password");
  const [description, setDescription] = useState("");
  const [value, setValue] = useState("");
  const [loadedFor, setLoadedFor] = useState<string | null>(null);

  const key = open ? (secret?.id ?? "__new__") : null;
  if (key && loadedFor !== key) {
    setLoadedFor(key);
    setName(secret?.name ?? "");
    setType(secret?.type ?? "ssh_password");
    setDescription(secret?.description ?? "");
    // 值永远不回显；编辑时留空表示不轮换。
    setValue("");
  }

  return (
    <FormDrawer
      loading={loading}
      onOpenChange={onOpenChange}
      onSubmit={() =>
        onSubmit({
          name: name.trim(),
          type,
          description: description.trim() || undefined,
          value: value || undefined,
        })
      }
      open={open}
      title={
        secret
          ? t("settings.secrets.editTitle")
          : t("settings.secrets.createTitle")
      }
    >
      <div className="argus-settings-form">
        <Field label={t("settings.secrets.name")}>
          <Input
            autoComplete="off"
            onChange={(event) => setName(event.target.value)}
            required
            value={name}
          />
        </Field>
        <Field label={t("settings.secrets.type")}>
          <Select
            disabled={secret !== null}
            onValueChange={(value) => setType(value as SecretType)}
            options={SECRET_TYPES.map((item) => ({
              value: item,
              label: t(`settings.secrets.types.${item}`),
            }))}
            value={type}
          />
        </Field>
        <Field label={t("settings.common.description")}>
          <Input
            onChange={(event) => setDescription(event.target.value)}
            value={description}
          />
        </Field>
        <Field
          hint={
            secret
              ? t("settings.secrets.valueHintEdit")
              : t("settings.secrets.valueHintCreate")
          }
          label={t("settings.secrets.value")}
        >
          <Textarea
            autoComplete="new-password"
            onChange={(event) => setValue(event.target.value)}
            required={secret === null}
            rows={4}
            style={{ fontFamily: "var(--font-mono)" }}
            value={value}
          />
        </Field>
      </div>
    </FormDrawer>
  );
}
