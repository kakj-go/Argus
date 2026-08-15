import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useApi, type SandboxSessionMeta } from "@argus/api-client";
import {
  Alert,
  Button,
  ConfirmDialog,
  DataTable,
  EmptyState,
  Spinner,
  StatusBadge,
} from "@argus/ui";
import { formatDateTime } from "../../lib/format";

type SessionRow = {
  id: string;
  enterpriseName: string;
  profileName: string;
  userId: string;
  purpose: string;
  status: SandboxSessionMeta["status"];
  startedAt?: string;
  lastActivityAt?: string;
};

function statusTone(status: SandboxSessionMeta["status"]) {
  if (status === "running") return "success" as const;
  if (status === "idle" || status === "starting" || status === "requested")
    return "info" as const;
  if (status === "failed" || status === "rejected") return "danger" as const;
  return "neutral" as const;
}

/** 活动会话 Tab：仅元数据（企业/Profile/状态/启动时间），支持终止。 */
export function SessionsTab() {
  const { t, i18n } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const [terminating, setTerminating] = useState<SessionRow | null>(null);

  const sessions = useQuery({
    queryKey: ["platform", "sessions"],
    queryFn: () => api.platform.sessions.list(),
  });
  const enterprises = useQuery({
    queryKey: ["platform", "enterprises"],
    queryFn: () => api.platform.enterprises.list(),
  });
  const profiles = useQuery({
    queryKey: ["platform", "profiles"],
    queryFn: () => api.platform.profiles.list(),
  });

  const terminate = useMutation({
    mutationFn: (id: string) => api.platform.sessions.terminate(id),
    onSuccess: () => {
      setTerminating(null);
      void queryClient.invalidateQueries({ queryKey: ["platform", "sessions"] });
    },
  });

  const enterpriseName = (id: string) =>
    enterprises.data?.items.find((item) => item.id === id)?.name ?? id;
  const profileName = (id: string) =>
    profiles.data?.find((item) => item.id === id)?.name ?? id;

  const rows: SessionRow[] = (sessions.data ?? []).map((item) => ({
    id: item.id,
    enterpriseName: enterpriseName(item.enterpriseId),
    profileName: profileName(item.profileId),
    userId: item.userId,
    purpose: item.purpose ?? "",
    status: item.status,
    startedAt: item.startedAt,
    lastActivityAt: item.lastActivityAt,
  }));

  const isActive = (status: SandboxSessionMeta["status"]) =>
    ["requested", "starting", "running", "idle"].includes(status);

  return (
    <div className="platform-stack">
      <Alert
        description={t("sandbox.sessions.metadataOnly.description")}
        title={t("sandbox.sessions.metadataOnly.title")}
        tone="info"
      />

      {sessions.isPending ? (
        <Spinner />
      ) : rows.length === 0 ? (
        <EmptyState description="" title={t("sandbox.sessions.empty")} />
      ) : (
        <DataTable<SessionRow>
          columns={[
            {
              key: "id",
              header: t("sandbox.sessions.table.id"),
              render: (row) => <code className="mono">{row.id}</code>,
            },
            {
              key: "enterpriseName",
              header: t("sandbox.sessions.table.enterprise"),
            },
            {
              key: "profileName",
              header: t("sandbox.sessions.table.profile"),
            },
            {
              key: "userId",
              header: t("sandbox.sessions.table.user"),
              render: (row) => <code className="mono">{row.userId}</code>,
            },
            {
              key: "purpose",
              header: t("sandbox.sessions.table.purpose"),
              render: (row) => row.purpose || t("common.none"),
            },
            {
              key: "status",
              header: t("sandbox.sessions.table.status"),
              render: (row) => (
                <StatusBadge
                  pulse={row.status === "running"}
                  tone={statusTone(row.status)}
                >
                  {t(`sandbox.sessions.status.${row.status}`)}
                </StatusBadge>
              ),
            },
            {
              key: "startedAt",
              header: t("sandbox.sessions.table.startedAt"),
              render: (row) => formatDateTime(row.startedAt, i18n.language),
            },
            {
              key: "lastActivityAt",
              header: t("sandbox.sessions.table.lastActivity"),
              render: (row) =>
                formatDateTime(row.lastActivityAt, i18n.language),
            },
            {
              key: "terminate",
              header: t("common.actions"),
              render: (row) =>
                isActive(row.status) ? (
                  <Button
                    onClick={() => setTerminating(row)}
                    size="sm"
                    variant="ghost"
                  >
                    {t("sandbox.sessions.terminate")}
                  </Button>
                ) : (
                  t("common.none")
                ),
            },
          ]}
          data={rows}
          getRowKey={(row) => row.id}
        />
      )}

      <ConfirmDialog
        danger
        description={
          terminating
            ? `${terminating.id} — ${t("sandbox.sessions.confirm.description")}`
            : undefined
        }
        loading={terminate.isPending}
        onConfirm={() => terminating && terminate.mutate(terminating.id)}
        onOpenChange={(open) => {
          if (!open) setTerminating(null);
        }}
        open={terminating !== null}
        title={t("sandbox.sessions.confirm.title")}
      />
    </div>
  );
}
