// Shared E2E origins: the Go harness injects the deployed domain origins
// (https://argus.dev ...); the loopback fallbacks only serve the local mock
// dev servers used by `pnpm e2e` without a cluster.
export const enterpriseOrigin =
  process.env.ARGUS_E2E_ENTERPRISE_ORIGIN ?? "http://127.0.0.1:4173";
export const platformOrigin =
  process.env.ARGUS_E2E_PLATFORM_ORIGIN ?? "http://127.0.0.1:4174";
export const cardOrigin =
  process.env.ARGUS_E2E_CARD_ORIGIN ?? "http://127.0.0.1:4176";
