import { useRef, useState, type ChangeEvent, type KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowUp,
  AtSign,
  Cable,
  FilePlus2,
  FileText,
  Paperclip,
  Server,
  Square,
  X,
} from "lucide-react";
import { useApi } from "@argus/api-client";
import { Button, Tooltip } from "@argus/ui";
import { usePermission } from "../../lib/permissions";

const mockMode = import.meta.env.VITE_API_MODE === "mock";

type MentionChip = { kind: "host" | "connector"; id: string; label: string };
type PickerState = {
  kind: "mention" | "command";
  start: number;
  end: number;
  query: string;
};
type PickerItem = {
  id: string;
  label: string;
  detail?: string;
  kind: "host" | "connector" | "command";
};

function detectTrigger(value: string, caret: number) {
  const match = /(?:^|[\s\n])([@/])([^\s@/]*)$/.exec(value.slice(0, caret));
  if (!match) return null;
  const token = match[2] ?? "";
  return {
    trigger: match[1] ?? "@",
    start: caret - token.length - 1,
    end: caret,
    query: token,
  };
}

export function ChatComposer({
  sending,
  disabled,
  onSend,
  onStop,
}: {
  sending: boolean;
  disabled?: boolean;
  onSend: (text: string, mockIntent?: "interactive_card.create") => void;
  onStop: () => void;
}) {
  const { t } = useTranslation();
  const api = useApi();
  const canCreateCard = usePermission("interactive_card.create");
  const [text, setText] = useState("");
  const [mentions, setMentions] = useState<MentionChip[]>([]);
  const [files, setFiles] = useState<string[]>([]);
  const [picker, setPicker] = useState<PickerState | null>(null);
  const [mockIntent, setMockIntent] = useState<"interactive_card.create">();
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const hosts = useQuery({
    queryKey: ["hosts", "picker"],
    queryFn: () => api.hosts.list(),
    enabled: picker?.kind === "mention",
  });
  const connectors = useQuery({
    queryKey: ["connectors", "picker"],
    queryFn: () => api.connectors.list(),
    enabled: picker?.kind === "mention",
  });

  const pickerItems: PickerItem[] = (() => {
    if (!picker) return [];
    const match = (label: string) =>
      label.toLowerCase().includes(picker.query.toLowerCase());
    if (picker.kind === "command") {
      const label = t("chat.composer.createInteractiveCard");
      return mockMode && canCreateCard && match(label)
        ? [
            {
              id: "interactive_card.create",
              label,
              detail: t("chat.composer.createInteractiveCardHint"),
              kind: "command" as const,
            },
          ]
        : [];
    }
    return [
      ...(connectors.data?.items ?? [])
        .filter((item) => match(item.name))
        .map((item) => ({
          id: `connector:${item.id}`,
          label: item.name,
          detail: item.status,
          kind: "connector" as const,
        })),
      ...(hosts.data?.items ?? [])
        .filter((item) => match(item.name))
        .map((item) => ({
          id: `host:${item.id}`,
          label: item.name,
          detail: item.hostname,
          kind: "host" as const,
        })),
    ];
  })();

  const autosize = () => {
    const element = textareaRef.current;
    if (!element) return;
    element.style.height = "auto";
    element.style.height = `${Math.min(element.scrollHeight, 168)}px`;
  };
  const updateText = (value: string, caret: number) => {
    setText(value);
    const trigger = detectTrigger(value, caret);
    if (
      !trigger ||
      (trigger.trigger === "/" && (!mockMode || !canCreateCard))
    ) {
      setPicker(null);
      return;
    }
    setPicker({
      kind: trigger.trigger === "@" ? "mention" : "command",
      start: trigger.start,
      end: trigger.end,
      query: trigger.query,
    });
  };
  const selectItem = (item: PickerItem) => {
    if (!picker) return;
    if (item.kind === "command") {
      setMockIntent("interactive_card.create");
      setText(text.slice(0, picker.start) + text.slice(picker.end));
      setPicker(null);
      requestAnimationFrame(() => textareaRef.current?.focus());
      return;
    }
    const [, id] = item.id.split(":");
    const kind = item.kind === "host" ? "host" : "connector";
    setMentions((current) =>
      current.some((entry) => entry.id === item.id)
        ? current
        : [...current, { kind, id: id ?? item.id, label: item.label }],
    );
    setText(text.slice(0, picker.start) + text.slice(picker.end));
    setPicker(null);
    requestAnimationFrame(() => textareaRef.current?.focus());
  };
  const submit = () => {
    const value = text.trim();
    if (!value || sending || disabled) return;
    onSend(value, mockIntent);
    setText("");
    setMentions([]);
    setFiles([]);
    setMockIntent(undefined);
    setPicker(null);
    requestAnimationFrame(autosize);
  };
  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (picker && event.key === "Enter" && pickerItems[0]) {
      event.preventDefault();
      selectItem(pickerItems[0]);
      return;
    }
    if (event.key === "Escape") setPicker(null);
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      submit();
    }
  };
  const insertTrigger = (trigger: "@" | "/") => {
    if (trigger === "/" && (!mockMode || !canCreateCard)) return;
    const caret = textareaRef.current?.selectionStart ?? text.length;
    const next = `${text.slice(0, caret)}${trigger}${text.slice(caret)}`;
    updateText(next, caret + 1);
    requestAnimationFrame(() => {
      textareaRef.current?.focus();
      textareaRef.current?.setSelectionRange(caret + 1, caret + 1);
    });
  };
  const pickFiles = (event: ChangeEvent<HTMLInputElement>) => {
    setFiles((current) => [
      ...current,
      ...Array.from(event.target.files ?? []).map((file) => file.name),
    ]);
    event.target.value = "";
  };

  return (
    <div className="argus-chat-composer">
      <div className="argus-chat-composer__inner">
        {(mentions.length > 0 || files.length > 0 || mockIntent) && (
          <div className="argus-chat-composer__chips">
            {mockIntent && (
              <span className="argus-chat-chip">
                <FilePlus2 size={12} />/
                {t("chat.composer.createInteractiveCard")}
                <button
                  aria-label={t("chat.composer.removeCommand")}
                  onClick={() => setMockIntent(undefined)}
                  type="button"
                >
                  <X size={11} />
                </button>
              </span>
            )}
            {mentions.map((chip) => (
              <span className="argus-chat-chip" key={chip.id}>
                {chip.kind === "host" ? (
                  <Server size={12} />
                ) : (
                  <Cable size={12} />
                )}
                {chip.label}
                <button
                  aria-label={`remove ${chip.label}`}
                  onClick={() =>
                    setMentions((current) =>
                      current.filter((entry) => entry.id !== chip.id),
                    )
                  }
                  type="button"
                >
                  <X size={11} />
                </button>
              </span>
            ))}
            {files.map((name, index) => (
              <span className="argus-chat-chip" key={`${name}-${index}`}>
                <FileText size={12} />
                {name}
                <button
                  aria-label={`remove ${name}`}
                  onClick={() =>
                    setFiles((current) =>
                      current.filter((_, item) => item !== index),
                    )
                  }
                  type="button"
                >
                  <X size={11} />
                </button>
              </span>
            ))}
          </div>
        )}
        <div className="argus-chat-composer__box">
          {picker && (
            <div className="argus-chat-picker" role="listbox">
              <div className="argus-chat-picker__title">
                {picker.kind === "mention"
                  ? t("chat.composer.mentionTitle")
                  : t("chat.composer.commandTitle")}
              </div>
              <div className="argus-chat-picker__list">
                {pickerItems.length === 0 ? (
                  <div className="argus-chat-picker__empty">
                    {t("chat.composer.noMatch")}
                  </div>
                ) : (
                  pickerItems.map((item) => (
                    <button
                      className="argus-chat-picker__item"
                      key={item.id}
                      onClick={() => selectItem(item)}
                      role="option"
                      type="button"
                    >
                      {item.kind === "command" ? (
                        <FilePlus2 size={14} />
                      ) : item.kind === "host" ? (
                        <Server size={14} />
                      ) : (
                        <Cable size={14} />
                      )}
                      {item.label}
                      <small>{item.detail}</small>
                    </button>
                  ))
                )}
              </div>
            </div>
          )}
          <textarea
            aria-label={t("chat.composer.send")}
            disabled={disabled}
            onChange={(event) => {
              updateText(event.target.value, event.target.selectionStart ?? 0);
              autosize();
            }}
            onKeyDown={handleKeyDown}
            placeholder={
              disabled
                ? t("chat.composer.noModel")
                : t("chat.composer.placeholder")
            }
            ref={textareaRef}
            rows={2}
            value={text}
          />
          <div className="argus-chat-composer__toolbar">
            <input
              hidden
              multiple
              onChange={pickFiles}
              ref={fileInputRef}
              type="file"
            />
            <Tooltip content={t("chat.composer.attach")}>
              <Button
                aria-label={t("chat.composer.attach")}
                onClick={() => fileInputRef.current?.click()}
                size="icon"
                variant="ghost"
              >
                <Paperclip size={15} />
              </Button>
            </Tooltip>
            <Tooltip content={t("chat.composer.mention")}>
              <Button
                aria-label={t("chat.composer.mention")}
                onClick={() => insertTrigger("@")}
                size="icon"
                variant="ghost"
              >
                <AtSign size={15} />
              </Button>
            </Tooltip>
            {mockMode && canCreateCard && (
              <Tooltip content={t("chat.composer.createInteractiveCard")}>
                <Button
                  aria-label={t("chat.composer.createInteractiveCard")}
                  onClick={() => insertTrigger("/")}
                  size="icon"
                  variant="ghost"
                >
                  <FilePlus2 size={15} />
                </Button>
              </Tooltip>
            )}
            <span>{t("chat.composer.hint")}</span>
            {sending ? (
              <Button
                aria-label={t("chat.composer.stop")}
                onClick={onStop}
                size="icon"
                variant="secondary"
              >
                <Square size={13} />
              </Button>
            ) : (
              <Button
                aria-label={t("chat.composer.send")}
                disabled={disabled || !text.trim()}
                onClick={submit}
                size="icon"
                variant="primary"
              >
                <ArrowUp size={16} />
              </Button>
            )}
          </div>
        </div>
        <small className="argus-chat-composer__note">
          {t("chat.composer.disclaimer")}
        </small>
      </div>
    </div>
  );
}
