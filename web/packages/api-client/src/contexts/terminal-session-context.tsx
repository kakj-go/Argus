import {
  createContext,
  useCallback,
  useContext,
  useRef,
  useState,
  type ReactNode,
} from "react";
import type { RemoteAccessSession } from "../generated/contracts";
import {
  RemoteAccessConnection,
  type RemoteAccessServerFrame,
} from "../transport/remote-access";

export type TerminalLine = { kind: "stdout" | "stderr"; content: string };

export type SessionState = {
  id: string;
  session: RemoteAccessSession;
  lines: TerminalLine[];
  state: "connecting" | "connected" | "disconnected";
  error?: string;
  hostId: string;
  hostName: string;
  accountName: string;
  protocol: string;
  connectedAt?: Date;
  hidden: boolean;
};

type ConnectionState = {
  connection: RemoteAccessConnection;
  state: SessionState;
  subscribers: Set<(state: SessionState) => void>;
  explicitTermination: boolean;
};

type TerminalSessionContextValue = {
  sessions: Map<string, SessionState>;
  dockOpen: boolean;
  activeSessionId: string | null;
  attachSession: (
    sessionId: string,
    session: RemoteAccessSession,
    ticket: any,
    hostName: string,
    accountName: string,
  ) => Promise<void>;
  hideSession: (sessionId: string) => void;
  showSession: (sessionId: string) => void;
  setActiveSession: (sessionId: string | null) => void;
  openDock: (sessionId?: string) => void;
  closeDock: () => void;
  terminateSession: (
    sessionId: string,
    apiTerminate: (id: string) => Promise<void>,
  ) => Promise<void>;
  sendInput: (sessionId: string, data: string) => void;
  resize: (sessionId: string, cols: number, rows: number) => void;
  subscribeToSession: (
    sessionId: string,
    callback: (state: SessionState) => void,
  ) => () => void;
  getSessionState: (sessionId: string) => SessionState | undefined;
  isConnected: (sessionId: string) => boolean;
};

const TerminalSessionContext =
  createContext<TerminalSessionContextValue | null>(null);

export function TerminalSessionProvider({ children }: { children: ReactNode }) {
  const [sessions, setSessions] = useState<Map<string, SessionState>>(
    new Map(),
  );
  const [dockOpen, setDockOpen] = useState(false);
  const [activeSessionId, setActiveSessionIdState] = useState<string | null>(
    null,
  );
  const connectionsRef = useRef<Map<string, ConnectionState>>(new Map());
  const terminationPromisesRef = useRef<Map<string, Promise<void>>>(new Map());
  const terminatedSessionIdsRef = useRef<Set<string>>(new Set());

  const publish = useCallback(
    (sessionId: string, update: (state: SessionState) => void) => {
      const record = connectionsRef.current.get(sessionId);
      if (!record) return;
      update(record.state);
      setSessions((previous) =>
        new Map(previous).set(sessionId, {
          ...record.state,
          lines: [...record.state.lines],
        }),
      );
      record.subscribers.forEach((callback) => {
        try {
          callback(record.state);
        } catch (error) {
          console.error("[TerminalSession] subscriber callback failed", error);
        }
      });
    },
    [],
  );

  const attachSession = useCallback(
    async (
      sessionId: string,
      session: RemoteAccessSession,
      ticket: any,
      hostName: string,
      accountName: string,
    ) => {
      const existing = connectionsRef.current.get(sessionId);
      if (existing) {
        existing.state.hidden = false;
        setSessions((previous) =>
          new Map(previous).set(sessionId, { ...existing.state }),
        );
        setActiveSessionIdState(sessionId);
        setDockOpen(true);
        return;
      }

      const initialState: SessionState = {
        id: sessionId,
        session,
        lines: [],
        state: "connecting",
        hostId: session.host_id,
        hostName,
        accountName,
        protocol: session.protocol === "ssh" ? "SSH PTY" : "WinRS PowerShell",
        hidden: false,
      };
      const connection = new RemoteAccessConnection(ticket, {
        cols: 100,
        rows: 30,
        onFrame: (frame: RemoteAccessServerFrame) => {
          const record = connectionsRef.current.get(sessionId);
          if (!record) return;
          if (frame.type === "server_ready") {
            publish(sessionId, (state) => {
              state.state = "connected";
              state.connectedAt = new Date();
            });
          } else if (frame.type === "output") {
            publish(sessionId, (state) => {
              state.lines = [
                ...state.lines,
                { kind: frame.stream, content: frame.data },
              ];
            });
          } else if (frame.type === "error") {
            publish(sessionId, (state) => {
              state.error = frame.message || frame.code;
            });
          } else if (
            frame.type === "state" &&
            [
              "terminated",
              "failed",
              "connection_lost",
              "invalidated",
              "expired",
            ].includes(frame.status)
          ) {
            publish(sessionId, (state) => {
              state.state = "disconnected";
              state.error = frame.reason || state.error;
            });
          }
        },
        onError: (reason) =>
          publish(sessionId, (state) => {
            state.state = "disconnected";
            state.error = `连接失败: ${reason}`;
          }),
        onClose: (reason) => {
          const record = connectionsRef.current.get(sessionId);
          if (!record || record.explicitTermination) return;
          publish(sessionId, (state) => {
            state.state = "disconnected";
            state.error = state.error || `连接已关闭: ${reason}`;
          });
        },
      });
      connectionsRef.current.set(sessionId, {
        connection,
        state: initialState,
        subscribers: new Set(),
        explicitTermination: false,
      });
      setSessions((previous) =>
        new Map(previous).set(sessionId, { ...initialState }),
      );
      setActiveSessionIdState(sessionId);
      setDockOpen(true);
    },
    [publish],
  );

  const hideSession = useCallback((sessionId: string) => {
    const record = connectionsRef.current.get(sessionId);
    if (!record) return;
    record.state.hidden = true;
    setSessions((previous) =>
      new Map(previous).set(sessionId, { ...record.state }),
    );
    setActiveSessionIdState((current) =>
      current === sessionId ? null : current,
    );
  }, []);

  const showSession = useCallback((sessionId: string) => {
    const record = connectionsRef.current.get(sessionId);
    if (!record) return;
    record.state.hidden = false;
    setSessions((previous) =>
      new Map(previous).set(sessionId, { ...record.state }),
    );
    setActiveSessionIdState(sessionId);
    setDockOpen(true);
  }, []);

  const setActiveSession = useCallback((sessionId: string | null) => {
    if (sessionId && !connectionsRef.current.has(sessionId)) return;
    setActiveSessionIdState(sessionId);
  }, []);
  const openDock = useCallback(
    (sessionId?: string) =>
      sessionId ? showSession(sessionId) : setDockOpen(true),
    [showSession],
  );
  const closeDock = useCallback(() => setDockOpen(false), []);

  const terminateSession = useCallback(
    async (sessionId: string, apiTerminate: (id: string) => Promise<void>) => {
      if (terminatedSessionIdsRef.current.has(sessionId)) return;
      const pending = terminationPromisesRef.current.get(sessionId);
      if (pending) return pending;
      const promise = (async () => {
        const record = connectionsRef.current.get(sessionId);
        if (record) {
          record.explicitTermination = true;
          record.connection.close("user_requested");
          connectionsRef.current.delete(sessionId);
          setSessions((previous) => {
            const next = new Map(previous);
            next.delete(sessionId);
            return next;
          });
          setActiveSessionIdState((current) =>
            current === sessionId ? null : current,
          );
        }
        await apiTerminate(sessionId);
        terminatedSessionIdsRef.current.add(sessionId);
      })();
      terminationPromisesRef.current.set(sessionId, promise);
      try {
        await promise;
      } finally {
        terminationPromisesRef.current.delete(sessionId);
      }
    },
    [],
  );

  const sendInput = useCallback((sessionId: string, data: string) => {
    const record = connectionsRef.current.get(sessionId);
    if (!record) return;
    record.connection.input(data);
  }, []);
  const resize = useCallback(
    (sessionId: string, cols: number, rows: number) => {
      const record = connectionsRef.current.get(sessionId);
      if (!record) return;
      record.connection.resize(cols, rows);
    },
    [],
  );
  const subscribeToSession = useCallback(
    (sessionId: string, callback: (state: SessionState) => void) => {
      const record = connectionsRef.current.get(sessionId);
      if (record) record.subscribers.add(callback);
      return () => record?.subscribers.delete(callback);
    },
    [],
  );
  const getSessionState = useCallback(
    (sessionId: string) => sessions.get(sessionId),
    [sessions],
  );
  const isConnected = useCallback(
    (sessionId: string) => sessions.get(sessionId)?.state === "connected",
    [sessions],
  );

  return (
    <TerminalSessionContext.Provider
      value={{
        sessions,
        dockOpen,
        activeSessionId,
        attachSession,
        hideSession,
        showSession,
        setActiveSession,
        openDock,
        closeDock,
        terminateSession,
        sendInput,
        resize,
        subscribeToSession,
        getSessionState,
        isConnected,
      }}
    >
      {children}
    </TerminalSessionContext.Provider>
  );
}

export function useTerminalSessions() {
  const context = useContext(TerminalSessionContext);
  if (!context)
    throw new Error(
      "useTerminalSessions must be used within TerminalSessionProvider",
    );
  return context;
}
