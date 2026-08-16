import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useApi, type EnterpriseAdmin } from "@argus/api-client";
import {
  Alert,
  Button,
  CodeBlock,
  ConfirmDialog,
  DataTable,
  EmptyState,
  Field,
  FormDrawer,
  Input,
  PageShell,
  Select,
  Spinner,
  StatusBadge,
} from "@argus/ui";
import { formatDateTime } from "../lib/format";

type AdminRow = {
  id: string;
  displayName: string;
  username: string;
  email: string;
  enterpriseId: string;
  enterpriseName: string;
  inviteStatus: EnterpriseAdmin["inviteStatus"];
  lastLoginAt?: string;
};

type AdminAction = "resend" | "resetAuth" | "disable";

function inviteTone(status: EnterpriseAdmin["inviteStatus"]) {
  if (status === "activated") return "success" as const;
  if (status === "pending") return "warning" as const;
  return "danger" as const;
}

/**
 * 企业管理员：邀请（创建后展示激活链接）、重发邀请、重置认证、禁用。
 * 明确不提供代登录。
 */
export function AdminsPage() {
  const { t, i18n } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();

  const [inviteOpen, setInviteOpen] = useState(false);
  const [created, setCreated] = useState<EnterpriseAdmin | null>(null);
  const [pendingAction, setPendingAction] = useState<{
    type: AdminAction;
    admin: AdminRow;
  } | null>(null);

  const [enterpriseId, setEnterpriseId] = useState("");
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [email, setEmail] = useState("");
  const [activation, setActivation] = useState<
    "invite_link" | "temporary_password"
  >("invite_link");

  const enterprises = useQuery({
    queryKey: ["platform", "enterprises"],
    queryFn: () => api.platform.enterprises.list(),
  });
  const admins = useQuery({
    queryKey: ["platform", "admins"],
    queryFn: () => api.platform.admins.list(),
  });

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["platform", "admins"] });

  const create = useMutation({
    mutationFn: () =>
      api.platform.admins.create({
        enterpriseId,
        username: username.trim(),
        displayName: displayName.trim(),
        email: email.trim() || undefined,
        activation,
      }),
    onSuccess: (admin) => {
      setInviteOpen(false);
      setCreated(admin);
      setUsername("");
      setDisplayName("");
      setEmail("");
      void invalidate();
    },
  });

  const action = useMutation({
    mutationFn: (input: { type: AdminAction; id: string }) =>
      input.type === "resend"
        ? api.platform.admins.resendInvite(input.id)
        : api.platform.admins[input.type](input.id),
    onSuccess: () => {
      setPendingAction(null);
      void invalidate();
    },
  });

  const enterpriseName = (id: string) =>
    enterprises.data?.items.find((item) => item.id === id)?.name ?? id;

  const rows: AdminRow[] = (admins.data ?? []).map((item) => ({
    id: item.id,
    displayName: item.displayName,
    username: item.username,
    email: item.email ?? "",
    enterpriseId: item.enterpriseId,
    enterpriseName: enterpriseName(item.enterpriseId),
    inviteStatus: item.inviteStatus,
    lastLoginAt: item.lastLoginAt,
  }));

  const activeEnterprises = (enterprises.data?.items ?? []).filter(
    (item) => item.status === "active",
  );

  return (
    <PageShell
      actions={
        <Button
          onClick={() => {
            setEnterpriseId(activeEnterprises[0]?.id ?? "");
            setInviteOpen(true);
          }}
          variant="primary"
        >
          {t("admins.invite")}
        </Button>
      }
      description={t("admins.description")}
      title={t("admins.title")}
    >
      <div className="argus-platform-stack">
        <Alert
          description={t("admins.noImpersonation.description")}
          title={t("admins.noImpersonation.title")}
          tone="info"
        />

        {admins.isPending ? (
          <Spinner />
        ) : rows.length === 0 ? (
          <EmptyState description="" title={t("admins.empty")} />
        ) : (
          <DataTable<AdminRow>
            columns={[
              { key: "displayName", header: t("admins.table.displayName") },
              {
                key: "username",
                header: t("admins.table.username"),
                render: (row) => (
                  <code className="argus-mono">{row.username}</code>
                ),
              },
              {
                key: "email",
                header: t("admins.table.email"),
                render: (row) => row.email || t("common.none"),
              },
              { key: "enterpriseName", header: t("admins.table.enterprise") },
              {
                key: "inviteStatus",
                header: t("admins.table.inviteStatus"),
                render: (row) => (
                  <StatusBadge tone={inviteTone(row.inviteStatus)}>
                    {t(`admins.status.${row.inviteStatus}`)}
                  </StatusBadge>
                ),
              },
              {
                key: "lastLoginAt",
                header: t("admins.table.lastLogin"),
                render: (row) => formatDateTime(row.lastLoginAt, i18n.language),
              },
              {
                key: "id",
                header: t("common.actions"),
                render: (row) => (
                  <div className="argus-row-actions">
                    <Button
                      onClick={() =>
                        setPendingAction({ type: "resend", admin: row })
                      }
                      size="sm"
                      variant="ghost"
                    >
                      {t("admins.action.resend")}
                    </Button>
                    <Button
                      onClick={() =>
                        setPendingAction({ type: "resetAuth", admin: row })
                      }
                      size="sm"
                      variant="ghost"
                    >
                      {t("admins.action.resetAuth")}
                    </Button>
                    {row.inviteStatus !== "disabled" && (
                      <Button
                        onClick={() =>
                          setPendingAction({ type: "disable", admin: row })
                        }
                        size="sm"
                        variant="ghost"
                      >
                        {t("admins.action.disable")}
                      </Button>
                    )}
                  </div>
                ),
              },
            ]}
            data={rows}
            getRowKey={(row) => row.id}
          />
        )}
      </div>

      {/* 邀请管理员 */}
      <FormDrawer
        description={t("admins.form.description")}
        loading={create.isPending}
        onOpenChange={setInviteOpen}
        onSubmit={() => create.mutate()}
        open={inviteOpen}
        submitLabel={t("common.create")}
        title={t("admins.form.title")}
      >
        <Field label={t("admins.form.enterprise")}>
          <Select
            onValueChange={setEnterpriseId}
            options={activeEnterprises.map((enterprise) => ({
              value: enterprise.id,
              label: enterprise.name,
            }))}
            value={enterpriseId}
          />
        </Field>
        <Field label={t("admins.form.displayName")}>
          <Input
            onChange={(event) => setDisplayName(event.target.value)}
            required
            value={displayName}
          />
        </Field>
        <Field label={t("admins.form.username")}>
          <Input
            onChange={(event) => setUsername(event.target.value)}
            required
            value={username}
          />
        </Field>
        <Field label={t("admins.form.email")}>
          <Input
            onChange={(event) => setEmail(event.target.value)}
            type="email"
            value={email}
          />
        </Field>
        <Field label={t("admins.form.activation")}>
          <Select
            onValueChange={(value) =>
              setActivation(value as "invite_link" | "temporary_password")
            }
            options={[
              {
                value: "invite_link",
                label: t("admins.activation.invite_link"),
              },
              {
                value: "temporary_password",
                label: t("admins.activation.temporary_password"),
              },
            ]}
            value={activation}
          />
        </Field>
      </FormDrawer>

      {/* 创建成功后展示激活链接 */}
      <FormDrawer
        footer={
          <Button onClick={() => setCreated(null)} variant="primary">
            {t("common.close")}
          </Button>
        }
        onOpenChange={(open) => {
          if (!open) setCreated(null);
        }}
        open={created !== null}
        title={t("admins.created.title")}
      >
        {created && (
          <div className="argus-drawer-stack">
            <Alert
              description={t("admins.created.description")}
              title={t("admins.created.title")}
              tone="success"
            />
            <CodeBlock
              code={`https://enterprise.argus.local/activate/${created.id}?user=${created.username}`}
              language="text"
            />
          </div>
        )}
      </FormDrawer>

      {/* 操作确认 */}
      <ConfirmDialog
        danger={pendingAction?.type !== "resend"}
        description={
          pendingAction
            ? `${pendingAction.admin.displayName} (${pendingAction.admin.username}) — ${t(`admins.confirm.${pendingAction.type}.description`)}`
            : undefined
        }
        loading={action.isPending}
        onConfirm={() =>
          pendingAction &&
          action.mutate({
            type: pendingAction.type,
            id: pendingAction.admin.id,
          })
        }
        onOpenChange={(open) => {
          if (!open) setPendingAction(null);
        }}
        open={pendingAction !== null}
        title={
          pendingAction ? t(`admins.confirm.${pendingAction.type}.title`) : ""
        }
      />
    </PageShell>
  );
}
