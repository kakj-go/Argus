import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { RemoteAccessReferences } from "@argus/api-client";
import { ActionGroup, Button, DataTable, Dialog, RowAction, StatusBadge } from "@argus/ui";

export type GovernanceItem = { id: string; name: string; status: "draft" | "enabled" | "disabled" | "archived"; version: number; updated_at: string };

export function GovernanceList<T extends GovernanceItem>({ items, extraColumns, onEdit, onEnable, onDisable, onRestore, onArchive, references }: {
  items: T[];
  extraColumns: { key: string; header: string; render(item: T): React.ReactNode }[];
  onEdit(item: T): void;
  onEnable(id: string): Promise<unknown>;
  onDisable(id: string): Promise<unknown>;
  onRestore(id: string): Promise<unknown>;
  onArchive(id: string): Promise<unknown>;
  references(id: string): Promise<RemoteAccessReferences>;
}) {
  const { t } = useTranslation();
  const [referenceID, setReferenceID] = useState<string | null>(null);
  const [pendingAction, setPendingAction] = useState<{ id: string; label: string; run: (id: string) => Promise<unknown> } | null>(null);
  const refQuery = useQuery({ queryKey: ["remote-access", "references", referenceID], queryFn: () => references(referenceID!), enabled: referenceID !== null });
  const selected = items.find((item) => item.id === referenceID);
  const action = async (callback: (id: string) => Promise<unknown>, id: string) => { await callback(id); };
  return <>
    <DataTable columns={[
      { key: "name", header: t("remoteAccess.name") },
      ...extraColumns,
      { key: "status", header: t("remoteAccess.status"), render: (row) => <StatusBadge tone={row.status === "enabled" ? "success" : row.status === "archived" ? "neutral" : "warning"}>{t(`remoteAccess.governanceStatus.${row.status}`)}</StatusBadge> },
      { key: "version", header: t("remoteAccess.version"), render: (row) => `v${row.version}` },
      { key: "updated_at", header: t("remoteAccess.updatedAt"), render: (row) => new Date(row.updated_at).toLocaleString() },
      { key: "actions", header: t("remoteAccess.actions"), render: (row) => <ActionGroup>
        <RowAction onClick={() => onEdit(row)}>{t("remoteAccess.edit")}</RowAction>
        <RowAction onClick={() => setReferenceID(row.id)}>{t("remoteAccess.details")}</RowAction>
        {row.status === "draft" && <RowAction onClick={() => void action(onEnable, row.id)}>{t("remoteAccess.enable")}</RowAction>}
        {row.status === "enabled" && <RowAction danger onClick={() => { setReferenceID(row.id); setPendingAction({ id: row.id, label: t("remoteAccess.disable"), run: onDisable }); }}>{t("remoteAccess.disable")}</RowAction>}
        {row.status === "disabled" && <RowAction onClick={() => void action(onEnable, row.id)}>{t("remoteAccess.enable")}</RowAction>}
        {row.status === "disabled" && <RowAction onClick={() => { setReferenceID(row.id); setPendingAction({ id: row.id, label: t("remoteAccess.archive"), run: onArchive }); }}>{t("remoteAccess.archive")}</RowAction>}
        {row.status === "archived" && <RowAction onClick={() => void action(onRestore, row.id)}>{t("remoteAccess.restore")}</RowAction>}
      </ActionGroup> },
    ]} data={items} getRowKey={(row) => row.id} />
    <Dialog description={t("remoteAccess.detailDescription")} onOpenChange={(open) => { if (!open) setReferenceID(null); }} open={referenceID !== null && pendingAction === null} title={selected?.name ?? t("remoteAccess.details")}>
      {selected && <dl className="argus-governance-references">
        <div><dt>{t("remoteAccess.status")}</dt><dd><StatusBadge tone={selected.status === "enabled" ? "success" : selected.status === "archived" ? "neutral" : "warning"}>{t(`remoteAccess.governanceStatus.${selected.status}`)}</StatusBadge></dd></div>
        <div><dt>{t("remoteAccess.version")}</dt><dd>v{selected.version}</dd></div>
        <div><dt>{t("remoteAccess.updatedAt")}</dt><dd>{new Date(selected.updated_at).toLocaleString()}</dd></div>
      </dl>}
      {refQuery.data && <dl className="argus-governance-references">
        <div><dt>{t("remoteAccess.rules")}</dt><dd>{refQuery.data.rules}</dd></div>
        <div><dt>{t("remoteAccess.requests")}</dt><dd>{refQuery.data.requests}</dd></div>
        <div><dt>{t("remoteAccess.leases")}</dt><dd>{refQuery.data.leases}</dd></div>
        <div><dt>{t("remoteAccess.sessions")}</dt><dd>{refQuery.data.sessions}</dd></div>
      </dl>}
      {refQuery.data && <p>{t("remoteAccess.detailImpact", { requests: refQuery.data.requests, leases: refQuery.data.leases, sessions: refQuery.data.sessions })}</p>}
    </Dialog>
    <Dialog
      description={t("remoteAccess.impactDescription")}
      footer={<><Button onClick={() => { setPendingAction(null); setReferenceID(null); }} variant="secondary">{t("common.cancel")}</Button><Button onClick={() => { if (pendingAction) void action(pendingAction.run, pendingAction.id).then(() => { setPendingAction(null); setReferenceID(null); }); }} variant="danger">{pendingAction?.label ?? t("remoteAccess.confirm")}</Button></>}
      onOpenChange={(open) => { if (!open) { setPendingAction(null); setReferenceID(null); } }}
      open={pendingAction !== null}
      title={t("remoteAccess.impactTitle")}
    >
      {pendingAction && refQuery.data && <p>{t("remoteAccess.impactSummary", { action: pendingAction.label, requests: refQuery.data.requests, leases: refQuery.data.leases, sessions: refQuery.data.sessions })}</p>}
    </Dialog>
  </>;
}
