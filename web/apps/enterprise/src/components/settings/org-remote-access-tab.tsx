import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import type { RemoteAccessGrantWrite, RemoteAccessPolicyWrite } from "@argus/api-client";
import { useApi } from "@argus/api-client";
import { Button, DataTable, EmptyState, Field, FormDrawer, Input, Select, StatusBadge, Switch } from "@argus/ui";

const splitIds = (value: string) => value.split(/[\s,]+/).map((item) => item.trim()).filter(Boolean);
const uuid = z.string().uuid();

type GrantForm = {
  subject_type: "user" | "department";
  subject_id: string;
  host_ids: string;
  account_ids: string;
  protocol: "ssh" | "winrs";
  valid_until: string;
};

type PolicyForm = {
  name: string;
  approver_role_ids: string;
  minimum_approvals: number;
  require_mfa: boolean;
};

export function OrgRemoteAccessTab() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const grants = useQuery({ queryKey: ["remote-access", "grants"], queryFn: () => api.remoteAccess.listGrants() });
  const policies = useQuery({ queryKey: ["remote-access", "policies"], queryFn: () => api.remoteAccess.listPolicies() });
  const [grantOpen, setGrantOpen] = useState(false);
  const [policyOpen, setPolicyOpen] = useState(false);
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ["remote-access"] });
  const createGrant = useMutation({ mutationFn: (input: RemoteAccessGrantWrite) => api.remoteAccess.createGrant(input), onSuccess: () => { setGrantOpen(false); void invalidate(); } });
  const createPolicy = useMutation({ mutationFn: (input: RemoteAccessPolicyWrite) => api.remoteAccess.createPolicy(input), onSuccess: () => { setPolicyOpen(false); void invalidate(); } });

  return <div className="argus-settings-section">
    <div className="argus-settings-section__head"><h2 className="argus-settings-section__title">{t("remoteAccess.grants")}</h2><Button onClick={() => setGrantOpen(true)} size="sm" variant="primary">{t("remoteAccess.newGrant")}</Button></div>
    {(grants.data?.items.length ?? 0) === 0 ? <EmptyState description="" title={t("remoteAccess.noGrants")} /> : <DataTable
      columns={[
        { key: "subject", header: t("remoteAccess.subject"), render: (row) => `${row.subject_type}: ${row.subject_id}` },
        { key: "hosts", header: t("remoteAccess.hosts"), render: (row) => row.host_ids.join(", ") || t("remoteAccess.labelSelector") },
        { key: "accounts", header: t("remoteAccess.accounts"), render: (row) => String(row.managed_account_ids.length) },
        { key: "protocols", header: t("remoteAccess.protocols"), render: (row) => row.protocols.join(", ") },
        { key: "valid_until", header: t("remoteAccess.validUntil"), render: (row) => new Date(row.valid_until).toLocaleString() },
        { key: "enabled", header: t("remoteAccess.status"), render: (row) => <StatusBadge tone={row.enabled ? "success" : "neutral"}>{t(row.enabled ? "remoteAccess.enabled" : "remoteAccess.disabled")}</StatusBadge> },
        { key: "actions", header: t("remoteAccess.actions"), render: (row) => row.enabled ? <Button onClick={() => void api.remoteAccess.disableGrant(row.id).then(invalidate)} size="sm" variant="danger">{t("remoteAccess.disable")}</Button> : null },
      ]}
      data={grants.data?.items ?? []} getRowKey={(row) => row.id}
    />}

    <div className="argus-settings-section__head"><h2 className="argus-settings-section__title">{t("remoteAccess.policies")}</h2><Button onClick={() => setPolicyOpen(true)} size="sm" variant="primary">{t("remoteAccess.newPolicy")}</Button></div>
    {(policies.data?.items.length ?? 0) === 0 ? <EmptyState description="" title={t("remoteAccess.noPolicies")} /> : <DataTable
      columns={[
        { key: "name", header: t("remoteAccess.name") },
        { key: "protocols", header: t("remoteAccess.protocols"), render: (row) => row.protocols.join(", ") },
        { key: "minimum_approvals", header: t("remoteAccess.approvals") },
        { key: "require_mfa", header: t("remoteAccess.mfa"), render: (row) => row.require_mfa ? t("remoteAccess.mfaRequiredM8") : t("remoteAccess.no") },
        { key: "duration", header: t("remoteAccess.limits"), render: (row) => `${row.idle_timeout_seconds}s / ${row.max_session_seconds}s` },
        { key: "enabled", header: t("remoteAccess.status"), render: (row) => <StatusBadge tone={row.enabled ? "success" : "neutral"}>{t(row.enabled ? "remoteAccess.enabled" : "remoteAccess.disabled")}</StatusBadge> },
        { key: "actions", header: t("remoteAccess.actions"), render: (row) => row.enabled ? <Button onClick={() => void api.remoteAccess.disablePolicy(row.id).then(invalidate)} size="sm" variant="danger">{t("remoteAccess.disable")}</Button> : null },
      ]}
      data={policies.data?.items ?? []} getRowKey={(row) => row.id}
    />}
    <GrantDrawer loading={createGrant.isPending} onOpenChange={setGrantOpen} onSubmit={(value) => createGrant.mutate(value)} open={grantOpen} />
    <PolicyDrawer loading={createPolicy.isPending} onOpenChange={setPolicyOpen} onSubmit={(value) => createPolicy.mutate(value)} open={policyOpen} />
  </div>;
}

function GrantDrawer({ open, onOpenChange, onSubmit, loading }: { open: boolean; onOpenChange(open: boolean): void; onSubmit(input: RemoteAccessGrantWrite): void; loading: boolean }) {
  const { t } = useTranslation();
  const schema = useMemo(() => z.object({
    subject_type: z.enum(["user", "department"]),
    subject_id: uuid,
    host_ids: z.string().refine((value) => splitIds(value).length > 0, t("remoteAccess.atLeastOneId")).refine((value) => splitIds(value).every((id) => uuid.safeParse(id).success), t("remoteAccess.invalidUuid")),
    account_ids: z.string().refine((value) => splitIds(value).length > 0, t("remoteAccess.atLeastOneId")).refine((value) => splitIds(value).every((id) => uuid.safeParse(id).success), t("remoteAccess.invalidUuid")),
    protocol: z.enum(["ssh", "winrs"]),
    valid_until: z.string().min(1, t("remoteAccess.required")),
  }), [t]);
  const { control, register, handleSubmit, reset, formState: { errors } } = useForm<GrantForm>({ resolver: zodResolver(schema), defaultValues: { subject_type: "user", subject_id: "", host_ids: "", account_ids: "", protocol: "ssh", valid_until: "" } });
  useEffect(() => { if (open) reset({ subject_type: "user", subject_id: "", host_ids: "", account_ids: "", protocol: "ssh", valid_until: "" }); }, [open, reset]);
  const submit = handleSubmit((value) => onSubmit({ subject_type: value.subject_type, subject_id: value.subject_id, host_ids: splitIds(value.host_ids), managed_account_ids: splitIds(value.account_ids), protocols: [value.protocol], actions: ["terminal"], valid_from: new Date().toISOString(), valid_until: new Date(value.valid_until).toISOString(), enabled: true }));
  return <FormDrawer description={t("remoteAccess.grantDescription")} loading={loading} onOpenChange={onOpenChange} onSubmit={submit} open={open} submitLabel={t("remoteAccess.create")} title={t("remoteAccess.newGrant")}>
    <Field label={t("remoteAccess.subjectType")}><Controller control={control} name="subject_type" render={({ field }) => <Select ariaLabel={t("remoteAccess.subjectType")} onValueChange={field.onChange} options={[{ value: "user", label: t("remoteAccess.user") }, { value: "department", label: t("remoteAccess.department") }]} value={field.value} />} /></Field>
    <Field error={errors.subject_id?.message} label={t("remoteAccess.subjectId")}><Input {...register("subject_id")} /></Field>
    <Field error={errors.host_ids?.message} label={t("remoteAccess.hostIds")}><Input {...register("host_ids")} placeholder={t("remoteAccess.hostIdsHint")} /></Field>
    <Field error={errors.account_ids?.message} label={t("remoteAccess.accountIds")}><Input {...register("account_ids")} placeholder={t("remoteAccess.hostIdsHint")} /></Field>
    <Field label={t("remoteAccess.protocol")}><Controller control={control} name="protocol" render={({ field }) => <Select ariaLabel={t("remoteAccess.protocol")} onValueChange={field.onChange} options={[{ value: "ssh", label: "SSH PTY" }, { value: "winrs", label: "WinRS PowerShell" }]} value={field.value} />} /></Field>
    <Field error={errors.valid_until?.message} label={t("remoteAccess.validUntil")}><Input {...register("valid_until")} type="datetime-local" /></Field>
  </FormDrawer>;
}

function PolicyDrawer({ open, onOpenChange, onSubmit, loading }: { open: boolean; onOpenChange(open: boolean): void; onSubmit(input: RemoteAccessPolicyWrite): void; loading: boolean }) {
  const { t } = useTranslation();
  const schema = useMemo(() => z.object({
    name: z.string().trim().min(1, t("remoteAccess.required")),
    approver_role_ids: z.string().refine((value) => splitIds(value).length > 0, t("remoteAccess.atLeastOneId")).refine((value) => splitIds(value).every((id) => uuid.safeParse(id).success), t("remoteAccess.invalidUuid")),
    minimum_approvals: z.number().int().min(1).max(16),
    require_mfa: z.boolean(),
  }), [t]);
  const { control, register, handleSubmit, reset, formState: { errors } } = useForm<PolicyForm>({ resolver: zodResolver(schema), defaultValues: { name: "", approver_role_ids: "", minimum_approvals: 1, require_mfa: false } });
  useEffect(() => { if (open) reset({ name: "", approver_role_ids: "", minimum_approvals: 1, require_mfa: false }); }, [open, reset]);
  const submit = handleSubmit((value) => onSubmit({ name: value.name.trim(), enabled: true, priority: 100, protocols: ["ssh", "winrs"], approver_role_ids: splitIds(value.approver_role_ids), minimum_approvals: value.minimum_approvals, separation_of_duties: true, require_mfa: value.require_mfa, max_session_seconds: 3600, idle_timeout_seconds: 900 }));
  return <FormDrawer description={t("remoteAccess.policyDescription")} loading={loading} onOpenChange={onOpenChange} onSubmit={submit} open={open} submitLabel={t("remoteAccess.create")} title={t("remoteAccess.newPolicy")}>
    <Field error={errors.name?.message} label={t("remoteAccess.name")}><Input {...register("name")} /></Field>
    <Field error={errors.approver_role_ids?.message} label={t("remoteAccess.approverRoleIds")}><Input {...register("approver_role_ids")} placeholder={t("remoteAccess.hostIdsHint")} /></Field>
    <Field error={errors.minimum_approvals?.message} label={t("remoteAccess.minimumApprovals")}><Input {...register("minimum_approvals", { valueAsNumber: true })} min="1" type="number" /></Field>
    <Controller control={control} name="require_mfa" render={({ field }) => <Switch checked={field.value} label={t("remoteAccess.requireMfa")} onChange={field.onChange} />} />
  </FormDrawer>;
}
