import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { ChevronDown, ChevronUp, Link2, LockKeyhole } from "lucide-react";
import {
  useApi,
  type InteractiveCard,
  type SlotBinding,
  type SlotBindingMode,
} from "@argus/api-client";
import { SandboxCard } from "@argus/card-host";
import {
  Badge,
  Button,
  EmptyState,
  Field,
  FormDrawer,
  PageShell,
  Select,
  StatusBadge,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@argus/ui";
import "../styles/ai-settings.css";
import { usePermission } from "../lib/permissions";

export function SettingsInteractiveCardsPage() {
  const { t } = useTranslation();
  const api = useApi();
  const admin = usePermission("interactive_card.create");
  const [tab, setTab] = useState<"enterprise" | "system">("enterprise");
  const [detail, setDetail] = useState<InteractiveCard | null>(null);
  const cards = useQuery({
    queryKey: ["interactiveCards"],
    queryFn: () => api.interactiveCards.list(),
  });
  const enterprise = (cards.data ?? []).filter((card) => card.source === "enterprise");
  const system = (cards.data ?? []).filter((card) => card.source === "system");
  return (
    <PageShell description={t("aiSettings.cards.description")} title={t("shell.nav.settingsInteractiveCards")}>
      <Tabs onValueChange={(value) => setTab(value as typeof tab)} value={tab}>
        <TabsList>
          <TabsTrigger value="enterprise">{t("aiSettings.cards.custom")}</TabsTrigger>
          <TabsTrigger value="system">{t("aiSettings.cards.system")}</TabsTrigger>
        </TabsList>
        <TabsContent value="enterprise">
          <CardRows cards={enterprise} editable={admin} onOpen={setDetail} />
        </TabsContent>
        <TabsContent value="system">
          <CardRows cards={system} editable={false} onOpen={setDetail} />
        </TabsContent>
      </Tabs>
      {detail && <CardDetailDrawer card={detail} editable={admin && detail.source === "enterprise"} onClose={() => setDetail(null)} />}
    </PageShell>
  );
}

function CardRows({ cards, editable, onOpen }: { cards: InteractiveCard[]; editable: boolean; onOpen: (card: InteractiveCard) => void }) {
  const { t } = useTranslation();
  if (!cards.length) return <EmptyState description="" title={t("aiSettings.cards.noCards")} />;
  return (
    <div className="argus-ic-list">
      {cards.map((card) => (
        <article className="argus-ic-row" key={card.id}>
          <div className="argus-ic-row__meta">
            <div><b>{card.name}</b><Badge>{card.version}</Badge></div>
            <p>{card.description}</p>
            <span><StatusBadge tone={card.enabled ? "success" : "neutral"}>{t(`aiSettings.cards.${card.lifecycle}`)}</StatusBadge> · {card.slug} · rev {card.revision}</span>
          </div>
          <LazyCardPreview card={card} />
          <div className="argus-ic-row__actions">
            {!editable && card.source === "system" && <LockKeyhole aria-label={t("aiSettings.cards.readonly")} size={15} />}
            <Button onClick={() => onOpen(card)} size="sm" variant="secondary">
              {editable ? t("aiSettings.cards.detail") : t("aiSettings.cards.preview")}
            </Button>
          </div>
        </article>
      ))}
    </div>
  );
}

function LazyCardPreview({ card }: { card: InteractiveCard }) {
  const { t } = useTranslation();
  const hostRef = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(false);
  const [expanded, setExpanded] = useState(false);
  useEffect(() => {
    const node = hostRef.current;
    if (!node) return;
    const observer = new IntersectionObserver(([entry]) => {
      if (entry?.isIntersecting) {
        setVisible(true);
        observer.disconnect();
      }
    }, { rootMargin: "200px" });
    observer.observe(node);
    return () => observer.disconnect();
  }, []);
  return (
    <div className={`argus-ic-preview ${expanded ? "is-expanded" : ""}`} ref={hostRef}>
      {visible && <SandboxCard cardInstanceId={`preview-${card.id}`} html={card.htmlTemplate} initialData={card.demoData} maxHeight={expanded ? 1200 : 320} minHeight={120} title={card.name} />}
      <Button className="argus-ic-preview__expand" onClick={() => setExpanded((value) => !value)} size="sm" variant="ghost">
        {expanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
        {expanded ? t("aiSettings.cards.collapse") : t("aiSettings.cards.expand")}
      </Button>
    </div>
  );
}

function CardDetailDrawer({ card: initial, editable, onClose }: { card: InteractiveCard; editable: boolean; onClose: () => void }) {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const [card, setCard] = useState(initial);
  const [slotName, setSlotName] = useState<string | null>(null);
  const refresh = async () => {
    const next = await api.interactiveCards.get(card.id);
    setCard(next);
    await queryClient.invalidateQueries({ queryKey: ["interactiveCards"] });
  };
  const validate = useMutation({ mutationFn: () => api.interactiveCards.validate(card.id), onSuccess: () => void refresh() });
  const toggle = useMutation({
    mutationFn: () => card.enabled ? api.interactiveCards.disable(card.id) : api.interactiveCards.enable(card.id),
    onSuccess: (next) => { setCard(next); void queryClient.invalidateQueries({ queryKey: ["interactiveCards"] }); },
  });
  return (
    <>
      <FormDrawer
        footer={
          editable ? (
            <>
              <Button loading={validate.isPending} onClick={() => validate.mutate()} variant="secondary">{t("aiSettings.cards.validate")}</Button>
              <Button disabled={!card.enabled && !card.validation?.valid} loading={toggle.isPending} onClick={() => toggle.mutate()} variant="primary">{card.enabled ? t("aiSettings.cards.disable") : t("aiSettings.cards.enable")}</Button>
            </>
          ) : <Badge><LockKeyhole size={12} />{t("aiSettings.cards.readonly")}</Badge>
        }
        onOpenChange={(open) => !open && onClose()}
        open
        title={card.name}
        width={760}
      >
        <div className="argus-ic-detail">
          <LazyCardPreview card={card} />
          <div className="argus-ic-validation">
            <StatusBadge tone={card.validation?.valid ? "success" : "warning"}>
              {card.validation?.valid ? t("aiSettings.cards.validationPassed") : t("aiSettings.cards.validationFailed")}
            </StatusBadge>
            {(card.validation?.issues ?? []).map((issue) => <small key={`${issue.code}-${issue.slot ?? ""}`}>{issue.code}: {issue.slot ?? issue.message}</small>)}
          </div>
          <section>
            <h3>Slots</h3>
            <div className="argus-ic-slots">
              {card.slots.map((slot) => (
                <button className={slotName === slot.name ? "is-active" : ""} disabled={!editable || slot.aiGenerated} key={slot.name} onClick={() => setSlotName(slot.name)} type="button">
                  <Link2 size={13} /><span>{slot.name}</span><small>{slot.type}{slot.required ? " · required" : ""}</small>
                </button>
              ))}
            </div>
          </section>
        </div>
      </FormDrawer>
      {slotName && <BindingDrawer card={card} onClose={() => setSlotName(null)} onSaved={() => { setSlotName(null); void refresh(); }} slotName={slotName} />}
    </>
  );
}

function BindingDrawer({ card, slotName, onClose, onSaved }: { card: InteractiveCard; slotName: string; onClose: () => void; onSaved: () => void }) {
  const { t } = useTranslation();
  const api = useApi();
  const schemas = useQuery({ queryKey: ["tool-schemas"], queryFn: () => api.interactiveCards.listToolSchemas() });
  const existing = card.bindings.find((binding) => binding.slotName === slotName);
  const [mode, setMode] = useState<SlotBindingMode>(existing?.mode ?? "strict");
  const [toolName, setToolName] = useState(existing?.toolName ?? "");
  const currentSchema = schemas.data?.find((schema) => schema.toolName === toolName) ?? schemas.data?.[0];
  const [fieldPath, setFieldPath] = useState(existing?.fieldPath ?? "");
  const save = useMutation({
    mutationFn: () => {
      const schema = currentSchema;
      if (!schema) throw new Error("schema required");
      const binding: SlotBinding = { slotName, mode, toolName: schema.toolName, schemaVersion: schema.version, fieldPath: fieldPath || schema.fields[0]?.path || "" };
      return api.interactiveCards.updateBindings(card.id, [...card.bindings.filter((entry) => entry.slotName !== slotName), binding]);
    },
    onSuccess: onSaved,
  });
  return (
    <FormDrawer loading={save.isPending} onOpenChange={(open) => !open && onClose()} onSubmit={() => save.mutate()} open submitLabel={t("aiSettings.cards.saveBinding")} title={`${t("aiSettings.cards.bindingTitle")} · ${slotName}`}>
      <div className="argus-settings-form">
        <Field label={t("aiSettings.cards.bindingMode")}><Select onValueChange={(value) => setMode(value as SlotBindingMode)} options={[{ value: "strict", label: "strict" }, { value: "preferred", label: "preferred" }]} value={mode} /></Field>
        <Field label={t("aiSettings.cards.tool")}><Select onValueChange={(value) => { setToolName(value); setFieldPath(""); }} options={(schemas.data ?? []).map((schema) => ({ value: schema.toolName, label: `${schema.toolName} · ${schema.version}` }))} value={currentSchema?.toolName ?? ""} /></Field>
        <Field label={t("aiSettings.cards.field")}><Select onValueChange={setFieldPath} options={(currentSchema?.fields ?? []).map((field) => ({ value: field.path, label: `${field.path} · ${field.type}` }))} value={fieldPath || currentSchema?.fields[0]?.path || ""} /></Field>
      </div>
    </FormDrawer>
  );
}
