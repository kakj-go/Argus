import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { LayoutTemplate, PanelRightClose, ShieldCheck } from "lucide-react";
import { useApi } from "@argus/api-client";
import { Badge, Button, Timeline } from "@argus/ui";
import { useMyPermissions, useMyRoles } from "../../lib/permissions";
import { useUiStore } from "../../store/ui";
import type { ChatMessage } from "./chat-view-model";

function formatRelative(value: string, locale: string): string {
  const diff = Date.now() - Date.parse(value);
  const minutes = Math.max(0, Math.round(diff / 60_000));
  if (minutes < 1) return locale === "en-US" ? "just now" : "刚刚";
  if (minutes < 60)
    return locale === "en-US" ? `${minutes}m ago` : `${minutes} 分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24)
    return locale === "en-US" ? `${hours}h ago` : `${hours} 小时前`;
  const days = Math.floor(hours / 24);
  return locale === "en-US" ? `${days}d ago` : `${days} 天前`;
}

/**
 * 会话页右侧上下文面板（可收起，开关状态在 ui store）：
 * 当前会话引用的资源/卡片、当前用户权限摘要、当前用户最近 5 条审计动作。
 */
export function ChatContextPanel({ messages }: { messages: ChatMessage[] }) {
  const { t, i18n } = useTranslation();
  const api = useApi();
  const toggleContextPanel = useUiStore((state) => state.toggleContextPanel);

  // 当前会话引用的卡片资源（来自消息中的卡片引用，按卡片实例去重）。
  const references = useMemo(() => {
    const seen = new Map<
      string,
      { id: string; title: string; version: string }
    >();
    for (const message of messages) {
      for (const card of message.cards ?? []) {
        if (!seen.has(card.id)) {
          seen.set(card.id, {
            id: card.id,
            title: card.title ?? card.interactiveCardId,
            version: card.version,
          });
        }
      }
    }
    return [...seen.values()];
  }, [messages]);

  const { data: me } = useQuery({
    queryKey: ["auth", "me"],
    queryFn: () => api.auth.me(),
  });
  // 与 useMyPermissions 相同的绑定解析（user 主体 ∪ 本部门 department 主体）。
  const myRoles = useMyRoles();
  const permissionCount = useMyPermissions().size;

  const { data: auditPage } = useQuery({
    queryKey: ["audit", "mine", me?.user.id],
    queryFn: () =>
      api.audit.list({ actorUserId: me?.user.id }, { page: { limit: 5 } }),
    enabled: Boolean(me?.user.id),
  });

  return (
    <aside className="argus-chat-context" data-testid="chat-context-panel">
      <div className="argus-chat-context__head">
        <span>{t("chat.context.title")}</span>
        <Button
          aria-label={t("chat.context.collapse")}
          onClick={toggleContextPanel}
          size="icon"
          variant="ghost"
        >
          <PanelRightClose size={15} />
        </Button>
      </div>

      <section className="argus-chat-context__section">
        <span className="argus-chat-context__label">
          {t("chat.context.references")}
        </span>
        {references.length === 0 && (
          <p className="argus-chat-context__empty">
            {t("chat.context.emptyReferences")}
          </p>
        )}
        {references.map((ref) => (
          <div className="argus-chat-context__ref" key={ref.id}>
            <LayoutTemplate aria-hidden size={13} />
            <span>{ref.title}</span>
            <small>v{ref.version}</small>
          </div>
        ))}
      </section>

      <section className="argus-chat-context__section">
        <span className="argus-chat-context__label">
          {t("chat.context.permissions")}
        </span>
        <div className="argus-chat-context__roles">
          {myRoles.length === 0 && (
            <p className="argus-chat-context__empty">
              {t("chat.context.noRoles")}
            </p>
          )}
          {myRoles.map((role) => (
            <Badge key={role.id} tone="accent">
              <ShieldCheck aria-hidden size={11} />
              {role.name}
            </Badge>
          ))}
        </div>
        {myRoles.length > 0 && (
          <p className="argus-chat-context__empty">
            {t("chat.context.permissionsCount", { count: permissionCount })}
          </p>
        )}
      </section>

      <section className="argus-chat-context__section">
        <span className="argus-chat-context__label">
          {t("chat.context.recentActions")}
        </span>
        {auditPage && auditPage.items.length === 0 && (
          <p className="argus-chat-context__empty">
            {t("chat.context.emptyActions")}
          </p>
        )}
        {auditPage && auditPage.items.length > 0 && (
          <Timeline
            items={auditPage.items.map((event) => ({
              title: event.summary,
              meta: `${event.action} · ${formatRelative(event.createdAt, i18n.language)}`,
              status: event.result === "success" ? "done" : "danger",
            }))}
          />
        )}
      </section>
    </aside>
  );
}
