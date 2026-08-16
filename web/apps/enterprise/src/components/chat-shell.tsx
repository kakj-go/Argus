import {
  Link,
  Outlet,
  useNavigate,
  useRouterState,
  useSearch,
} from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { House, Menu, MessageSquarePlus, Settings2, X } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Conversation } from "@argus/api-client/provisional";
import { useApi } from "@argus/api-client";
import { AppShell, Button, SearchInput } from "@argus/ui";
import { useUiStore } from "../store/ui";
import { AccountActions } from "./account-actions";

function Brand() {
  return (
    <Link className="argus-brand" to="/">
      <span className="argus-brand__mark">◉</span>
      <span className="argus-brand__name">
        Argus<small>enterprise</small>
      </span>
    </Link>
  );
}

type ConversationGroup = {
  key: "today" | "yesterday" | "last7Days" | "earlier";
  items: Conversation[];
};

/** 按最后更新时间分组：今天 / 昨天 / 最近 7 天 / 更早。 */
function groupConversations(items: Conversation[]): ConversationGroup[] {
  const groups: ConversationGroup[] = [
    { key: "today", items: [] },
    { key: "yesterday", items: [] },
    { key: "last7Days", items: [] },
    { key: "earlier", items: [] },
  ];
  const startOfToday = new Date();
  startOfToday.setHours(0, 0, 0, 0);
  const today = startOfToday.getTime();
  const day = 86_400_000;
  const sorted = [...items].sort(
    (a, b) => Date.parse(b.lastMessageAt) - Date.parse(a.lastMessageAt),
  );
  for (const item of sorted) {
    const at = Date.parse(item.lastMessageAt);
    if (at >= today) groups[0]!.items.push(item);
    else if (at >= today - day) groups[1]!.items.push(item);
    else if (at >= today - 6 * day) groups[2]!.items.push(item);
    else groups[3]!.items.push(item);
  }
  return groups.filter((group) => group.items.length > 0);
}

function formatTime(value: string, locale: string): string {
  const date = new Date(value);
  const startOfToday = new Date();
  startOfToday.setHours(0, 0, 0, 0);
  if (date.getTime() >= startOfToday.getTime()) {
    return date.toLocaleTimeString(locale, {
      hour: "2-digit",
      minute: "2-digit",
    });
  }
  return date.toLocaleDateString(locale, {
    month: "numeric",
    day: "numeric",
  });
}

function ConversationList() {
  const { t, i18n } = useTranslation();
  const api = useApi();
  const navigate = useNavigate();
  const setMobileNavOpen = useUiStore((state) => state.setMobileNavOpen);
  const search = useSearch({ strict: false }) as { c?: string };
  const [keyword, setKeyword] = useState("");
  const { data } = useQuery({
    queryKey: ["conversations"],
    queryFn: () => api.conversations.list(),
  });

  const groups = useMemo(() => {
    const items = data?.items ?? [];
    const trimmed = keyword.trim().toLowerCase();
    const filtered = trimmed
      ? items.filter((item) => item.title.toLowerCase().includes(trimmed))
      : items;
    return groupConversations(filtered);
  }, [data, keyword]);

  const openConversation = (id?: string) => {
    void navigate({ to: "/", search: id ? { c: id } : {} });
    setMobileNavOpen(false);
  };

  return (
    <>
      <div className="argus-sidebar__action">
        <Button
          className="argus-chat-new-button"
          onClick={() => openConversation()}
          variant="primary"
        >
          <MessageSquarePlus size={16} />
          {t("shell.chat.new")}
        </Button>
        <SearchInput
          aria-label={t("shell.chat.searchPlaceholder")}
          onChange={(event) => setKeyword(event.target.value)}
          placeholder={t("shell.chat.searchPlaceholder")}
          value={keyword}
        />
      </div>
      <div className="argus-chat-history">
        {data && data.items.length === 0 && (
          <p className="argus-chat-history__empty">{t("shell.chat.empty")}</p>
        )}
        {data && data.items.length > 0 && groups.length === 0 && (
          <p className="argus-chat-history__empty">{t("shell.chat.noMatch")}</p>
        )}
        {groups.map((group) => (
          <div className="argus-chat-history__group" key={group.key}>
            <div className="argus-nav-section__label">
              {t(`shell.chat.group.${group.key}`)}
            </div>
            <div className="argus-sessions">
              {group.items.map((item) => (
                <button
                  className={item.id === search.c ? "is-active" : ""}
                  key={item.id}
                  onClick={() => openConversation(item.id)}
                  type="button"
                >
                  <span>{item.title}</span>
                  <small>{formatTime(item.lastMessageAt, i18n.language)}</small>
                </button>
              ))}
            </div>
          </div>
        ))}
      </div>
    </>
  );
}

function Sidebar() {
  const { t } = useTranslation();
  const { mobileNavOpen, setMobileNavOpen } = useUiStore();
  return (
    <>
      <div
        aria-hidden
        className={`argus-mobile-scrim ${mobileNavOpen ? "is-visible" : ""}`}
        onClick={() => setMobileNavOpen(false)}
      />
      <aside
        className={`argus-sidebar ${mobileNavOpen ? "is-mobile-open" : ""}`}
      >
        <div className="argus-sidebar__head">
          <Brand />
          <Button
            aria-label={t("shell.closeNavigation")}
            className="argus-mobile-close"
            onClick={() => setMobileNavOpen(false)}
            size="icon"
            variant="ghost"
          >
            <X size={17} />
          </Button>
        </div>
        <ConversationList />
        <div className="argus-sidebar__footer">
          <Link className="argus-chat-admin-link" to="/hosts">
            <Settings2 size={15} />
            <span>{t("shell.enterAdmin")}</span>
          </Link>
        </div>
      </aside>
    </>
  );
}

function Header() {
  const { t } = useTranslation();
  const setMobileNavOpen = useUiStore((state) => state.setMobileNavOpen);
  return (
    <header className="argus-topbar">
      <Button
        aria-label={t("shell.openNavigation")}
        className="argus-mobile-menu"
        onClick={() => setMobileNavOpen(true)}
        size="icon"
        variant="ghost"
      >
        <Menu size={18} />
      </Button>
      <div className="argus-topbar__title">
        <span>{t("shell.nav.chat")}</span>
      </div>
      <div className="argus-topbar__actions">
        <AccountActions />
      </div>
    </header>
  );
}

function MobileNavigation() {
  const { t } = useTranslation();
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  });
  const items = [
    { key: "shell.nav.conversation", to: "/", icon: House },
    { key: "shell.nav.management", to: "/hosts", icon: Settings2 },
  ];
  return (
    <nav className="argus-mobile-primary">
      {items.map((item) => (
        <Link
          className={pathname === item.to ? "is-active" : ""}
          key={item.key}
          to={item.to}
        >
          <item.icon size={18} />
          <span>{t(item.key)}</span>
        </Link>
      ))}
    </nav>
  );
}

/**
 * Chatbox 布局（docs/07）：左侧会话导航 + 右侧会话工作区。
 * 会话工作区（`/`）由后续迭代实现，当前为占位页。
 */
export function ChatShell() {
  return (
    <AppShell
      className="argus-chat-shell"
      header={<Header />}
      overlay={<MobileNavigation />}
      sidebar={<Sidebar />}
    >
      <Outlet />
    </AppShell>
  );
}
