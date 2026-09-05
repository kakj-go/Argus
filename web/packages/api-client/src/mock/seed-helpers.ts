import type { MockHost } from "./resource-models";

export { mockInstallInstructionSets as seedInstructionSets } from "./install-instructions";

export const DAY = 86_400_000;
export const HOUR = 3_600_000;
export const MINUTE = 60_000;

export function createSeedHost(ago: (offsetMs: number) => string) {
  return (
    partial: Partial<MockHost> & Pick<MockHost, "id" | "name" | "address">,
  ): MockHost => ({
    enterpriseId: "ent-acme",
    hostname: partial.name ?? "",
    port: 22,
    platform: "linux",
    architecture: "amd64",
    connectionMode: "direct_ssh",
    environment: "production",
    labels: {},
    connectionStatus: "online",
    collectorStatus: "not_installed",
    createdAt: ago(90 * DAY),
    updatedAt: ago(DAY),
    ...partial,
  });
}
