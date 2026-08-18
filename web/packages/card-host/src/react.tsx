import type { CardManifest, RenderPlan } from "@argus/api-client/contracts";
import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from "react";
import type {
  CardBridgeBindings,
  CardLocale,
  CardNotifyLevel,
  CardTheme,
  CardValidationReport,
  CardValidationRequest,
} from "./protocol";
import {
  createCardHost,
  type ActionInvokeHandler,
  type CardHost,
  type CardProtocolViolation,
  type QueryInvokeHandler,
} from "./sandbox-host";

export type SandboxCardProps = {
  card_origin: string;
  manifest: CardManifest;
  render_plan: RenderPlan;
  html: string;
  locale: CardLocale;
  color_scheme: CardTheme;
  bindings?: Partial<CardBridgeBindings>;
  initial_data?: Record<string, unknown>;
  design_tokens?: Record<string, string>;
  onQueryInvoke?: QueryInvokeHandler;
  onActionInvoke?: ActionInvokeHandler;
  onNotify?: (level: CardNotifyLevel, message: string) => void;
  onReady?: () => void;
  onProtocolViolation?: (reason: CardProtocolViolation, message: unknown) => void;
  validation?: CardValidationRequest;
  onValidationReport?: (report: CardValidationReport) => void;
  title?: string;
  className?: string;
  style?: CSSProperties;
  min_height?: number;
  max_height?: number;
};

const DEFAULT_MIN_HEIGHT = 120;
const DEFAULT_MAX_HEIGHT = 2000;

export function collectDesignTokens(): Record<string, string> {
  if (typeof window === "undefined" || !document.documentElement) return {};
  const styles = window.getComputedStyle(document.documentElement);
  const tokens: Record<string, string> = {};
  for (let index = 0; index < styles.length; index += 1) {
    const name = styles.item(index);
    if (name.startsWith("--")) tokens[name] = styles.getPropertyValue(name).trim();
  }
  return tokens;
}

export function openCardLink(url: string): void {
  try {
    const parsed = new URL(url, window.location.href);
    if (!["http:", "https:", "mailto:"].includes(parsed.protocol)) return;
    window.open(parsed.href, "_blank", "noopener,noreferrer");
  } catch {
    // Invalid links are ignored.
  }
}

async function sha256(value: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(value));
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

export function SandboxCard(props: SandboxCardProps) {
  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  const hostRef = useRef<CardHost | null>(null);
  const callbacksRef = useRef(props);
  callbacksRef.current = props;
  const minHeight = props.min_height ?? DEFAULT_MIN_HEIGHT;
  const maxHeight = props.max_height ?? DEFAULT_MAX_HEIGHT;
  const [height, setHeight] = useState(minHeight);
  const [contentHash, setContentHash] = useState<string | null>(null);
  const [hashError, setHashError] = useState(false);

  useEffect(() => {
    let active = true;
    void sha256(props.html).then((hash) => {
      if (!active) return;
      const expected = props.manifest.entrypoint_hash;
      setHashError(Boolean(expected) && expected !== hash);
      setContentHash(hash);
    });
    return () => { active = false; };
  }, [props.html, props.manifest.entrypoint_hash]);

  const manifest = useMemo<CardManifest>(
    () => ({
      ...props.manifest,
      entrypoint_hash: props.manifest.entrypoint_hash || contentHash || "pending",
    }),
    [props.manifest, contentHash],
  );
  const context = useMemo(
    () => ({
      locale: props.locale,
      color_scheme: props.color_scheme,
      design_tokens: props.design_tokens ?? collectDesignTokens(),
    }),
    [props.locale, props.color_scheme, props.design_tokens],
  );
  const bindingsKey = JSON.stringify(props.bindings ?? {});
  const dataKey = JSON.stringify(props.initial_data ?? {});
  const validationKey = JSON.stringify(props.validation ?? null);

  useEffect(() => {
    const iframe = iframeRef.current;
    if (!iframe || !contentHash || hashError) return;
    const host = createCardHost(iframe, {
      card_origin: props.card_origin,
      manifest,
      render_plan: props.render_plan,
      html: props.html,
      bindings: props.bindings,
      initial_data: props.initial_data,
      context,
      max_height: maxHeight,
      onReady: () => callbacksRef.current.onReady?.(),
      onQueryInvoke: (bindingId) => callbacksRef.current.onQueryInvoke?.(bindingId),
      onActionInvoke: (bindingId) => callbacksRef.current.onActionInvoke?.(bindingId),
      onNotify: (level, message) => callbacksRef.current.onNotify?.(level, message),
      onResize: (next) => setHeight(Math.max(minHeight, Math.min(next, maxHeight))),
      onProtocolViolation: (reason, message) => callbacksRef.current.onProtocolViolation?.(reason, message),
      validation: callbacksRef.current.validation,
      onValidationReport: (report) => callbacksRef.current.onValidationReport?.(report),
    });
    hostRef.current = host;
    setHeight(minHeight);
    return () => {
      host.destroy();
      hostRef.current = null;
    };
    // Binding identity is represented by bindingsKey.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [props.card_origin, props.html, props.render_plan, manifest, contentHash, hashError, bindingsKey, validationKey, minHeight, maxHeight]);

  useEffect(() => hostRef.current?.setContext(context), [context]);
  useEffect(() => {
    hostRef.current?.setData(props.initial_data ?? {});
    // Data identity is represented by dataKey.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dataKey]);

  if (hashError) {
    return <div className={props.className} role="alert">Card content verification failed</div>;
  }
  const runtimeUrl = new URL(props.card_origin);
  runtimeUrl.searchParams.set("parent_origin", window.location.origin);
  runtimeUrl.searchParams.set("card_instance_id", props.render_plan.card_instance_id);

  return (
    <iframe
      ref={iframeRef}
      className={props.className}
      referrerPolicy="no-referrer"
      sandbox="allow-scripts allow-same-origin"
      src={runtimeUrl.href}
      style={{ display: "block", width: "100%", border: 0, overflow: "hidden", height, ...props.style }}
      title={props.title ?? "Argus card"}
    />
  );
}
