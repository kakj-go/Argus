import type { ActionOneTimeResult } from "../generated/contracts";

export function mockInstallInstructionSets(
  token: string,
  expiresAt: string,
  kind: "host" | "connector" = "host",
): ActionOneTimeResult["instruction_sets"] {
  return (["linux-system", "linux-user"] as const).map((scope) => {
    const scriptPath =
      kind === "connector"
        ? "/api/v1/connectors/bootstrap-script"
        : "/v1/host-bootstrap-script";
    const command = `(set -eu; umask 077; ARGUS_BOOTSTRAP=$(mktemp "\${TMPDIR:-/tmp}/argus-bootstrap.XXXXXX"); trap 'rm -f "$ARGUS_BOOTSTRAP"' EXIT HUP INT TERM; curl -fsS --proto '=https' --tlsv1.2 --insecure --header 'X-Argus-Enrollment-Token: ${token}' --output "$ARGUS_BOOTSTRAP" 'https://argus.example.com${scriptPath}?scope=${scope}'; chmod 0700 "$ARGUS_BOOTSTRAP"; sh "$ARGUS_BOOTSTRAP")`; // ARGUS_INSECURE_FIRST_FETCH_ONLY
    return {
      scope,
      command,
      download_tls_mode: "insecure-first-fetch",
      expires_at: expiresAt,
      trust_bundle_epoch: 1,
      trust_bundle_sha256: "b".repeat(64),
      installer_sha256: "a".repeat(64),
      capability_warnings:
        scope === "linux-user"
          ? ["User mode cannot run profiles that require host capabilities."]
          : [],
    };
  });
}
