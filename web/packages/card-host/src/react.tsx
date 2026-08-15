import { useLocale, useTheme } from "@argus/ui";
import {
  Component,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";
import { injectCardRuntime } from "./card-runtime";
import type {
  CardBridgeBindings,
  CardLocale,
  CardNotifyLevel,
  CardPresentationContext,
  CardTheme,
} from "./protocol";
import {
  createCardHost,
  generateCardNonce,
  type ActionInvokeHandler,
  type CardHost,
  type CardProtocolViolation,
  type QueryInvokeHandler,
} from "./sandbox-host";

export type SandboxCardProps = {
  cardInstanceId: string;
  /** Raw card HTML (InteractiveCard.htmlTemplate); the bridge runtime is injected. */
  html: string;
  /** Capability allowlist for this instance. */
  bindings?: Partial<CardBridgeBindings>;
  /** Initial slot data delivered with host.init. */
  initialData?: Record<string, unknown>;
  /** Overrides the locale from @argus/ui LocaleProvider (default zh-CN). */
  locale?: CardLocale;
  /** Overrides the theme from @argus/ui ThemeProvider (default dark). */
  theme?: CardTheme;
  /** Design tokens sent to the card; defaults to document CSS custom properties. */
  designTokens?: Record<string, string>;
  onQueryInvoke?: QueryInvokeHandler;
  onActionInvoke?: ActionInvokeHandler;
  onNotify?: (level: CardNotifyLevel, message: string) => void;
  /** Defaults to a controlled window.open (http/https/mailto only). */
  onOpenLink?: (url: string) => void;
  onReady?: () => void;
  onProtocolViolation?: (
    reason: CardProtocolViolation,
    message: unknown,
  ) => void;
  /** Accessible iframe title. */
  title?: string;
  className?: string;
  style?: CSSProperties;
  /** Height before the first card.resize, default 120px. */
  minHeight?: number;
  /** Clamp for card.resize, default 2000px. */
  maxHeight?: number;
};

const DEFAULT_MIN_HEIGHT = 120;
const DEFAULT_MAX_HEIGHT = 2000;

const ALLOWED_LINK_PROTOCOLS = new Set(["http:", "https:", "mailto:"]);

/** Controlled link opening: scheme allowlist + isolated new window. */
export function openCardLink(url: string): void {
  try {
    const parsed = new URL(url, window.location.href);
    if (!ALLOWED_LINK_PROTOCOLS.has(parsed.protocol)) return;
    window.open(parsed.href, "_blank", "noopener,noreferrer");
  } catch {
    // Malformed URLs are dropped.
  }
}

/** Reads the live CSS custom properties (--*) from the document root. */
export function collectDesignTokens(): Record<string, string> {
  if (typeof window === "undefined" || !document.documentElement) return {};
  const styles = window.getComputedStyle(document.documentElement);
  const tokens: Record<string, string> = {};
  for (let index = 0; index < styles.length; index += 1) {
    const name = styles.item(index);
    if (name.startsWith("--")) {
      tokens[name] = styles.getPropertyValue(name).trim();
    }
  }
  return tokens;
}

type FrameProps = SandboxCardProps & {
  locale: CardLocale;
  theme: CardTheme;
};

function SandboxCardFrame(props: FrameProps) {
  const {
    cardInstanceId,
    html,
    minHeight = DEFAULT_MIN_HEIGHT,
    maxHeight = DEFAULT_MAX_HEIGHT,
  } = props;
  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  const hostRef = useRef<CardHost | null>(null);
  const [height, setHeight] = useState(minHeight);

  // New HTML or instance => new nonce and a fresh handshake.
  const nonce = useMemo(
    () => generateCardNonce(),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [cardInstanceId, html],
  );
  const srcDoc = useMemo(
    () => injectCardRuntime(html, { cardInstanceId, nonce }),
    [html, cardInstanceId, nonce],
  );

  // Callbacks always see the latest props without recreating the host.
  const callbacksRef = useRef(props);
  callbacksRef.current = props;

  const bindingsKey = JSON.stringify([
    props.bindings?.queryBindingIds ?? [],
    props.bindings?.actionBindingIds ?? [],
  ]);
  const [queryBindingIds, actionBindingIds] = useMemo(
    () => JSON.parse(bindingsKey) as [string[], string[]],
    [bindingsKey],
  );

  const context: CardPresentationContext = useMemo(
    () => ({
      locale: props.locale,
      theme: props.theme,
      colorScheme: props.theme,
      designTokens: props.designTokens ?? collectDesignTokens(),
    }),
    [props.locale, props.theme, props.designTokens],
  );

  useEffect(() => {
    const iframe = iframeRef.current;
    if (!iframe) return;
    const bindings: CardBridgeBindings = { queryBindingIds, actionBindingIds };
    const host = createCardHost(iframe, {
      cardInstanceId,
      nonce,
      bindings,
      initialData: callbacksRef.current.initialData ?? {},
      context,
      maxHeight,
      onReady: () => callbacksRef.current.onReady?.(),
      onQueryInvoke: (bindingId, params) =>
        callbacksRef.current.onQueryInvoke?.(bindingId, params),
      onActionInvoke: (bindingId, params) =>
        callbacksRef.current.onActionInvoke?.(bindingId, params),
      onNotify: (level, message) =>
        callbacksRef.current.onNotify?.(level, message),
      onOpenLink: (url) =>
        (callbacksRef.current.onOpenLink ?? openCardLink)(url),
      onResize: (next) =>
        setHeight(Math.max(minHeight, Math.min(next, maxHeight))),
      onProtocolViolation: (reason, message) =>
        callbacksRef.current.onProtocolViolation?.(reason, message),
    });
    hostRef.current = host;
    setHeight(minHeight);
    return () => {
      host.destroy();
      hostRef.current = null;
    };
    // Context changes are pushed via setContext below, not by recreation.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cardInstanceId, nonce, bindingsKey, minHeight, maxHeight]);

  useEffect(() => {
    hostRef.current?.setContext(context);
  }, [context]);

  return (
    <iframe
      ref={iframeRef}
      className={props.className}
      referrerPolicy="no-referrer"
      sandbox="allow-scripts"
      srcDoc={srcDoc}
      style={{
        display: "block",
        width: "100%",
        border: 0,
        overflow: "hidden",
        height,
        ...props.style,
      }}
      title={props.title ?? "Argus card"}
    />
  );
}

/** Reads theme/locale from @argus/ui providers; throws when absent. */
function SandboxCardWithEnvironment(props: SandboxCardProps) {
  const { resolvedTheme } = useTheme();
  const { locale } = useLocale();
  return (
    <SandboxCardFrame
      {...props}
      locale={props.locale ?? locale}
      theme={props.theme ?? resolvedTheme}
    />
  );
}

type BoundaryProps = { children: ReactNode; fallback: ReactNode };
type BoundaryState = { failed: boolean };

/** Falls back to explicit defaults when no @argus/ui providers are present. */
class EnvironmentBoundary extends Component<BoundaryProps, BoundaryState> {
  state: BoundaryState = { failed: false };

  static getDerivedStateFromError(): BoundaryState {
    return { failed: true };
  }

  render() {
    return this.state.failed ? this.props.fallback : this.props.children;
  }
}

/** Render errors inside the host tree never take down the chat surface. */
class CardErrorBoundary extends Component<BoundaryProps, BoundaryState> {
  state: BoundaryState = { failed: false };

  static getDerivedStateFromError(): BoundaryState {
    return { failed: true };
  }

  render() {
    return this.state.failed ? this.props.fallback : this.props.children;
  }
}

const DEFAULT_LOCALE: CardLocale = "zh-CN";
const DEFAULT_THEME: CardTheme = "dark";

/**
 * Sandboxed card host: renders card HTML in an opaque-origin iframe
 * (sandbox="allow-scripts", no same-origin/forms/popups) and bridges
 * query.invoke/action.invoke/notify/resize/link.open to the host app.
 */
export function SandboxCard(props: SandboxCardProps) {
  const fallbackFrame = (
    <SandboxCardFrame
      {...props}
      locale={props.locale ?? DEFAULT_LOCALE}
      theme={props.theme ?? DEFAULT_THEME}
    />
  );
  return (
    <CardErrorBoundary fallback={null}>
      <EnvironmentBoundary fallback={fallbackFrame}>
        <SandboxCardWithEnvironment {...props} />
      </EnvironmentBoundary>
    </CardErrorBoundary>
  );
}
