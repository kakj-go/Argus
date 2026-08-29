import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { z } from "zod";
import type { RemoteAccessRule, RemoteAccessRuleWrite } from "@argus/api-client";
import { useApi } from "@argus/api-client";
import { Alert, Button, CheckItem, EmptyState, Field, FormDrawer, Input, Select, StatusBadge, Switch } from "@argus/ui";
import { GovernanceList } from "./governance-list";

function RuleSimulator() {
  const { t } = useTranslation();
  const api = useApi();
  const hosts = useQuery({ queryKey: ["hosts", "rule-simulator"], queryFn: () => api.hosts.list() });
  const accounts = useQuery({ queryKey: ["managed-accounts", "rule-simulator"], queryFn: () => api.secrets.listManagedAccounts() });
  const [hostId, setHostId] = useState("");
  const [accountId, setAccountId] = useState("");
  const [protocol, setProtocol] = useState<"ssh" | "winrs">("ssh");
  const [stepUp, setStepUp] = useState(false);
  const [result, setResult] = useState<Awaited<ReturnType<typeof api.remoteAccess.simulateRule>> | null>(null);
  const simulatorForm = useForm({ resolver: zodResolver(z.object({ hostId: z.string().min(1), accountId: z.string().min(1) })), defaultValues: { hostId: "", accountId: "" } });
  const simulate = useMutation({ mutationFn: () => api.remoteAccess.simulateRule({ host_id: hostId, managed_account_id: accountId, protocol, action: "terminal", step_up_authenticated: stepUp }), onSuccess: setResult });
  return <section className="argus-rule-simulator">
    <div className="argus-settings-section__head"><h3 className="argus-settings-section__title">{t("remoteAccess.simulator.title")}</h3></div>
    <p className="argus-settings-section__hint">{t("remoteAccess.simulator.description")}</p>
    <form className="argus-rule-simulator__form" onSubmit={simulatorForm.handleSubmit(() => simulate.mutate())}>
      <Field error={simulatorForm.formState.errors.hostId?.message && t("remoteAccess.validation.required")} label={t("remoteAccess.hosts")} requirement="required"><Select ariaLabel={t("remoteAccess.hosts")} onValueChange={(value) => { setHostId(value); simulatorForm.setValue("hostId", value, { shouldValidate: true }); }} options={(hosts.data?.items ?? []).map((item) => ({ value: item.id, label: item.name }))} value={hostId} /></Field>
      <Field error={simulatorForm.formState.errors.accountId?.message && t("remoteAccess.validation.required")} label={t("remoteAccess.accounts")} requirement="required"><Select ariaLabel={t("remoteAccess.accounts")} onValueChange={(value) => { setAccountId(value); simulatorForm.setValue("accountId", value, { shouldValidate: true }); }} options={(accounts.data ?? []).map((item) => ({ value: item.id, label: item.username }))} value={accountId} /></Field>
      <Field label={t("remoteAccess.protocol")} requirement="required"><Select ariaLabel={t("remoteAccess.protocol")} onValueChange={(value) => setProtocol(value as "ssh" | "winrs")} options={[{ value: "ssh", label: "SSH" }, { value: "winrs", label: "WinRS" }]} value={protocol} /></Field>
      <div className="argus-profile-switch"><div><strong>{t("remoteAccess.stepUpState")}</strong><p>{t("remoteAccess.stepUpStateHelp")}</p></div><Switch checked={stepUp} label={t("remoteAccess.stepUpState")} onChange={setStepUp} /></div>
      <Button disabled={!hostId || !accountId} loading={simulate.isPending} type="submit" variant="secondary">{t("remoteAccess.simulator.run")}</Button>
    </form>
    {result && <div className="argus-rule-simulator__result"><StatusBadge tone={result.outcome === "allowed" ? "success" : result.outcome === "denied" ? "danger" : "warning"}>{result.outcome}</StatusBadge><p>{result.explanation.join(" ")}</p><small>{t("remoteAccess.simulator.reasonCodes", { values: result.reason_codes.join(", ") || t("common.unknown") })}</small><small>{t("remoteAccess.simulator.snapshot", { hash: result.snapshot_hash })}</small></div>}
  </section>;
}

const initial: RemoteAccessRuleWrite = { name: "", description: "", priority: 100, protocols: ["ssh"], actions: ["terminal"], source_cidrs: [], time_windows: [], effects: [], status: "draft" };
const effectValues = ["deny", "require_mfa", "require_approval", "notify"] as const;

function Checks<T extends string>({ values, selected, label, onChange }: { values: readonly T[]; selected: T[]; label(value: T): string; onChange(value: T[]): void }) {
  return <div className="argus-governance-checks">{values.map((value) => { const checked = selected.includes(value); return <button aria-pressed={checked} key={value} onClick={() => onChange(checked ? selected.filter((item) => item !== value) : [...selected, value])} type="button"><CheckItem checked={checked}>{label(value)}</CheckItem></button>; })}</div>;
}

export function RulesTab() {
  const { t } = useTranslation();
  const api = useApi();
  const qc = useQueryClient();
  const query = useQuery({ queryKey: ["remote-access", "rules"], queryFn: () => api.remoteAccess.listRules({ limit: 200 }) });
  const workflows = useQuery({ queryKey: ["remote-access", "workflows"], queryFn: () => api.remoteAccess.listApprovalWorkflows({ limit: 200 }) });
  const profiles = useQuery({ queryKey: ["remote-access", "profiles"], queryFn: () => api.remoteAccess.listSessionProfiles({ limit: 200 }) });
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<RemoteAccessRule | null>(null);
  const [form, setForm] = useState<RemoteAccessRuleWrite>(initial);
  const schema = useMemo(() => z.object({
    name: z.string().trim().min(1, t("remoteAccess.validation.required")).max(128),
    description: z.string().max(2048),
    priority: z.number().int().min(0, t("remoteAccess.validation.range", { min: 0, max: 10000 })).max(10000, t("remoteAccess.validation.range", { min: 0, max: 10000 })),
    protocols: z.array(z.enum(["ssh", "winrs"])).min(1, t("remoteAccess.validation.selectOne")).max(2),
    actions: z.array(z.literal("terminal")).length(1), source_cidrs: z.array(z.string()).max(64), time_windows: z.array(z.object({ day_of_week: z.number(), start: z.string(), end: z.string(), timezone: z.string() })).max(32),
    effects: z.array(z.enum(effectValues)).max(4),
    approval_workflow_id: z.string().optional(), session_profile_id: z.string().optional(), status: z.literal("draft"),
  }).superRefine((value, ctx) => {
    if (value.effects.includes("deny") && value.effects.length !== 1) ctx.addIssue({ code: "custom", path: ["effects"], message: t("remoteAccess.validation.selectOne") });
    if (value.effects.includes("require_approval") && !value.approval_workflow_id) ctx.addIssue({ code: "custom", path: ["approval_workflow_id"], message: t("remoteAccess.validation.approvalWorkflow") });
    if (value.effects.length === 0 && !value.session_profile_id) ctx.addIssue({ code: "custom", path: ["effects"], message: t("remoteAccess.validation.sessionProfile") });
  }), [t]);
  const validatedForm = useForm<RemoteAccessRuleWrite>({ resolver: zodResolver(schema), defaultValues: initial });
  useEffect(() => {
    if (!open) return;
    const value = editing ? { name: editing.name, description: editing.description, priority: editing.priority, protocols: editing.protocols, actions: editing.actions, source_cidrs: editing.source_cidrs, time_windows: editing.time_windows, effects: editing.effects, approval_workflow_id: editing.approval_workflow_id, session_profile_id: editing.session_profile_id, status: "draft" as const } : initial;
    setForm(value);
    validatedForm.reset(value);
  }, [editing, open, validatedForm]);
  const invalidate = async () => { await qc.invalidateQueries({ queryKey: ["remote-access", "rules"] }); };
  const save = useMutation({ mutationFn: () => { if (!editing) return api.remoteAccess.createRule(form); const { status: _status, ...input } = form; void _status; return api.remoteAccess.updateRule(editing.id, { ...input, expected_version: editing.version }); }, onSuccess: async () => { setOpen(false); setEditing(null); await invalidate(); } });
  const lifecycle = async (fn: (id: string) => Promise<unknown>, id: string) => { await fn(id); await invalidate(); };
  const set = <K extends keyof RemoteAccessRuleWrite>(key: K, value: RemoteAccessRuleWrite[K]) => setForm((current) => { const next = { ...current, [key]: value }; validatedForm.reset(next, { keepErrors: true }); return next; });
  const updateEffects = (effects: RemoteAccessRuleWrite["effects"]) => {
    const normalized = effects.includes("deny")
      ? form.effects.includes("deny") && effects.length > 1 ? effects.filter((effect) => effect !== "deny") : ["deny"] as RemoteAccessRuleWrite["effects"]
      : effects;
    setForm((current) => {
      const next = {
        ...current,
        effects: normalized,
        approval_workflow_id: normalized.includes("require_approval") ? current.approval_workflow_id : undefined,
        session_profile_id: normalized.includes("deny") ? undefined : current.session_profile_id,
      };
      validatedForm.reset(next, { keepErrors: true });
      return next;
    });
  };
  const items = query.data?.items ?? [];
  return <div className="argus-settings-section">
    <div className="argus-settings-section__head"><h2 className="argus-settings-section__title">{t("remoteAccess.rules")}</h2><Button onClick={() => { setEditing(null); setOpen(true); }} size="sm" variant="primary">{t("remoteAccess.newRule")}</Button></div>
    {items.length === 0 ? <EmptyState description="" title={t("remoteAccess.noRules")} /> : <GovernanceList extraColumns={[
      { key: "priority", header: t("remoteAccess.priority"), render: (row) => row.priority },
      { key: "effects", header: t("remoteAccess.effects"), render: (row) => row.effects.length > 0 ? row.effects.join(" · ") : t("remoteAccess.profileOnly") },
    ]} items={items} onArchive={(id) => lifecycle(api.remoteAccess.archiveRule, id)} onDisable={(id) => lifecycle(api.remoteAccess.disableRule, id)} onEdit={(item) => { setEditing(item); setOpen(true); }} onEnable={(id) => lifecycle(api.remoteAccess.enableRule, id)} onRestore={(id) => lifecycle(api.remoteAccess.restoreRule, id)} references={api.remoteAccess.getRuleReferences} />}
    <RuleSimulator />
    <FormDrawer description={t("remoteAccess.ruleDescription")} loading={save.isPending} onOpenChange={setOpen} onSubmit={validatedForm.handleSubmit(() => save.mutate())} open={open} submitLabel={t("remoteAccess.save")} title={editing ? t("remoteAccess.editRule") : t("remoteAccess.newRule")}>
      <ol className="argus-rule-steps"><li>{t("remoteAccess.ruleSteps.match")}</li><li>{t("remoteAccess.ruleSteps.effects")}</li><li>{t("remoteAccess.ruleSteps.workflow")}</li><li>{t("remoteAccess.ruleSteps.profile")}</li><li>{t("remoteAccess.ruleSteps.summary")}</li></ol>
      {save.isError && <Alert description={String(save.error)} title={t("settings.common.saveFailed")} tone="danger" />}
      <Field error={validatedForm.formState.errors.name?.message} label={t("remoteAccess.name")} requirement="required"><Input onChange={(event) => set("name", event.target.value)} value={form.name} /></Field>
      <Field label={t("remoteAccess.description")} requirement="optional"><Input onChange={(event) => set("description", event.target.value)} value={form.description} /></Field>
      <Field error={validatedForm.formState.errors.priority?.message} label={t("remoteAccess.priority")} requirement="required"><Input max={10000} min={0} onChange={(event) => set("priority", Number(event.target.value))} type="number" value={form.priority} /></Field>
      <Field controlMode="group" error={validatedForm.formState.errors.protocols?.message} label={t("remoteAccess.protocols")} requirement="required"><Checks label={(value) => value.toUpperCase()} onChange={(value) => set("protocols", value)} selected={form.protocols} values={["ssh", "winrs"] as const} /></Field>
      <Field label={t("remoteAccess.sourceCidrs")} requirement="optional"><Input onChange={(event) => set("source_cidrs", event.target.value.split(",").map((value) => value.trim()).filter(Boolean))} placeholder="10.0.0.0/8, 192.168.0.0/16" value={form.source_cidrs.join(", ")} /></Field>
      <Field controlMode="group" error={validatedForm.formState.errors.effects?.message} label={t("remoteAccess.effects")} requirement="optional"><Checks label={(value) => t(`remoteAccess.effect.${value}`)} onChange={updateEffects} selected={form.effects} values={effectValues} /></Field>
      {form.effects.includes("require_approval") && <Field error={validatedForm.formState.errors.approval_workflow_id?.message} label={t("remoteAccess.workflows")} requirement="required"><Select ariaLabel={t("remoteAccess.workflows")} onValueChange={(value) => set("approval_workflow_id", value || undefined)} options={(workflows.data?.items ?? []).filter((item) => item.status === "enabled").map((item) => ({ value: item.id, label: `${item.name} · v${item.version}` }))} value={form.approval_workflow_id ?? ""} /></Field>}
      {!form.effects.includes("deny") && <Field label={t("remoteAccess.sessionProfiles")} requirement="optional"><Select ariaLabel={t("remoteAccess.sessionProfiles")} onValueChange={(value) => set("session_profile_id", value || undefined)} options={(profiles.data?.items ?? []).filter((item) => item.status === "enabled").map((item) => ({ value: item.id, label: `${item.name} · v${item.version}` }))} value={form.session_profile_id ?? ""} /></Field>}
      <Alert description={t("remoteAccess.ruleSummary", { protocols: form.protocols.join(", "), effects: form.effects.join(", ") || t("remoteAccess.profileOnly"), priority: form.priority })} title={t("remoteAccess.ruleSteps.summary")} tone={form.effects.includes("deny") ? "warning" : "info"} />
    </FormDrawer>
  </div>;
}
