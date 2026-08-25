import { describe, expect, it } from "vitest";
import type { PendingActionPublic } from "@argus/api-client";
import { presentPendingAction } from "./pending-action-presentation";

const zh = (key: string, options?: Record<string, unknown>) => {
  const values: Record<string, string> = {
    "common.unknown": "未知",
    "governance.approvals.risk.write": "写入",
    "hosts.preview.createTitle": `新增主机 ${String(options?.name ?? "")}`,
    "hosts.preview.createSummary": "创建已验证的主机资源",
    "hosts.preview.createResourceDiff": `+ 主机资源 ${String(options?.name ?? "")}`,
    "hosts.preview.collectorDiff": "+ OTLP 收集器",
    "kubernetes.pendingAction.createTitle": `新增 Kubernetes 集群 ${String(options?.name ?? "")}`,
    "kubernetes.pendingAction.createSummary": "创建已验证的 Kubernetes 集群资源",
    "kubernetes.pendingAction.createResourceDiff": `~ Kubernetes 集群资源 ${String(options?.name ?? "")}`,
  };
  return values[key] ?? key;
};

function action(preview: Record<string, unknown>, title: string): PendingActionPublic {
  return {
    action_ref: "pa-test",
    schema_version: "argus.pending_action/v1",
    title,
    summary: "Create a validated host resource",
    risk: "write",
    preview,
    diff: [
      { kind: "add", text: "+ resource" },
      { kind: "add", text: "+ collector" },
    ],
    expires_at: new Date(Date.now() + 60_000).toISOString(),
    status: "awaiting_confirmation",
    available_actions: ["confirm", "cancel"],
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  };
}

describe("pending action presentation", () => {
  it("localizes host creation preview fields", () => {
    const presented = presentPendingAction(
      action({ name: "web-01", address: "10.0.0.1", port: 22 }, "Create host web-01"),
      zh,
    );
    expect(presented.title).toBe("新增主机 web-01");
    expect(presented.summary).toBe("创建已验证的主机资源");
    expect(presented.riskLabel).toBe("写入");
    expect(presented.diff.map((line) => line.text)).toEqual(["+ 主机资源 web-01", "+ OTLP 收集器"]);
  });

  it("localizes Kubernetes creation preview fields", () => {
    const presented = presentPendingAction(
      action({ name: "prod-east", api_server: "https://10.0.0.1:6443" }, "接入集群 prod-east"),
      zh,
    );
    expect(presented.title).toBe("新增 Kubernetes 集群 prod-east");
    expect(presented.summary).toBe("创建已验证的 Kubernetes 集群资源");
  });
});
