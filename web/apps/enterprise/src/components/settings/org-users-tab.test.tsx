// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  ApiProvider,
  createMockApiClient,
  type MockApiClient,
} from "@argus/api-client";
import { useEnterpriseAuthStore } from "@argus/auth";
import { LocaleProvider } from "@argus/ui";
import "../../i18n";
import { OrgUsersTab } from "./org-users-tab";

beforeEach(() => {
  window.localStorage.setItem("argus.locale", "zh-CN");
});

afterEach(() => {
  cleanup();
  useEnterpriseAuthStore.getState().clear();
});

function createWrapper(client: MockApiClient, queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <LocaleProvider>
        <ApiProvider client={client}>
          <QueryClientProvider client={queryClient}>
            {children}
          </QueryClientProvider>
        </ApiProvider>
      </LocaleProvider>
    );
  };
}

describe("OrgUsersTab", () => {
  it("does not reuse the raw user directory cache as enriched table rows", async () => {
    const client = createMockApiClient({ persist: false, delay: 0 });
    const session = await client.auth.login({
      username: "root",
      password: "123456",
    });
    useEnterpriseAuthStore.setState({
      status: "authenticated",
      session,
    });

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false, staleTime: Number.POSITIVE_INFINITY },
      },
    });
    queryClient.setQueryData(["org", "users"], await client.org.listUsers());

    render(<OrgUsersTab />, {
      wrapper: createWrapper(client, queryClient),
    });

    expect(
      await screen.findByRole("row", {
        name: /企业超级管理员.*企业管理员/,
      }),
    ).toBeVisible();
  });

  it("updates direct roles from the member edit drawer", async () => {
    const client = createMockApiClient({ persist: false, delay: 0 });
    const session = await client.auth.login({
      username: "root",
      password: "123456",
    });
    useEnterpriseAuthStore.setState({
      status: "authenticated",
      session,
    });
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    render(<OrgUsersTab />, {
      wrapper: createWrapper(client, queryClient),
    });

    const row = await screen.findByRole("row", {
      name: /企业超级管理员.*企业管理员/,
    });
    fireEvent.click(within(row).getByRole("button", { name: "编辑" }));

    const drawer = await screen.findByRole("dialog", { name: "编辑成员" });
    const viewerRole = await within(drawer).findByRole("button", {
      name: "资源查看者",
    });
    fireEvent.click(viewerRole);
    fireEvent.click(within(drawer).getByRole("button", { name: "提交" }));

    await waitFor(() => expect(drawer).not.toBeVisible());
    await expect(
      client.org.getUserRoleAssignments("u-root"),
    ).resolves.toMatchObject({
      direct_role_ids: expect.arrayContaining(["role-ea", "role-pv"]),
    });
  });
});
