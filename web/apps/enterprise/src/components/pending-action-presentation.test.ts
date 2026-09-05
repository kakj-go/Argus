import { describe, expect, it } from "vitest";
import type { PendingActionPublic } from "@argus/api-client";
import { commonEn, commonZh } from "../i18n/common";
import { governanceEn, governanceZh } from "../i18n/governance";
import { pendingActionsEn, pendingActionsZh } from "../i18n/pending-actions";
import {
  PENDING_ACTION_TYPES,
  presentPendingAction,
  presentPendingActionPreview,
} from "./pending-action-presentation";

function translator(...resources: unknown[]) {
  const merged = Object.assign({}, ...resources) as Record<string, unknown>;
  return (key: string, options?: Record<string, unknown>) => {
    const value = key.split(".").reduce<unknown>((current, segment) => {
      if (typeof current !== "object" || current === null) return undefined;
      return (current as Record<string, unknown>)[segment];
    }, merged);
    if (typeof value !== "string") return key;
    return value.replace(/\{\{(\w+)\}\}/g, (_, name: string) =>
      String(options?.[name] ?? ""),
    );
  };
}

const zh = translator(commonZh, governanceZh, pendingActionsZh);
const en = translator(commonEn, governanceEn, pendingActionsEn);

function action(
  actionType: string,
  preview: Record<string, unknown> = { name: "web-01" },
  status: PendingActionPublic["status"] = "awaiting_confirmation",
): PendingActionPublic {
  return {
    action_ref: "pa-test",
    action_type: actionType,
    schema_version: "argus.pending_action/v1",
    title: "RAW TITLE",
    summary: "RAW SUMMARY",
    risk: "write",
    preview,
    diff: [{ kind: "change", text: "RAW DIFF" }],
    expires_at: new Date(Date.now() + 60_000).toISOString(),
    status,
    available_actions: ["confirm", "cancel"],
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  };
}

describe("pending action presentation", () => {
  it.each([zh, en])(
    "localizes every known action type without raw text",
    (t) => {
      for (const actionType of PENDING_ACTION_TYPES) {
        const presented = presentPendingAction(action(actionType), t);
        expect(presented.known, actionType).toBe(true);
        expect(`${presented.title} ${presented.summary}`).not.toContain("RAW");
        expect(presented.diff.map((line) => line.text).join(" ")).not.toContain(
          "RAW",
        );
        expect(presented.title, actionType).not.toContain("pendingActions.");
      }
    },
  );

  it("localizes the bastion confirmation shown in the reported screenshot", () => {
    const presented = presentPendingAction(
      action("bastion_scope.create", { name: "测试" }),
      zh,
    );
    expect(presented.title).toBe("添加堡垒机 测试");
    expect(presented.summary).toBe("创建稳定的 Bastion Scope 和一次性注册信息");
    expect(presented.diff[0]?.text).toBe("创建堡垒机 测试");
  });

  it("fails closed for an unknown action type", () => {
    const presented = presentPendingAction(action("unknown.action"), zh);
    expect(presented.known).toBe(false);
    expect(presented.title).toBe("待确认操作");
    expect(presented.summary).not.toContain("RAW");
    expect(presented.diff[0]?.text).not.toContain("RAW");
  });

  it("filters internal ids and localizes enum values in approval previews", () => {
    const fields = presentPendingActionPreview(
      action("bastion.connector.replace", {
        scope_id: "internal-id",
        name: "上海堡垒机",
        install_mode: "direct_install",
      }),
      en,
    );
    expect(fields).toEqual([
      { label: "Name", value: "上海堡垒机" },
      {
        label: "Installation mode",
        value: "Platform-managed SSH installation",
      },
    ]);
  });
});
