import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { useApi } from "@argus/api-client";
import {
  SandboxCard,
  type ActionInvokeHandler,
  type QueryInvokeHandler,
} from "@argus/card-host";
import { Badge, useTheme } from "@argus/ui";
import { cardOrigin, provisionalCardContract } from "../../lib/card-contract";
import type { CardInstance } from "./chat-view-model";

/**
 * 消息中的沙箱卡片引用：统一包装（标题栏 + 来源徽标 + 边框）后
 * 用 SandboxCard 渲染 InteractiveCard.htmlTemplate。
 */
export function SandboxCardFrame({ card }: { card: CardInstance }) {
  const { t, i18n } = useTranslation();
  const { resolvedTheme } = useTheme();
  const locale = i18n.language === "en-US" ? "en-US" : "zh-CN";
  const api = useApi();
  const { data: skill } = useQuery({
    queryKey: ["interactiveCards", card.interactiveCardId],
    queryFn: () => api.interactiveCards.get(card.interactiveCardId),
  });

  // mock 侧没有按 queryBindingId 注册的查询处理器（翻页/刷新）；
  // 这里回传卡片当前数据作为占位，接入真实后端后按 binding 路由到对应查询。
  const handleQueryInvoke: QueryInvokeHandler = () => skill?.demoData ?? {};

  // 卡片内 Action Slot：若绑定的是 PendingActionPublic 的 actionBindingId，
  // 触发确认流程（等价于确认卡片的 [确认执行]，不经过模型）。
  const handleActionInvoke: ActionInvokeHandler = async (bindingId) => {
    if (card.pendingActionRef && card.actionBindingId === bindingId) {
      const result = await api.approvals.confirm(card.pendingActionRef);
      return {
        status: result.pending_action.status,
        execution_ref: result.execution?.execution_id,
      };
    }
    return { ignored: true };
  };

  const title = card.title ?? skill?.name ?? t("chat.card.untitled");
  const contract = skill
    ? provisionalCardContract({
        card_id: skill.id,
        revision: skill.revision,
        card_instance_id: card.id,
        action_binding_ids: card.actionBindingId ? [card.actionBindingId] : [],
        locale,
        color_scheme: resolvedTheme,
      })
    : null;

  return (
    <div className="argus-chat-card" data-testid="sandbox-card-frame">
      <header className="argus-chat-card__head">
        <span>{title}</span>
        <small>v{card.version}</small>
        <Badge tone={skill?.source === "system" ? "info" : "accent"}>
          {skill?.source === "system"
            ? t("chat.card.system")
            : t("chat.card.aiGenerated")}
        </Badge>
      </header>
      <div className="argus-chat-card__body">
        {skill && contract ? (
          <SandboxCard
            bindings={{
              query_binding_ids: [],
              action_binding_ids: card.actionBindingId
                ? [card.actionBindingId]
                : [],
            }}
            card_origin={cardOrigin}
            color_scheme={resolvedTheme}
            html={skill.htmlTemplate}
            initial_data={skill.demoData}
            locale={locale}
            manifest={contract.manifest}
            onActionInvoke={handleActionInvoke}
            onQueryInvoke={handleQueryInvoke}
            render_plan={contract.render_plan}
            title={title}
          />
        ) : (
          <div className="argus-chat-card__loading">
            {t("chat.card.loading")}
          </div>
        )}
      </div>
    </div>
  );
}
