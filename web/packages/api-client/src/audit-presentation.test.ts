import { describe, expect, it } from "vitest";
import {
  AUDIT_ACTION_CODES,
  AUDIT_ACTOR_TYPE_CODES,
  AUDIT_RESOURCE_TYPE_CODES,
  auditCodeKey,
  auditPresentationKey,
  humanizeAuditCode,
} from "./audit-presentation";

describe("audit presentation catalog", () => {
  it("has stable, collision-free translation keys", () => {
    for (const codes of [
      AUDIT_ACTION_CODES,
      AUDIT_RESOURCE_TYPE_CODES,
      AUDIT_ACTOR_TYPE_CODES,
    ]) {
      const keys = codes.map(auditCodeKey);
      expect(new Set(keys).size).toBe(codes.length);
    }
  });

  it("builds namespaced keys and readable fallbacks", () => {
    expect(
      auditPresentationKey("settings.audit", "actions", "secret.create"),
    ).toBe("settings.audit.actions.secret_create");
    expect(humanizeAuditCode("platform_super_admin")).toBe(
      "Platform Super Admin",
    );
  });
});
