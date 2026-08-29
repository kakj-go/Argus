// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import {
  ApiError,
  ApiProvider,
  createMockApiClient,
  type Host,
  type MockApiClient,
} from "@argus/api-client";
import { LocaleProvider } from "@argus/ui";
import { TerminalSessionProvider } from "@argus/api-client";
import "../../i18n";
import { RealTerminalTab } from "./real-terminal-tab";

beforeAll(() => {
  Element.prototype.scrollIntoView = vi.fn();
});
beforeEach(() => {
  window.localStorage.setItem("argus.locale", "zh-CN");
});
afterEach(cleanup);

function createWrapper(client: MockApiClient) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <LocaleProvider>
        <ApiProvider client={client}>
          <TerminalSessionProvider>
            <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
          </TerminalSessionProvider>
        </ApiProvider>
      </LocaleProvider>
    );
  };
}

async function renderTerminal() {
  const client = createMockApiClient({ persist: false, delay: 0 });
  await client.auth.login({ username: "root", password: "123456" });
  const host = (await client.hosts.list()).items[0] as Host;
  const listManagedAccounts = vi
    .spyOn(client.secrets, "listManagedAccounts")
    .mockResolvedValue([
      {
        id: "account-1",
        enterprise_id: host.enterprise_id,
        host_id: host.id,
        credential_id: "credential-1",
        username: "argus",
        privilege_level: "standard",
        allowed_protocols: ["ssh", "winrm"],
        status: "active",
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        version: 1,
      },
    ]);
  vi.spyOn(client.remoteAccess, "listSessions").mockResolvedValue({
    items: [],
    nextCursor: null,
    hasMore: false,
  });
  render(<RealTerminalTab host={host} />, { wrapper: createWrapper(client) });
  await screen.findByRole("combobox", { name: "登录账号" });
  await waitFor(() => expect(listManagedAccounts).toHaveBeenCalled());
  fireEvent.click(screen.getByRole("combobox", { name: "登录账号" }));
  fireEvent.click(await screen.findByRole("option", { name: "argus" }));
  fireEvent.change(screen.getByRole("textbox", { name: "事由" }), {
    target: { value: "investigate incident" },
  });
  return client;
}

describe("RealTerminalTab step-up", () => {
  it("opens MFA step-up and resumes the existing request after verification", async () => {
    const client = await renderTerminal();
    const createRequest = vi
      .spyOn(client.remoteAccess, "createRequest")
      .mockResolvedValueOnce({
        id: "request-step-up-required",
        status: "awaiting_mfa",
      } as never);
    const resumeRequest = vi
      .spyOn(client.remoteAccess, "resumeRequest")
      .mockResolvedValueOnce({ status: "awaiting_approval" } as never);
    const stepUp = vi.spyOn(client.auth, "stepUp").mockResolvedValue({
      amr: ["password", "totp"],
      expires_at: new Date(Date.now() + 300_000).toISOString(),
    });

    fireEvent.click(screen.getByRole("button", { name: "建立会话" }));
    expect(
      await screen.findByRole("dialog", { name: "验证后建立远程会话" }),
    ).toBeVisible();
    fireEvent.change(screen.getByLabelText(/验证码或恢复码/), {
      target: { value: "123456" },
    });
    fireEvent.click(screen.getByRole("button", { name: "验证并继续" }));

    await waitFor(() =>
      expect(stepUp).toHaveBeenCalledWith({ code: "123456" }),
    );
    await waitFor(() =>
      expect(resumeRequest).toHaveBeenCalledWith("request-step-up-required"),
    );
    expect(createRequest).toHaveBeenCalledTimes(1);
    expect(
      screen.queryByText("errors.remote_access.mfa_required"),
    ).not.toBeInTheDocument();
  });

  it("shows a safe step-up failure with the request ID", async () => {
    const client = await renderTerminal();
    vi.spyOn(client.remoteAccess, "createRequest").mockResolvedValueOnce({
      id: "request-step-up-required",
      status: "awaiting_mfa",
    } as never);
    const resumeRequest = vi.spyOn(client.remoteAccess, "resumeRequest");
    vi.spyOn(client.auth, "stepUp").mockRejectedValueOnce(
      new ApiError(
        {
          code: "MFA_CODE_INVALID",
          message_key: "errors.auth.mfa_code_invalid",
          request_id: "request-invalid-proof",
          retryable: false,
        },
        422,
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "建立会话" }));
    await screen.findByRole("dialog", { name: "验证后建立远程会话" });
    fireEvent.change(screen.getByLabelText(/验证码或恢复码/), {
      target: { value: "654321" },
    });
    fireEvent.click(screen.getByRole("button", { name: "验证并继续" }));

    expect(await screen.findByText(/认证证明无效或已过期/)).toBeVisible();
    expect(screen.getByText(/request-invalid-proof/)).toBeVisible();
    expect(resumeRequest).not.toHaveBeenCalled();
    expect(
      screen.queryByText("errors.auth.mfa_code_invalid"),
    ).not.toBeInTheDocument();
  });
});
