import { create } from "zustand";

export type TerminalDockPosition = "bottom" | "left" | "right";

export type TerminalDockPreference = {
  position: TerminalDockPosition;
  /** Dock 尺寸，相对主内容区对应维度的百分比（20-80）。 */
  sizePercent: number;
};

const terminalDockStorageKey = "argus.terminalDock";
const MIN_DOCK_PERCENT = 20;
const MAX_DOCK_PERCENT = 80;

export function clampDockPercent(value: number): number {
  if (!Number.isFinite(value)) return 50;
  return Math.max(MIN_DOCK_PERCENT, Math.min(MAX_DOCK_PERCENT, value));
}

function loadTerminalDockPreference(): TerminalDockPreference {
  try {
    const raw = window.localStorage.getItem(terminalDockStorageKey);
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<TerminalDockPreference>;
      if (
        (parsed.position === "bottom" ||
          parsed.position === "left" ||
          parsed.position === "right") &&
        typeof parsed.sizePercent === "number"
      ) {
        return {
          position: parsed.position,
          sizePercent: clampDockPercent(parsed.sizePercent),
        };
      }
    }
  } catch {
    // Corrupt preference falls back to the default placement.
  }
  return { position: "bottom", sizePercent: 50 };
}

function persistTerminalDockPreference(value: TerminalDockPreference) {
  try {
    window.localStorage.setItem(
      terminalDockStorageKey,
      JSON.stringify(value),
    );
  } catch {
    // Persistence is best-effort; the in-memory placement still applies.
  }
}

type UiState = {
  sidebarCollapsed: boolean;
  mobileNavOpen: boolean;
  commandOpen: boolean;
  /** Chatbox 右侧上下文面板（会话页）是否展开。 */
  contextPanelOpen: boolean;
  /** 远程终端 Dock 的停靠位置与尺寸，持久化在 localStorage。 */
  terminalDock: TerminalDockPreference;
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
  setTerminalDockPosition: (position: TerminalDockPosition) => void;
  setTerminalDockSize: (sizePercent: number) => void;
  resetTerminalDockSize: () => void;
};

export const useUiStore = create<UiState>((set) => ({
  sidebarCollapsed: false,
  mobileNavOpen: false,
  commandOpen: false,
  contextPanelOpen: true,
  terminalDock: loadTerminalDockPreference(),
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
  setTerminalDockPosition: (position) =>
    set((state) => {
      const next = { ...state.terminalDock, position };
      persistTerminalDockPreference(next);
      return { terminalDock: next };
    }),
  setTerminalDockSize: (sizePercent) =>
    set((state) => {
      const next = {
        ...state.terminalDock,
        sizePercent: clampDockPercent(sizePercent),
      };
      persistTerminalDockPreference(next);
      return { terminalDock: next };
    }),
  resetTerminalDockSize: () =>
    set((state) => {
      const next = { ...state.terminalDock, sizePercent: 50 };
      persistTerminalDockPreference(next);
      return { terminalDock: next };
    }),
}));
