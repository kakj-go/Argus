// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";

import { createSeedDb } from "./seed";
import { saveDb, STORAGE_PREFIX, type MockDb } from "./store";

describe("mock persistence sensitive data boundary", () => {
  beforeEach(() => localStorage.clear());

  it("models the authenticated one-command bootstrap", () => {
    const db = createSeedDb(1_700_000_000_000);
    const enrollment = db.enrollmentTokens[0];
    expect(enrollment).toBeDefined();
    const instruction = enrollment!.instructionSets[0];
    expect(instruction).toBeDefined();
    expect(instruction!.command).toContain("--proto '=https'");
    expect(instruction!.command).toContain("--tlsv1.2");
    expect(instruction!.command).toContain("--insecure");
    expect(instruction!.command).toContain("X-Argus-Enrollment-Token:");
    expect(instruction!.command).toContain(enrollment!.token);
    expect(instruction!.command).not.toContain("\n");
  });

  it("never writes one-time host or Connector commands and tokens", () => {
    const db = createSeedDb(1_700_000_000_000);
    const sensitive = [
      ...db.hostEnrollmentTokens.flatMap((item) => [
        item.token,
        ...item.instructionSets.flatMap((set) => [set.command]),
      ]),
      ...db.enrollmentTokens.flatMap((item) => [
        item.token,
        ...item.instructionSets.flatMap((set) => [set.command]),
      ]),
      ...db.uninstallCommands.flatMap((item) => [
        item.token,
        item.uninstallCommand,
      ]),
      ...Object.values(db.oneTimeResults).flatMap((item) =>
        item.instruction_sets.flatMap((set) => [set.command]),
      ),
    ].filter(Boolean);

    expect(sensitive.length).toBeGreaterThan(0);
    saveDb(db);

    const key = Object.keys(localStorage).find((item) =>
      item.startsWith(STORAGE_PREFIX),
    );
    expect(key).toBeDefined();
    const raw = localStorage.getItem(key!) ?? "";
    for (const value of sensitive) expect(raw).not.toContain(value);

    const stored = JSON.parse(raw) as MockDb;
    expect(stored.oneTimeResults).toEqual({});
    expect(
      stored.bastionScopes.flatMap((scope) => [
        scope.registrationToken?.token,
        ...(scope.registrationToken?.instructionSets.flatMap((set) => [
          set.command,
        ]) ?? []),
        scope.uninstallCommand?.token,
        scope.uninstallCommand?.uninstallCommand,
      ]),
    ).not.toContainEqual(expect.stringMatching(/\S/));
  });
});
