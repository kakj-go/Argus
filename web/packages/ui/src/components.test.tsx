// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { type ReactNode, useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MetricChart } from "./chart";
import { DateTimePicker } from "./date-time-picker";
import { FilterBar, SearchInput } from "./filter";
import { Field, Input } from "./form";
import { KeyValueGrid } from "./key-value-grid";
import { LocaleProvider } from "./locale";
import { LogViewer } from "./log-viewer";
import { ConfirmDialog, FormDrawer } from "./overlays";
import { PageShell } from "./page";
import { PreviewCommitCard } from "./preview-commit";
import { Select } from "./select";
import { StatCard } from "./stat-card";
import { StatusBadge } from "./status-badge";
import { TerminalEmulator } from "./terminal";
import { Wizard } from "./wizard";
import { ResourceAuthorizationDualList } from "./dual-list";

function Wrapper({ children }: { children: ReactNode }) {
  return <LocaleProvider>{children}</LocaleProvider>;
}

// jsdom has no global afterEach wiring for auto-cleanup, and defaults to en-US.
beforeEach(() => {
  window.localStorage.setItem("argus.locale", "zh-CN");
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
      unobserve() {}
    },
  );
});
afterEach(cleanup);

describe("PageShell", () => {
  it("renders title, breadcrumbs and actions", () => {
    render(
      <PageShell
        actions={<button>New</button>}
        breadcrumbs={[{ label: "Home", href: "/" }, { label: "Hosts" }]}
        description="All machines"
        title="Hosts"
      >
        content
      </PageShell>,
      { wrapper: Wrapper },
    );
    expect(screen.getByRole("heading", { name: "Hosts" })).toBeInTheDocument();
    expect(screen.getByText("Home")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New" })).toBeInTheDocument();
  });
});

describe("FilterBar", () => {
  it("renders search, filters and refresh; calls handlers", () => {
    const onSearch = vi.fn();
    const onFilter = vi.fn();
    const onRefresh = vi.fn();
    render(
      <FilterBar
        filters={[
          {
            allLabel: "All",
            key: "status",
            onChange: onFilter,
            options: [{ label: "Online", value: "online" }],
            value: "",
          },
        ]}
        onRefresh={onRefresh}
        search={{ onChange: onSearch, value: "" }}
      />,
      { wrapper: Wrapper },
    );
    fireEvent.change(screen.getByRole("searchbox"), {
      target: { value: "web" },
    });
    expect(onSearch).toHaveBeenCalledWith("web");
    fireEvent.click(screen.getByRole("combobox"));
    fireEvent.click(screen.getByRole("option", { name: "Online" }));
    expect(onFilter).toHaveBeenCalledWith("online");
    fireEvent.click(screen.getByRole("button", { name: "刷新" }));
    expect(onRefresh).toHaveBeenCalled();
  });
});

describe("SearchInput", () => {
  it("renders a search input", () => {
    render(<SearchInput placeholder="find" />, { wrapper: Wrapper });
    expect(screen.getByPlaceholderText("find")).toBeInTheDocument();
  });
});

describe("DateTimePicker", () => {
  it("opens when the input text is clicked and keeps the local value format", () => {
    const onChange = vi.fn();
    const { container } = render(
      <DateTimePicker
        aria-label="Expires at"
        onChange={onChange}
        type="datetime-local"
        value="2026-08-26T23:06"
      />,
      { wrapper: Wrapper },
    );
    const input = screen.getByRole("textbox", { name: "Expires at" });
    fireEvent.click(input);
    expect(container.querySelector(".react-datepicker")).toBeInTheDocument();
  });
});

describe("ConfirmDialog", () => {
  it("renders and fires confirm", () => {
    const onConfirm = vi.fn();
    render(
      <ConfirmDialog
        danger
        onConfirm={onConfirm}
        onOpenChange={() => {}}
        open
        title="Delete host?"
      />,
      { wrapper: Wrapper },
    );
    fireEvent.click(screen.getByRole("button", { name: "确认" }));
    expect(onConfirm).toHaveBeenCalled();
  });

  it("closes with Escape and restores focus to the trigger", async () => {
    function Harness() {
      const [open, setOpen] = useState(false);
      return (
        <>
          <button onClick={() => setOpen(true)}>Open dialog</button>
          <ConfirmDialog
            onConfirm={() => {}}
            onOpenChange={setOpen}
            open={open}
            title="Dialog"
          />
        </>
      );
    }
    render(<Harness />, { wrapper: Wrapper });
    const trigger = screen.getByRole("button", { name: "Open dialog" });
    trigger.focus();
    fireEvent.click(trigger);
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    fireEvent.keyDown(document.activeElement ?? document.body, {
      key: "Escape",
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(trigger).toHaveFocus());
  });
});

describe("FormDrawer", () => {
  it("renders form content and submit button", () => {
    render(
      <FormDrawer onOpenChange={() => {}} open title="New host">
        <input aria-label="hostname" />
      </FormDrawer>,
      { wrapper: Wrapper },
    );
    expect(screen.getByText("New host")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "提交" })).toBeInTheDocument();
  });

  it("submits through the native form", () => {
    const onSubmit = vi.fn();
    render(
      <FormDrawer
        onOpenChange={() => {}}
        onSubmit={onSubmit}
        open
        title="New host"
      >
        <Input aria-label="hostname" />
      </FormDrawer>,
      { wrapper: Wrapper },
    );
    fireEvent.submit(screen.getByRole("dialog", { name: "New host" }));
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });
});

describe("ResourceAuthorizationDualList", () => {
  const labels = {
    host: "Host",
    kubernetes: "Kubernetes",
    searchPlaceholder: "搜索资源",
    available: "未授权",
    authorized: "已授权",
    inherited: "继承授权",
    moveSelected: "批量移动",
    moveAll: "全部移动",
    removeSelected: "批量移除",
    removeAll: "全部移除",
    previousPage: "上一页",
    nextPage: "下一页",
  };

  it("moves multiple selected resources and protects inherited grants", () => {
    function Harness() {
      const [value, setValue] = useState({
        host: ["h2"],
        kubernetes_cluster: [] as string[],
      });
      return (
        <>
          <ResourceAuthorizationDualList
            hosts={[
              { id: "h1", label: "Host 1" },
              {
                id: "h2",
                label: "Host 2",
                inherited: true,
                source: "部门：平台",
              },
            ]}
            clusters={[]}
            labels={labels}
            value={value}
            onChange={setValue}
          />
          <output>{value.host.join(",")}</output>
        </>
      );
    }
    render(<Harness />, { wrapper: Wrapper });
    const checks = screen.getAllByRole("checkbox");
    fireEvent.click(checks[0]!);
    fireEvent.click(screen.getByRole("button", { name: "批量移动" }));
    expect(screen.getByText("已授权 (2)")).toBeInTheDocument();
    expect(screen.getByText("部门：平台")).toBeInTheDocument();
  });

  it("paginates each side with a stable page size", () => {
    const hosts = Array.from({ length: 21 }, (_, index) => ({
      id: `h${index}`,
      label: `Host ${index}`,
    }));
    render(
      <ResourceAuthorizationDualList
        hosts={hosts}
        clusters={[]}
        labels={labels}
        value={{ host: [], kubernetes_cluster: [] }}
        onChange={() => {}}
      />,
      { wrapper: Wrapper },
    );
    expect(screen.getByText("1 / 2")).toBeInTheDocument();
    fireEvent.click(screen.getAllByRole("button", { name: "下一页" })[0]!);
    expect(screen.getByText("Host 20")).toBeInTheDocument();
  });
});

describe("Field", () => {
  it("associates validation errors with the input", () => {
    render(
      <Field
        error="Hostname is required"
        label="Hostname"
        requirement="required"
      >
        <Input />
      </Field>,
      { wrapper: Wrapper },
    );
    const input = screen.getByRole("textbox", { name: "Hostname" });
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(input).toHaveAttribute("aria-required", "true");
    expect(input).toBeRequired();
    expect(screen.getByText("*")).toHaveAttribute("aria-hidden", "true");
    const messageId = input.getAttribute("aria-describedby");
    expect(messageId).toBeTruthy();
    expect(document.getElementById(messageId!)).toHaveTextContent(
      "Hostname is required",
    );
  });

  it("does not render a marker for optional fields", () => {
    render(
      <Field label="Notes" requirement="optional">
        <Input />
      </Field>,
      { wrapper: Wrapper },
    );
    expect(screen.queryByText("*")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Notes")).not.toBeRequired();
  });

  it("associates required and error semantics with Select", () => {
    render(
      <Field
        error="Choose an environment"
        label="Environment"
        requirement="required"
      >
        <Select
          onValueChange={() => {}}
          options={[{ label: "Production", value: "production" }]}
          value=""
        />
      </Field>,
      { wrapper: Wrapper },
    );
    const select = screen.getByRole("combobox", { name: "Environment" });
    expect(select).toHaveAttribute("aria-required", "true");
    expect(select).toHaveAttribute("aria-invalid", "true");
    expect(
      document.getElementById(select.getAttribute("aria-describedby")!),
    ).toHaveTextContent("Choose an environment");
  });

  it("labels composite controls as a group without duplicating control ids", () => {
    render(
      <Field controlMode="group" label="Port range" requirement="optional">
        <Input aria-label="Start port" />
        <Input aria-label="End port" />
      </Field>,
      { wrapper: Wrapper },
    );
    const group = screen.getByRole("group", { name: "Port range" });
    const start = screen.getByRole("textbox", { name: "Start port" });
    const end = screen.getByRole("textbox", { name: "End port" });
    const controls = [start, end];
    expect(group).toContainElement(controls[0]!);
    expect(controls[0]!).not.toHaveAttribute("id");
    expect(controls[1]!).not.toHaveAttribute("id");
    expect(controls[0]!).not.toHaveAttribute("aria-labelledby");
    expect(controls[1]!).not.toHaveAttribute("aria-labelledby");
  });
});

describe("StatusBadge", () => {
  it("renders tone and pulse dot", () => {
    render(
      <StatusBadge pulse tone="success">
        Online
      </StatusBadge>,
      { wrapper: Wrapper },
    );
    expect(screen.getByText("Online")).toBeInTheDocument();
  });
});

describe("PreviewCommitCard", () => {
  it("shows countdown, confirm/cancel, and switches to result state", () => {
    const onConfirm = vi.fn();
    const { rerender } = render(
      <PreviewCommitCard
        affected={[{ name: "web-01", detail: "restart" }]}
        expiresAt={Date.now() + 60_000}
        onConfirm={onConfirm}
        risk="dangerous"
        title="Restart nginx"
      >
        summary
      </PreviewCommitCard>,
      { wrapper: Wrapper },
    );
    expect(screen.getByText("Restart nginx")).toBeInTheDocument();
    expect(screen.getByText("web-01")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
    expect(onConfirm).toHaveBeenCalled();
    rerender(
      <LocaleProvider>
        <PreviewCommitCard risk="read" status="success" title="Restart nginx" />
      </LocaleProvider>,
    );
    expect(screen.getByText("执行成功")).toBeInTheDocument();
  });

  it("auto-expires when expiresAt is in the past", () => {
    render(
      <PreviewCommitCard
        expiresAt={Date.now() - 1000}
        risk="write"
        title="Old plan"
      />,
      { wrapper: Wrapper },
    );
    expect(screen.getByText("预览已过期")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "确认执行" })).toBeNull();
  });
});

describe("MetricChart", () => {
  it("renders line and bar charts with legend", () => {
    const { container, rerender } = render(
      <MetricChart
        labels={["a", "b", "c"]}
        series={[{ name: "cpu", points: [1, 2, 3] }]}
        showLegend
      />,
      { wrapper: Wrapper },
    );
    expect(screen.getByRole("img", { name: "cpu" })).toBeInTheDocument();
    expect(container.querySelector(".argus-chart__legend")).toHaveTextContent(
      "cpu",
    );
    rerender(
      <LocaleProvider>
        <MetricChart series={[{ name: "mem", points: [3, 2, 1] }]} type="bar" />
      </LocaleProvider>,
    );
    expect(container.querySelectorAll("rect").length).toBe(3);
  });
});

describe("TerminalEmulator", () => {
  it("renders playback lines and echoes typed commands", () => {
    const onCommand = vi.fn();
    render(
      <TerminalEmulator
        host="web-01"
        lines={[
          { content: "ok", kind: "stdout" },
          { content: "boom", kind: "stderr" },
        ]}
        onCommand={onCommand}
        startedAt={Date.now() - 5000}
      />,
      { wrapper: Wrapper },
    );
    expect(screen.getByText("web-01")).toBeInTheDocument();
    expect(screen.getByText("boom")).toBeInTheDocument();
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "ls" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onCommand).toHaveBeenCalledWith("ls");
    expect(screen.getByText("ls")).toBeInTheDocument();
  });

  it("hides the input in read-only playback mode", () => {
    render(<TerminalEmulator lines={[]} readOnly />, { wrapper: Wrapper });
    expect(screen.queryByRole("textbox")).toBeNull();
  });
});

describe("Wizard", () => {
  it("renders steps and blocks next when canNext is false", () => {
    render(
      <Wizard
        canNext={false}
        current={0}
        onNext={() => {}}
        steps={[
          { id: "a", title: "Basic" },
          { id: "b", title: "Confirm" },
        ]}
      >
        step body
      </Wizard>,
      { wrapper: Wrapper },
    );
    expect(screen.getByText("step body")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "下一步" })).toBeDisabled();
  });

  it("shows submit on the last step", () => {
    const onSubmit = vi.fn();
    render(
      <Wizard
        current={1}
        onSubmit={onSubmit}
        steps={[
          { id: "a", title: "Basic" },
          { id: "b", title: "Confirm" },
        ]}
      >
        done
      </Wizard>,
      { wrapper: Wrapper },
    );
    fireEvent.click(screen.getByRole("button", { name: "提交" }));
    expect(onSubmit).toHaveBeenCalled();
  });
});

describe("KeyValueGrid", () => {
  it("renders items in a grid", () => {
    render(
      <KeyValueGrid columns={3} items={[{ label: "IP", value: "10.0.0.1" }]} />,
      { wrapper: Wrapper },
    );
    expect(screen.getByText("10.0.0.1")).toBeInTheDocument();
  });
});

describe("LogViewer", () => {
  it("renders lines with level coloring hooks", () => {
    const { container } = render(
      <LogViewer
        lines={[
          { content: "started", level: "info", timestamp: "10:00" },
          { content: "disk full", level: "error" },
        ]}
      />,
      { wrapper: Wrapper },
    );
    expect(screen.getByText("disk full")).toBeInTheDocument();
    expect(container.querySelector(".is-error")).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();
  });
});

describe("StatCard", () => {
  it("renders label, value and detail", () => {
    render(<StatCard detail="+2" label="Hosts" tone="accent" value="42" />, {
      wrapper: Wrapper,
    });
    expect(screen.getByText("Hosts")).toBeInTheDocument();
    expect(screen.getByText("42")).toBeInTheDocument();
  });
});
