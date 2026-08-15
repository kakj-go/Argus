import { create } from "zustand";

type UiState = {
  sidebarCollapsed: boolean;
  mobileNavOpen: boolean;
  commandOpen: boolean;
  /** Chatbox 右侧上下文面板（会话页）是否展开。 */
  contextPanelOpen: boolean;
  /**
   * 会话标题的本地覆盖（conversationId -> title）。
   * API 暂无会话重命名端点，先在前端保存，后续由 conversations.update 持久化。
   */
  conversationTitles: Record<string, string>;
  toggleSidebar: () => void;
  setMobileNavOpen: (open: boolean) => void;
  setCommandOpen: (open: boolean) => void;
  toggleContextPanel: () => void;
  renameConversation: (id: string, title: string) => void;
};

export const useUiStore = create<UiState>((set) => ({
  sidebarCollapsed: false,
  mobileNavOpen: false,
  commandOpen: false,
  contextPanelOpen: true,
  conversationTitles: {},
  toggleSidebar: () =>
    set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
  setMobileNavOpen: (mobileNavOpen) => set({ mobileNavOpen }),
  setCommandOpen: (commandOpen) => set({ commandOpen }),
  toggleContextPanel: () =>
    set((state) => ({ contextPanelOpen: !state.contextPanelOpen })),
  renameConversation: (id, title) =>
    set((state) => ({
      conversationTitles: { ...state.conversationTitles, [id]: title },
    })),
}));
