// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { ActionOneTimeResult } from "@argus/api-client";
import { LocaleProvider } from "@argus/ui";

import "../../i18n";
import { InstallInstructionPanel } from "./install-instruction-panel";

beforeEach(() => window.localStorage.setItem("argus.locale", "zh-CN"));
afterEach(cleanup);

function resultWithWarnings(warnings: string[] | null): ActionOneTimeResult {
  return {
    schema_version: "argus.action_one_time_result/v3",
    execution_id: "00000000-0000-0000-0000-000000000001",
    result_kind: "connector_install_command",
    expires_at: new Date(Date.now() + 60_000).toISOString(),
    instruction_sets: [
      {
        scope: "linux-system",
        command: "download bootstrap and execute it",
        download_tls_mode: "insecure-first-fetch",
        expires_at: new Date(Date.now() + 60_000).toISOString(),
        trust_bundle_epoch: 1,
        trust_bundle_sha256: "b".repeat(64),
        installer_sha256: "a".repeat(64),
        capability_warnings: warnings as string[],
      },
    ],
  };
}

describe("InstallInstructionPanel", () => {
  it("keeps the command visible when a stale server returns null warnings", () => {
    render(
      <LocaleProvider>
        <InstallInstructionPanel result={resultWithWarnings(null)} />
      </LocaleProvider>,
    );

    expect(screen.getByText("download bootstrap and execute it")).toBeVisible();
    expect(screen.getByText("自签名证书快速接入")).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "一行安装（推荐）" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "交互式" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "自动化" }),
    ).not.toBeInTheDocument();
  });

  it("keeps strict one-line onboarding explicit about token exposure", () => {
    const result = resultWithWarnings([]);
    result.instruction_sets[0]!.download_tls_mode = "strict";
    render(
      <LocaleProvider>
        <InstallInstructionPanel result={result} />
      </LocaleProvider>,
    );

    expect(screen.getByText("一行命令包含令牌")).toBeVisible();
    expect(screen.queryByText("自签名证书快速接入")).not.toBeInTheDocument();
  });

  it("shows a safe missing-result state for a malformed instruction list", () => {
    const result = resultWithWarnings([]);
    result.instruction_sets = null as unknown as [];
    render(
      <LocaleProvider>
        <InstallInstructionPanel result={result} />
      </LocaleProvider>,
    );

    expect(screen.getByText("安装指令不可用")).toBeVisible();
  });
});
