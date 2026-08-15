import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useApi } from "@argus/api-client";
import type { Secret, SecretType } from "@argus/api-client";
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
import "../i18n/settings";
import "../styles/settings.css";
import { formatDateTime } from "../components/settings/shared";

const SECRET_TYPES: SecretType[] = [
  "ssh_password",
  "ssh_private_key",
  "winrm_password",
  "kubeconfig",
  "api_token",
  "basic_auth",
];

type SecretRow = {
  id: string;
  name: string;
  type: SecretType;
  description: string;
  referenceCount: number;
  createdBy: string;
  updatedAt: string;
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

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["secrets"] });

  const save = useMutation({
    mutationFn: (input: {
      id?: string;
      name: string;
      type: SecretType;
      description?: string;
      value?: string;
    }) =>
      input.id
        ? api.secrets.update(input.id, {
            name: input.name,
            description: input.description,
            value: input.value || undefined,
          })
        : api.secrets.create({
            name: input.name,
            type: input.type,
            description: input.description,
            value: input.value ?? "",
          }),
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

  const rows: SecretRow[] = (secrets.data?.items ?? []).map((secret) => ({
    id: secret.id,
    name: secret.name,
    type: secret.type,
    description: secret.description ?? "",
    referenceCount: secret.referenceCount,
    createdBy: secret.createdBy,
    updatedAt: secret.updatedAt,
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
                key: "referenceCount",
                header: t("settings.secrets.referenceCount"),
                render: (row) =>
                  row.referenceCount > 0 ? (
                    <Badge tone="warning">{row.referenceCount}</Badge>
                  ) : (
                    "0"
                  ),
              },
              {
                key: "createdBy",
                header: t("settings.secrets.createdBy"),
                render: (row) => <code className="mono">{row.createdBy}</code>,
              },
              {
                key: "updatedAt",
                header: t("settings.common.updatedAt"),
                render: (row) => formatDateTime(row.updatedAt),
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
        {deleting && deleting.referenceCount > 0 && (
          <p className="argus-settings-warning" role="alert">
            {t("settings.secrets.deleteReferencedWarning", {
              count: deleting.referenceCount,
            })}
          </p>
        )}
      </ConfirmDialog>
    </PageShell>
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
