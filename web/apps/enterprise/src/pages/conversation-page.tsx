import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Archive, Check, PanelRight, Pencil, X } from "lucide-react";
import type {
  AIModel,
  InteractiveCardCreateCommand,
  ModelAvailability,
} from "@argus/api-client";
import { useApi } from "@argus/api-client";
import { Button, Input, Select } from "@argus/ui";
import { ChatComposer } from "../components/chat/composer";
import { ChatContextPanel } from "../components/chat/context-panel";
import { ChatMessageList } from "../components/chat/message-list";
import { useChatStream } from "../components/chat/use-chat-stream";
import { useUiStore } from "../store/ui";
import "../styles/chat.css";

const EXAMPLE_KEYS = [
  "hostCreate",
  "hostOverview",
  "addHost",
  "restarts",
] as const;

/** 欢迎空态：产品简介 + 示例问题快捷按钮（点击直接发送）。 */
function Welcome({
  onExample,
}: {
  onExample: (text: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="argus-chat-welcome">
      <span className="argus-chat-welcome__mark">◉</span>
      <h1 className="argus-chat-welcome__title">{t("chat.welcome.title")}</h1>
      <p className="argus-chat-welcome__desc">{t("chat.welcome.description")}</p>
      <div className="argus-chat-welcome__examples">
        {EXAMPLE_KEYS.map((key) => (
          <button
            className="argus-chat-welcome__example"
            key={key}
            onClick={() => onExample(t(`chat.welcome.examples.${key}`))}
            type="button"
          >
            {t(`chat.welcome.examples.${key}`)}
          </button>
        ))}
      </div>
    </div>
  );
}

/** 会话头部：标题（点击重命名，内联 input）、归档、上下文面板开关。 */
function ConversationHeader({
  conversationId,
  title,
  selectedModelId,
  models,
  availability,
  onModelChange,
}: {
  conversationId: string;
  title: string;
  selectedModelId: string;
  models: AIModel[];
  availability: ModelAvailability[];
  onModelChange: (modelId: string) => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const {
    contextPanelOpen,
    toggleContextPanel,
    renameConversation,
  } = useUiStore();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(title);
  const [archiving, setArchiving] = useState(false);

  const saveTitle = () => {
    const next = draft.trim();
    if (next) renameConversation(conversationId, next);
    setEditing(false);
  };

  const archive = async () => {
    setArchiving(true);
    try {
      await api.conversations.archive(conversationId);
      await queryClient.invalidateQueries({ queryKey: ["conversations"] });
      void navigate({ to: "/", search: {} });
    } finally {
      setArchiving(false);
    }
  };

  return (
    <header className="argus-chat__header">
      {editing ? (
        <div className="argus-chat__title-edit">
          <Input
            aria-label={t("chat.header.rename")}
            autoFocus
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") saveTitle();
              if (event.key === "Escape") setEditing(false);
            }}
            placeholder={t("chat.header.renamePlaceholder")}
            value={draft}
          />
          <Button
            aria-label={t("chat.header.save")}
            onClick={saveTitle}
            size="icon"
            variant="ghost"
          >
            <Check size={15} />
          </Button>
          <Button
            aria-label={t("chat.header.cancel")}
            onClick={() => setEditing(false)}
            size="icon"
            variant="ghost"
          >
            <X size={15} />
          </Button>
        </div>
      ) : (
        <div className="argus-chat__title">
          <span>{title}</span>
          <Button
            aria-label={t("chat.header.rename")}
            onClick={() => {
              setDraft(title);
              setEditing(true);
            }}
            size="icon"
            variant="ghost"
          >
            <Pencil size={13} />
          </Button>
        </div>
      )}
      <div className="argus-chat__header-actions">
        <Select
          ariaLabel={t("chat.model.select")}
          className="argus-input argus-chat__model-select"
          onValueChange={onModelChange}
          options={models.map((model) => {
            const state = availability.find((entry) => entry.modelId === model.id);
            return {
              value: model.id,
              disabled: !state?.available,
              label: `${model.name}${
                state?.available
                  ? ""
                  : ` · ${t(`chat.model.reason.${state?.reason ?? "unavailable"}`)}`
              }`,
            };
          })}
          value={selectedModelId}
        />
        <Button
          aria-label={t("chat.header.archive")}
          loading={archiving}
          onClick={archive}
          size="icon"
          variant="ghost"
        >
          <Archive size={15} />
        </Button>
        <Button
          aria-label={t("chat.header.toggleContext")}
          className={contextPanelOpen ? "is-active" : ""}
          onClick={toggleContextPanel}
          size="icon"
          variant="ghost"
        >
          <PanelRight size={15} />
        </Button>
      </div>
    </header>
  );
}

/**
 * Chatbox 会话页（路由 `/`，ChatShell 布局内）：
 * 读取 `?c=` 选中会话；无会话时显示欢迎空态。
 */
export function ConversationPage() {
  const { t } = useTranslation();
  const api = useApi();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const search = useSearch({ strict: false }) as { c?: string };
  const conversationId = search.c;
  const contextPanelOpen = useUiStore((state) => state.contextPanelOpen);
  const titleOverride = useUiStore((state) =>
    conversationId ? state.conversationTitles[conversationId] : undefined,
  );
  const { streaming, pendingUser, sending, error, send, stop } =
    useChatStream();

  const { data: conversation } = useQuery({
    queryKey: ["conversations", "detail", conversationId],
    queryFn: () => api.conversations.get(conversationId ?? ""),
    enabled: Boolean(conversationId),
  });
  const { data: messages } = useQuery({
    queryKey: ["conversations", conversationId, "messages"],
    queryFn: () => api.conversations.listMessages(conversationId ?? ""),
    enabled: Boolean(conversationId),
  });
  const { data: models = [] } = useQuery({
    queryKey: ["models", "chat"],
    queryFn: () => api.models.list(),
  });
  const { data: availability = [] } = useQuery({
    queryKey: ["models", "availability"],
    queryFn: () => api.models.listAvailability(),
  });
  const availableModels = models.filter(
    (model) =>
      availability.find((entry) => entry.modelId === model.id)?.available,
  );
  const preferredModelId =
    conversation?.selectedModelId ??
    window.localStorage.getItem("argus.lastModelId") ??
    availableModels[0]?.id;

  // 会话推送事件（如 card_action_result，docs/04 §9）到达后刷新消息列表。
  useEffect(() => {
    if (!conversationId) return;
    return api.conversations.subscribe(conversationId, () => {
      void queryClient.invalidateQueries({
        queryKey: ["conversations", conversationId, "messages"],
      });
    });
  }, [api, conversationId, queryClient]);

  const handleSend = (text: string, command?: InteractiveCardCreateCommand) => {
    void (async () => {
      let id = conversationId;
      if (!id) {
        // 欢迎空态下直接发送：先建会话（首条消息截断为标题）再跳转。
        const created = await api.conversations.create({
          title: text.length > 30 ? `${text.slice(0, 30)}…` : text,
          selectedModelId: preferredModelId,
        });
        await queryClient.invalidateQueries({ queryKey: ["conversations"] });
        void navigate({ to: "/", search: { c: created.id } });
        id = created.id;
      }
      await send(id, text, command);
    })();
  };

  const changeModel = async (modelId: string) => {
    window.localStorage.setItem("argus.lastModelId", modelId);
    if (!conversationId) return;
    await api.conversations.updateModel(conversationId, modelId);
    await queryClient.invalidateQueries({
      queryKey: ["conversations", "detail", conversationId],
    });
  };

  const title =
    titleOverride ?? conversation?.title ?? t("chat.header.untitled");

  return (
    <div
      className={`argus-chat ${contextPanelOpen ? "" : "is-context-collapsed"}`}
    >
      <div className="argus-chat__main">
        {conversationId && (
          <ConversationHeader
            availability={availability}
            conversationId={conversationId}
            models={models}
            onModelChange={(modelId) => void changeModel(modelId)}
            selectedModelId={conversation?.selectedModelId ?? preferredModelId ?? ""}
            title={title}
          />
        )}
        {conversationId ? (
          <ChatMessageList
            messages={messages ?? []}
            pendingUser={pendingUser}
            streaming={streaming}
          />
        ) : (
          <Welcome onExample={(text) => handleSend(text)} />
        )}
        {error && (
          <div className="argus-chat-composer__note" role="alert">
            {t("chat.error.sendFailed")}
          </div>
        )}
        <ChatComposer
          disabled={
            !preferredModelId ||
            !availability.find((entry) => entry.modelId === preferredModelId)
              ?.available
          }
          onSend={handleSend}
          onStop={stop}
          sending={sending}
        />
      </div>
      {contextPanelOpen && <ChatContextPanel messages={messages ?? []} />}
    </div>
  );
}
