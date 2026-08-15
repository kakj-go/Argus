// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MetricChart } from "./chart";
import { FilterBar, SearchInput } from "./filter";
import { KeyValueGrid } from "./key-value-grid";
import { LocaleProvider } from "./locale";
import { LogViewer } from "./log-viewer";
import { ConfirmDialog, FormDrawer } from "./overlays";
import { PageShell } from "./page";
import { PreviewCommitCard } from "./preview-commit";
import { StatCard } from "./stat-card";
import { StatusBadge } from "./status-badge";
import { TerminalEmulator } from "./terminal";
import { Wizard } from "./wizard";

function Wrapper({ children }: { children: ReactNode }) {
  return <LocaleProvider>{children}</LocaleProvider>;
}

// jsdom has no global afterEach wiring for auto-cleanup, and defaults to en-US.
beforeEach(() => {
  window.localStorage.setItem("argus.locale", "zh-CN");
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
        planHash="abc123"
        risk="dangerous"
        title="Restart nginx"
      >
        summary
      </PreviewCommitCard>,
      { wrapper: Wrapper },
    );
    expect(screen.getByText("Restart nginx")).toBeInTheDocument();
    expect(screen.getByText("abc123")).toBeInTheDocument();
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
    expect(container.querySelector("svg")).toBeInTheDocument();
    expect(screen.getByText("cpu")).toBeInTheDocument();
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
