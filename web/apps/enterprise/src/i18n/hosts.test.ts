import { describe, expect, it } from "vitest";
import { hostsEn, hostsZh } from "./hosts";

describe("hosts translations", () => {
  it("localizes the direct executor labels in Chinese", () => {
    expect(hostsZh.hosts.path.direct).toBe("Argus 直连执行器 → {{address}}");
    expect(hostsZh.hosts.standalone.directExecutor).toBe("直连执行器");
    expect(hostsZh.hosts.standalone.egressHint).toContain("直连执行器");
  });

  it("keeps the English product term in English", () => {
    expect(hostsEn.hosts.path.direct).toBe("Argus Direct Executor → {{address}}");
    expect(hostsEn.hosts.standalone.directExecutor).toBe("Direct Executor");
  });
});
