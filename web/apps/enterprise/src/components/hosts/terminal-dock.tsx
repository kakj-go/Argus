import { useEffect, useMemo, useRef, useState } from "react";
import {
  PanelBottom,
  PanelBottomClose,
  PanelLeft,
  PanelRight,
  Power,
  type LucideIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { useApi, useTerminalSessions } from "@argus/api-client";
import { Button, TerminalEmulator } from "@argus/ui";
import { SessionTab } from "./session-tab";
import {
  clampDockPercent,
  useUiStore,
  type TerminalDockPosition,
} from "../../store/ui";
import "../../styles/terminal-panel.css";

export { clampDockPercent };

const MIN_DOCK_PERCENT = 20;
const MAX_DOCK_PERCENT = 80;

const DOCK_POSITIONS: Array<{
  value: TerminalDockPosition;
  labelKey: string;
  icon: LucideIcon;
}> = [
  { value: "bottom", labelKey: "hosts.terminal.dockPositionBottom", icon: PanelBottom },
  { value: "left", labelKey: "hosts.terminal.dockPositionLeft", icon: PanelLeft },
  { value: "right", labelKey: "hosts.terminal.dockPositionRight", icon: PanelRight },
];

/** 拖拽方向语义：沿箭头方向移动分隔条时的尺寸增减。 */
function keyboardDelta(position: TerminalDockPosition, key: string): number | null {
  const grow: Record<TerminalDockPosition, string> = {
    bottom: "ArrowUp",
    left: "ArrowLeft",
    right: "ArrowRight",
  };
  const shrink: Record<TerminalDockPosition, string> = {
    bottom: "ArrowDown",
    left: "ArrowRight",
    right: "ArrowLeft",
  };
  if (key === grow[position]) return 5;
  if (key === shrink[position]) return -5;
  return null;
}

export function TerminalDock() {
  const { t } = useTranslation();
  const api = useApi();
  const queryClient = useQueryClient();
  const {
    sessions,
    dockOpen,
    activeSessionId,
    hideSession,
    showSession,
    openDock,
    setActiveSession,
    closeDock,
    terminateSession,
    sendInput,
    resize,
  } = useTerminalSessions();
  const dockPosition = useUiStore((state) => state.terminalDock.position);
  const dockSizePercent = useUiStore((state) => state.terminalDock.sizePercent);
  const setTerminalDockPosition = useUiStore(
    (state) => state.setTerminalDockPosition,
  );
  const setTerminalDockSize = useUiStore((state) => state.setTerminalDockSize);
  const resetTerminalDockSize = useUiStore(
    (state) => state.resetTerminalDockSize,
  );
  const [terminatingIds, setTerminatingIds] = useState<Set<string>>(
    new Set(),
  );
  const draggingRef = useRef(false);
  const dockRef = useRef<HTMLElement>(null);

  const visibleSessions = useMemo(
    () => Array.from(sessions.values()).filter((session) => !session.hidden),
    [sessions],
  );
  const activeSession = activeSessionId
    ? sessions.get(activeSessionId)
    : undefined;

  useEffect(() => {
    if (activeSession && !activeSession.hidden) return;
    setActiveSession(visibleSessions[0]?.id ?? null);
  }, [activeSession, setActiveSession, visibleSessions]);

  useEffect(() => {
    if (visibleSessions.length === 0 && dockOpen) closeDock();
  }, [closeDock, dockOpen, visibleSessions.length]);

  // 百分比相对整个外壳（视口）计算，与 grid 里 --terminal-dock-size
  // 的解析基准一致：底部按整屏高度，左右按整屏宽度。
  const percentFromPointer = (event: PointerEvent): number | null => {
    const shell = dockRef.current?.closest(".argus-app-shell") ?? null;
    const rect = shell?.getBoundingClientRect();
    if (!rect || rect.width === 0 || rect.height === 0) return null;
    switch (dockPosition) {
      case "bottom":
        return ((rect.bottom - event.clientY) / rect.height) * 100;
      case "left":
        return ((event.clientX - rect.left) / rect.width) * 100;
      case "right":
        return ((rect.right - event.clientX) / rect.width) * 100;
    }
  };

  useEffect(() => {
    const onMove = (event: PointerEvent) => {
      if (!draggingRef.current) return;
      const next = percentFromPointer(event);
      if (next !== null) setTerminalDockSize(next);
    };
    const onUp = () => {
      draggingRef.current = false;
      document.body.classList.remove(
        "argus-is-resizing-terminal",
        "argus-is-resizing-terminal-ew",
      );
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    return () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
  });

  const terminate = async (sessionId: string) => {
    if (terminatingIds.has(sessionId)) return;
    setTerminatingIds((previous) => new Set(previous).add(sessionId));
    try {
      await terminateSession(sessionId, async (id) => {
        await api.remoteAccess.terminateSession(id, "user_requested");
      });
    } catch (error) {
      console.warn("[TerminalDock] Failed to terminate session:", error);
    } finally {
      // 终端坞就地终止后立即失效会话列表缓存，避免会话中心/主机页
      // 在 staleTime 窗口内仍显示旧的活动会话；终止失败（如会话已
      // 不存在）同样刷新，保持与会话中心一致的行为。
      void queryClient.invalidateQueries({
        queryKey: ["remote-access", "sessions"],
      });
      setTerminatingIds((previous) => {
        const next = new Set(previous);
        next.delete(sessionId);
        return next;
      });
    }
  };

  if (visibleSessions.length === 0) {
    const hiddenSession = Array.from(sessions.values())[0];
    if (!hiddenSession) return null;
    return (
      <button
        aria-label={t("hosts.terminal.openDock")}
        className="argus-terminal-dock-launcher"
        onClick={() => showSession(hiddenSession.id)}
        type="button"
      >
        {t("hosts.terminal.openDock")}
      </button>
    );
  }
  if (!dockOpen) {
    const firstSession = visibleSessions[0];
    if (!firstSession) return null;
    return (
      <button
        aria-label={t("hosts.terminal.openDock")}
        className="argus-terminal-dock-launcher"
        onClick={() => openDock(firstSession.id)}
        type="button"
      >
        {t("hosts.terminal.openDock")}
      </button>
    );
  }

  const isVerticalDock = dockPosition !== "bottom";

  return (
    <section
      aria-label={t("hosts.terminal.dockTitle")}
      className="argus-terminal-dock"
      ref={dockRef}
    >
      <div
        aria-label={t("hosts.terminal.resizeDock")}
        aria-orientation={isVerticalDock ? "vertical" : "horizontal"}
        aria-valuemax={MAX_DOCK_PERCENT}
        aria-valuemin={MIN_DOCK_PERCENT}
        aria-valuenow={Math.round(dockSizePercent)}
        className="argus-terminal-dock__separator"
        onKeyDown={(event) => {
          const delta = keyboardDelta(dockPosition, event.key);
          if (delta === null) return;
          event.preventDefault();
          setTerminalDockSize(dockSizePercent + delta);
        }}
        onDoubleClick={resetTerminalDockSize}
        onPointerDown={(event) => {
          event.currentTarget.setPointerCapture(event.pointerId);
          draggingRef.current = true;
          document.body.classList.add(
            isVerticalDock
              ? "argus-is-resizing-terminal-ew"
              : "argus-is-resizing-terminal",
          );
        }}
        role="separator"
        tabIndex={0}
        title={t("hosts.terminal.resetSize")}
      />
      <header className="argus-terminal-dock__tabs" role="tablist">
        <div className="argus-terminal-dock__tab-list">
          {visibleSessions.map((session) => (
            <SessionTab
              key={session.id}
              session={session}
              isActive={session.id === activeSessionId}
              isTerminating={terminatingIds.has(session.id)}
              onSelect={() => setActiveSession(session.id)}
              onClose={() => hideSession(session.id)}
            />
          ))}
        </div>
        <div className="argus-terminal-dock__actions">
          <div
            aria-label={t("hosts.terminal.dockPosition")}
            className="argus-terminal-dock__positions"
            role="group"
          >
            {DOCK_POSITIONS.map(({ value, labelKey, icon: Icon }) => (
              <Button
                aria-label={t(labelKey)}
                aria-pressed={dockPosition === value}
                className={
                  dockPosition === value ? "is-active-position" : undefined
                }
                key={value}
                onClick={() => setTerminalDockPosition(value)}
                size="icon"
                title={t(labelKey)}
                variant="ghost"
              >
                <Icon size={14} />
              </Button>
            ))}
          </div>
          <Button
            aria-label={t("hosts.terminal.terminate")}
            disabled={!activeSession || terminatingIds.has(activeSession.id)}
            onClick={() => activeSession && void terminate(activeSession.id)}
            size="icon"
            title={t("hosts.terminal.terminate")}
            variant="ghost"
          >
            <Power size={14} />
          </Button>
          <Button
            aria-label={t("hosts.terminal.collapseDock")}
            onClick={closeDock}
            size="icon"
            title={t("hosts.terminal.collapseDock")}
            variant="ghost"
          >
            <PanelBottomClose size={15} />
          </Button>
        </div>
      </header>
      <div className="argus-terminal-dock__viewport">
        {activeSession && (
          <TerminalEmulator
            account={activeSession.accountName}
            autoFocusKey={`${activeSession.id}:${dockOpen}`}
            host={activeSession.hostName}
            lines={activeSession.lines}
            mode={activeSession.protocol === "SSH PTY" ? "pty" : "line"}
            onCommand={(command) =>
              sendInput(activeSession.id, `${command}\r\n`)
            }
            onData={(data) => sendInput(activeSession.id, data)}
            onResize={(cols, rows) => resize(activeSession.id, cols, rows)}
            prompt={activeSession.protocol === "WinRS PowerShell" ? "PS>" : ""}
            protocol={activeSession.protocol}
            sessionId={activeSession.id}
            startedAt={activeSession.connectedAt}
            state={activeSession.state}
          />
        )}
      </div>
    </section>
  );
}
