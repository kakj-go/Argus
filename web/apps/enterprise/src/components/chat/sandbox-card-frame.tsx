import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { useApi } from "@argus/api-client";
import {
  SandboxCard,
  type ActionInvokeHandler,
  type QueryInvokeHandler,
} from "@argus/card-host";
import { Badge, useTheme } from "@argus/ui";
import { cardOrigin } from "../../lib/card-contract";
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
  const presentation = useQuery({
    queryKey: ["card-presentation", card.id, locale, resolvedTheme],
    queryFn: () =>
      api.interactiveCards.createPresentation(card.id, {
        locale,
        color_scheme: resolvedTheme,
      }),
    retry: false,
  });

  const handleQueryInvoke: QueryInvokeHandler = (bindingId) =>
    api.interactiveCards.invokeQueryBinding(bindingId);

  const handleActionInvoke: ActionInvokeHandler = (bindingId) =>
    api.interactiveCards.invokeActionBinding(bindingId);

  const value = presentation.data;
  const title = card.title ?? t("chat.card.untitled");

  return (
    <div className="argus-chat-card" data-testid="sandbox-card-frame">
      <header className="argus-chat-card__head">
        <span>{title}</span>
        <small>v{card.version}</small>
        <Badge tone={value?.manifest.source === "system" ? "info" : "accent"}>
          {value?.manifest.source === "system"
            ? t("chat.card.system")
            : t("chat.card.aiGenerated")}
        </Badge>
      </header>
      <div className="argus-chat-card__body">
        {value ? (
          <SandboxCard
            bindings={{
              query_binding_ids: Object.values(
                value.render_plan.query_binding_ids,
              ),
              action_binding_ids: Object.values(
                value.render_plan.action_binding_ids,
              ),
            }}
            card_origin={cardOrigin}
            color_scheme={resolvedTheme}
            html={value.entrypoint_html}
            initial_data={value.initial_data as Record<string, unknown>}
            locale={locale}
            manifest={value.manifest}
            onActionInvoke={handleActionInvoke}
            onQueryInvoke={handleQueryInvoke}
            render_plan={value.render_plan}
            title={title}
          />
        ) : presentation.isError ? (
          <div className="argus-chat-card__loading" role="alert">
            {t("chat.card.unavailable")}
          </div>
        ) : (
          <div className="argus-chat-card__loading">
            {t("chat.card.loading")}
          </div>
        )}
      </div>
    </div>
  );
}
