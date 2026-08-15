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
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ChatMessage, ToolCallTrace } from "@argus/api-client";
import {
  ApiProvider,
  createMockApiClient,
  type MockApiClient,
} from "@argus/api-client";
import { LocaleProvider } from "@argus/ui";
import "../../i18n";
import { ChatMessageItem } from "./message-item";
import { ChatMessageList } from "./message-list";
import { PendingActionCard } from "./pending-action-card";
import { ToolTrace } from "./tool-trace";

// @argus/ui 的 LocaleProvider 依据 localStorage/navigator 判定语言，固定为中文。
window.localStorage.setItem("argus.locale", "zh-CN");

// vite.config 未开启 vitest globals，RTL 的自动 cleanup 不生效，手动清理。
afterEach(cleanup);

function createWrapper(client: MockApiClient) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
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

function login(client: MockApiClient) {
  return client.auth.login({ username: "root", password: "123456" });
}

const NOW = new Date().toISOString();

const TOOL_CALLS: ToolCallTrace[] = [
  {
    callId: "call-1",
    toolName: "host.resolve_context",
    status: "success",
    startedAt: NOW,
    durationMs: 320,
    summary: "调用成功",
  },
  {
    callId: "call-2",
    toolName: "host.test_connection",
    status: "running",
    startedAt: NOW,
  },
];

function makeMessage(patch: Partial<ChatMessage>): ChatMessage {
  return {
    id: "msg-t1",
    conversationId: "conv-1",
    role: "assistant",
    content: "",
    createdAt: NOW,
    ...patch,
  };
}

describe("ToolTrace", () => {
  it("默认折叠，点击展开后展示工具名称与耗时", () => {
    render(<ToolTrace toolCalls={TOOL_CALLS} />);
    expect(screen.queryByText("host.resolve_context")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "展开工具调用" }));
    expect(screen.getByText("host.resolve_context")).toBeInTheDocument();
    expect(screen.getByText("host.test_connection")).toBeInTheDocument();
    expect(screen.getByText("320ms")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "收起工具调用" }));
    expect(screen.queryByText("host.resolve_context")).not.toBeInTheDocument();
  });
});

describe("ChatMessageItem", () => {
  it("渲染用户消息与 AI 消息（含折叠的工具 trace）", () => {
    const client = createMockApiClient({ persist: false, delay: 0 });
    const wrapper = createWrapper(client);
    render(
      <ChatMessageItem
        message={makeMessage({
          id: "msg-u1",
          role: "user",
          content: "帮我新增一台 Web 主机",
        })}
      />,
      { wrapper },
    );
    expect(screen.getByTestId("chat-message-user")).toHaveTextContent(
      "帮我新增一台 Web 主机",
    );

    render(
      <ChatMessageItem
        message={makeMessage({
          id: "msg-a1",
          content: "已完成目标解析与连接测试。",
          toolCalls: TOOL_CALLS,
        })}
      />,
      { wrapper },
    );
    expect(screen.getByTestId("chat-message-assistant")).toHaveTextContent(
      "已完成目标解析与连接测试。",
    );
    expect(screen.getByTestId("tool-trace")).toBeInTheDocument();
  });
});

describe("ChatMessageList", () => {
  it("查询数据落地时不重复显示乐观消息和流式消息", () => {
    const userMessage = makeMessage({
      id: "msg-user-persisted",
      role: "user",
      content: "主机容量表",
    });
    const assistantMessage = makeMessage({
      id: "msg-assistant-persisted",
      content: "已创建交互卡片草稿",
    });

    render(
      <ChatMessageList
        messages={[userMessage, assistantMessage]}
        pendingUser={{ ...userMessage, id: "local-user-1" }}
        streaming={assistantMessage}
      />,
    );

    expect(screen.getAllByText("主机容量表")).toHaveLength(1);
    expect(screen.getAllByText("已创建交互卡片草稿")).toHaveLength(1);
  });
});

describe("PendingActionCard", () => {
  it("[确认执行] 直接调 approvals.confirm，状态原地流转到成功", async () => {
    const client = createMockApiClient({
      persist: false,
      delay: 0,
      stepDelay: 10,
    });
    await login(client);
    const action = await client.approvals.preview({
      tool: "host.create",
      title: "新增主机",
      conversationId: "conv-1",
      params: { name: "host-test-x", address: "10.0.0.99" },
    });
    const confirmSpy = vi.spyOn(client.approvals, "confirm");

    render(
      <PendingActionCard
        card={{
          id: "cardi-t1",
          interactiveCardId: "cs-host-create-confirm",
          version: "3.0.1",
          title: "新增主机确认",
          pendingActionRef: action.actionRef,
          actionBindingId: "cab-t1",
        }}
      />,
      { wrapper: createWrapper(client) },
    );

    // 预览加载完成：标题、Diff、计划哈希可见
    await screen.findByText("新增主机");
    expect(screen.getByText(/resource\.host host-test-x/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
    await waitFor(() =>
      expect(confirmSpy).toHaveBeenCalledWith(action.actionRef),
    );
    // mock 任务步骤按 stepDelay 推进，轮询后进入成功终态
    await screen.findByText(/已创建主机 host-test-x/, undefined, {
      timeout: 4_000,
    });
    expect(screen.getByText("查看任务")).toBeInTheDocument();
  });

  it("[取消] 调 approvals.cancel，状态流转为已取消", async () => {
    const client = createMockApiClient({ persist: false, delay: 0 });
    await login(client);
    const action = await client.approvals.preview({
      tool: "host.create",
      title: "新增主机",
      conversationId: "conv-1",
      params: { name: "host-test-y", address: "10.0.0.98" },
    });
    const cancelSpy = vi.spyOn(client.approvals, "cancel");

    render(
      <PendingActionCard
        card={{
          id: "cardi-t2",
          interactiveCardId: "cs-host-create-confirm",
          version: "3.0.1",
          pendingActionRef: action.actionRef,
        }}
      />,
      { wrapper: createWrapper(client) },
    );

    await screen.findByText("新增主机");
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    await waitFor(() =>
      expect(cancelSpy).toHaveBeenCalledWith(action.actionRef),
    );
    await screen.findByText("操作已取消");
  });
});
