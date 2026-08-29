import { runtimeConfig } from "./runtime-config";

/**
 * Card runtime origin: deployment runtime config first, VITE_ override for
 * dev builds, and the local dev server as the final fallback.
 */
export function cardOrigin(): string {
  return (
    runtimeConfig().cardOrigin ??
    import.meta.env.VITE_CARD_ORIGIN ??
    "http://localhost:4176"
  );
}
