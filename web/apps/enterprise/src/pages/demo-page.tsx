import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  AlertCircle,
  Bell,
  Check,
  ChevronDown,
  Copy,
  Info,
  Plus,
  Search,
  Settings2,
  ShieldAlert,
  Sparkles,
  Trash2,
  TriangleAlert,
} from "lucide-react";
import {
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CheckItem,
  CodeBlock,
  ConfirmDialog,
  DataTable,
  DescriptionList,
  Dialog,
  DiffViewer,
  Divider,
  Dropdown,
  EmptyState,
  Field,
  FilterBar,
  FormDrawer,
  Input,
  KeyValueGrid,
  LogViewer,
  MenuItem,
  Metric,
  MetricChart,
  PageShell,
  Pagination,
  PreviewCommitCard,
  Progress,
  Skeleton,
  Spinner,
  StatCard,
  StatusBadge,
  Switch,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  TerminalEmulator,
  Textarea,
  Timeline,
  Tooltip,
  Wizard,
  useUiText,
  useTheme,
} from "@argus/ui";

const demoNav = [
  { id: "foundations", key: "demo.foundations" },
  { id: "actions", key: "demo.actions" },
  { id: "forms", key: "demo.forms" },
  { id: "navigation", key: "demo.navigation" },
  { id: "data", key: "demo.data" },
  { id: "feedback", key: "demo.feedback" },
  { id: "patterns", key: "demo.patterns" },
  { id: "ai-cards", key: "demo.aiCards" },
];

function DemoSection({
  id,
  title,
  description,
  children,
}: {
  id: string;
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <section className="demo-section" id={id}>
      <header>
        <div>
          <span>0{demoNav.findIndex((item) => item.id === id) + 1}</span>
          <h2>{title}</h2>
        </div>
        <p>{description}</p>
      </header>
      {children}
    </section>
  );
}
function DemoBlock({
  title,
  description,
  children,
  className = "",
}: {
  title: string;
  description?: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <Card className={`demo-block ${className}`}>
      <CardHeader description={description} title={title} />
      <CardContent>{children}</CardContent>
    </Card>
  );
}

export function DemoPage() {
  const { t } = useTranslation();
  const text = useUiText();
  const { resolvedTheme } = useTheme();
  const [enabled, setEnabled] = useState(true);
  const [page, setPage] = useState(1);
  const [demoSearch, setDemoSearch] = useState("");
  const [demoEnv, setDemoEnv] = useState("");
  const [demoStatus, setDemoStatus] = useState("");
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [wizardStep, setWizardStep] = useState(0);
  return (
    <div className="demo-page">
      <aside className="demo-nav">
        <div>
          <b>Argus UI</b>
          <span>v0.1.0 · {resolvedTheme}</span>
        </div>
        {demoNav.map((item) => (
          <a href={`#${item.id}`} key={item.id}>
            {t(item.key)}
          </a>
        ))}
        <div className="demo-nav__meta">
          <span>
            <i /> React 19
          </span>
          <span>
            <i /> Radix UI
          </span>
          <span>
            <i /> Tailwind + CVA
          </span>
        </div>
      </aside>
      <div className="demo-content">
        <header className="demo-hero">
          <Badge tone="accent">{t("demo.badge")}</Badge>
          <h1>{t("demo.title")}</h1>
          <p>{t("demo.description")}</p>
          <div>
            <Button variant="primary">
              <Sparkles size={15} />
              {t("demo.viewCards")}
            </Button>
            <Button variant="secondary">
              <Copy size={14} />
              {t("demo.copyToken")}
            </Button>
          </div>
        </header>
        <DemoSection
          description={text(
            "深色暖灰基底，保留 demo.html 的沉稳气质；组件值全部引用共享 Design Token。",
            "A calm warm-gray foundation inspired by demo.html; every component value references shared design tokens.",
          )}
          id="foundations"
          title={t("demo.foundations")}
        >
          <div className="demo-grid two">
            <DemoBlock title={text("语义颜色", "Semantic colors")}>
              <div className="swatches">
                {[
                  ["Canvas", "--bg-canvas"],
                  ["Surface", "--bg-surface"],
                  ["Elevated", "--bg-elevated"],
                  ["Accent", "--accent"],
                  ["Success", "--success"],
                  ["Warning", "--warning"],
                  ["Danger", "--danger"],
                  ["Info", "--info"],
                ].map(([name, token]) => (
                  <div key={name}>
                    <i style={{ background: `var(${token})` }} />
                    <span>
                      <b>{name}</b>
                      <code>{token}</code>
                    </span>
                  </div>
                ))}
              </div>
            </DemoBlock>
            <DemoBlock title={text("文字层级", "Type hierarchy")}>
              <div className="type-scale">
                <div className="display">
                  {text(
                    "复杂系统，也应一目了然。",
                    "Complex systems should still feel clear.",
                  )}
                </div>
                <div className="heading">
                  {text(
                    "企业资源与运行状态",
                    "Enterprise resources and runtime",
                  )}
                </div>
                <div className="body">
                  {text(
                    "正文用于清晰描述业务状态、风险与下一步动作。",
                    "Body copy clearly explains business state, risk, and the next action.",
                  )}
                </div>
                <div className="caption">
                  {text("辅助信息", "Supporting detail")} · 2026-08-15 10:32:04
                </div>
                <code>tool_call_id: tc_01JAK2</code>
              </div>
            </DemoBlock>
          </div>
        </DemoSection>
        <DemoSection
          description={text(
            "按钮由 CVA 管理尺寸和语义变体；状态徽章必须同时具有文字或图标。",
            "CVA manages button sizes and semantic variants; status badges always include text or an icon.",
          )}
          id="actions"
          title={t("demo.actions")}
        >
          <div className="demo-grid two">
            <DemoBlock title="Button">
              <div className="demo-stack">
                <div>
                  <Button variant="primary">
                    {text("主要操作", "Primary")}
                  </Button>
                  <Button variant="secondary">
                    {text("次要操作", "Secondary")}
                  </Button>
                  <Button variant="ghost">{text("幽灵按钮", "Ghost")}</Button>
                  <Button variant="danger">
                    <Trash2 size={14} />
                    {text("危险操作", "Danger")}
                  </Button>
                </div>
                <div>
                  <Button size="sm" variant="primary">
                    <Plus size={13} />
                    {text("小按钮", "Small")}
                  </Button>
                  <Button loading>{text("处理中", "Processing")}</Button>
                  <Button disabled>{text("禁用状态", "Disabled")}</Button>
                  <Tooltip
                    content={text(
                      "图标按钮需要可访问名称",
                      "Icon buttons need an accessible name",
                    )}
                  >
                    <Button
                      aria-label={text("通知", "Notifications")}
                      size="icon"
                      variant="secondary"
                    >
                      <Bell size={15} />
                    </Button>
                  </Tooltip>
                </div>
              </div>
            </DemoBlock>
            <DemoBlock title="StatusBadge">
              <div className="badge-wall">
                <Badge tone="neutral">{text("默认", "Default")}</Badge>
                <Badge dot tone="success">
                  {text("在线", "Online")}
                </Badge>
                <Badge dot tone="warning">
                  {text("队列积压", "Queue backlog")}
                </Badge>
                <Badge dot tone="danger">
                  {text("数据中断", "Data interrupted")}
                </Badge>
                <Badge dot tone="info">
                  {text("运行中", "Running")}
                </Badge>
                <Badge tone="accent">production</Badge>
              </div>
              <Divider label={text("业务示例", "Business examples")} />
              <div className="badge-wall">
                <Badge tone="success">
                  <Check size={11} />
                  {text("监控中", "Monitoring")}
                </Badge>
                <Badge tone="warning">
                  <TriangleAlert size={11} />
                  {text("中风险", "Medium risk")}
                </Badge>
                <Badge tone="danger">
                  <AlertCircle size={11} />
                  {text("审批拒绝", "Approval denied")}
                </Badge>
              </div>
              <Divider
                label={text("StatusBadge 全 tone", "StatusBadge tones")}
              />
              <div className="badge-wall">
                <StatusBadge tone="neutral">
                  {text("未知", "Unknown")}
                </StatusBadge>
                <StatusBadge tone="accent">production</StatusBadge>
                <StatusBadge pulse tone="success">
                  {text("在线", "Online")}
                </StatusBadge>
                <StatusBadge tone="warning">
                  {text("降级", "Degraded")}
                </StatusBadge>
                <StatusBadge tone="danger">
                  {text("离线", "Offline")}
                </StatusBadge>
                <StatusBadge pulse tone="info">
                  {text("接入中", "Onboarding")}
                </StatusBadge>
              </div>
            </DemoBlock>
          </div>
        </DemoSection>
        <DemoSection
          description={text(
            "输入组件为 React Hook Form + Zod 预留一致的标签、帮助和错误状态。",
            "Input components expose consistent labels, help text, and errors for React Hook Form and Zod.",
          )}
          id="forms"
          title={t("demo.forms")}
        >
          <div className="demo-grid two">
            <DemoBlock title={text("输入控件", "Input controls")}>
              <div className="form-demo">
                <Field
                  hint={text(
                    "名称在企业内唯一",
                    "Unique within the enterprise",
                  )}
                  label={text("Connector 名称", "Connector name")}
                >
                  <Input
                    defaultValue={text(
                      "上海机房堡垒机-01",
                      "Shanghai DC Bastion-01",
                    )}
                  />
                </Field>
                <Field
                  error={text(
                    "地址必须使用 HTTPS",
                    "The address must use HTTPS",
                  )}
                  label={text("Gateway 地址", "Gateway address")}
                >
                  <Input defaultValue="http://gateway.internal" />
                </Field>
                <Field
                  hint={text(
                    "不要在此输入密码或私钥",
                    "Do not enter passwords or private keys here",
                  )}
                  label={text("备注", "Notes")}
                >
                  <Textarea
                    placeholder={text(
                      "描述用途与网络范围",
                      "Describe purpose and network scope",
                    )}
                  />
                </Field>
              </div>
            </DemoBlock>
            <DemoBlock title={text("选择与开关", "Selection and switches")}>
              <div className="form-demo">
                <div className="demo-row">
                  <span>
                    <b>{text("启用 Provider", "Enable provider")}</b>
                    <small>
                      {text(
                        "停用后现有 Alias 会自动降级",
                        "Existing aliases fall back automatically when disabled",
                      )}
                    </small>
                  </span>
                  <Switch
                    checked={enabled}
                    label={text("启用 Provider", "Enable provider")}
                    onChange={setEnabled}
                  />
                </div>
                <CheckItem checked>Metrics</CheckItem>
                <CheckItem checked>Logs</CheckItem>
                <CheckItem checked={false}>Traces</CheckItem>
                <Divider />
                <MenuItem
                  active
                  end={
                    <Badge tone="accent">{text("推荐", "Recommended")}</Badge>
                  }
                >
                  host-basic
                </MenuItem>
                <MenuItem
                  end={
                    <span className="muted-copy">
                      {text("12 项", "12 items")}
                    </span>
                  }
                >
                  system-logs
                </MenuItem>
              </div>
            </DemoBlock>
          </div>
          <DemoBlock
            description={text(
              "多步表单统一上一步/下一步/提交；canNext 控制步骤门禁。",
              "Multi-step forms share back/next/submit; canNext gates each step.",
            )}
            title="Wizard"
          >
            <Wizard
              canNext
              current={wizardStep}
              onBack={() => setWizardStep((value) => Math.max(0, value - 1))}
              onNext={() => setWizardStep((value) => value + 1)}
              onSubmit={() => setWizardStep(0)}
              steps={[
                {
                  id: "mode",
                  title: text("连接方式", "Connection"),
                  description: text(
                    "经堡垒机或公网直连",
                    "Via bastion or direct",
                  ),
                },
                {
                  id: "info",
                  title: text("主机信息", "Host info"),
                  description: text(
                    "地址、协议与凭证",
                    "Address, protocol, credential",
                  ),
                },
                {
                  id: "test",
                  title: text("连接测试", "Connection test"),
                  description: text("验证后生成预览", "Verify, then preview"),
                },
              ]}
              submitLabel={text("生成预览", "Generate preview")}
            >
              {wizardStep === 0 && (
                <MenuItem active>
                  SSH {text("经堡垒机", "via bastion")}
                </MenuItem>
              )}
              {wizardStep === 1 && (
                <Field label={text("地址", "Address")}>
                  <Input defaultValue="10.0.1.5" />
                </Field>
              )}
              {wizardStep === 2 && (
                <Alert
                  description={text("延迟 148ms", "Latency 148ms")}
                  tone="success"
                  title={text("测试通过，可生成预览", "Test passed")}
                />
              )}
            </Wizard>
          </DemoBlock>
        </DemoSection>
        <DemoSection
          description={text(
            "Tabs、Dialog、Dropdown 与 Tooltip 基于 Radix 原语，统一焦点管理和键盘交互。",
            "Tabs, dialogs, dropdowns, and tooltips use Radix primitives for consistent focus and keyboard behavior.",
          )}
          id="navigation"
          title={t("demo.navigation")}
        >
          <div className="demo-grid two">
            <DemoBlock title="Tabs">
              <Tabs defaultValue="overview">
                <TabsList>
                  <TabsTrigger value="overview">
                    {text("概览", "Overview")}
                  </TabsTrigger>
                  <TabsTrigger value="config">
                    {text("配置", "Configuration")}
                  </TabsTrigger>
                  <TabsTrigger value="events">
                    {text("事件 3", "Events 3")}
                  </TabsTrigger>
                </TabsList>
                <TabsContent value="overview">
                  <Alert
                    description={text(
                      "最近一次健康检查在 8 秒前完成。",
                      "The latest health check completed 8 seconds ago.",
                    )}
                    icon={<Info size={15} />}
                    title={text("Connector 连接正常", "Connector is healthy")}
                  />
                </TabsContent>
                <TabsContent value="config">
                  <CodeBlock
                    code={
                      "connection_epoch: 42\ncapabilities: [ssh, artifact_tunnel]"
                    }
                  />
                </TabsContent>
                <TabsContent value="events">
                  {text(
                    "3 条连接事件均已审计。",
                    "All three connection events were audited.",
                  )}
                </TabsContent>
              </Tabs>
            </DemoBlock>
            <DemoBlock title="Overlay">
              <div className="demo-stack">
                <div>
                  <Dialog
                    description={text(
                      "该示例展示焦点锁定、遮罩和标准底部操作区。",
                      "This example demonstrates focus lock, an overlay, and the standard footer actions.",
                    )}
                    footer={
                      <>
                        <Button variant="ghost">
                          {text("取消", "Cancel")}
                        </Button>
                        <Button variant="primary">
                          {text("确认", "Confirm")}
                        </Button>
                      </>
                    }
                    title={text("创建 Connector", "Create connector")}
                    trigger={
                      <Button variant="primary">
                        <Plus size={14} />
                        {text("打开 Dialog", "Open dialog")}
                      </Button>
                    }
                  >
                    <div className="form-demo">
                      <Field label={text("名称", "Name")}>
                        <Input
                          placeholder={text(
                            "例如：上海机房堡垒机-01",
                            "For example: Shanghai-DC-01",
                          )}
                        />
                      </Field>
                      <Field
                        label={text("允许注册次数", "Allowed registrations")}
                      >
                        <Input defaultValue="1" type="number" />
                      </Field>
                    </div>
                  </Dialog>
                  <Dropdown
                    items={[
                      { label: text("查看详情", "View details") },
                      {
                        label: text("复制资源 ID", "Copy resource ID"),
                        shortcut: "⌘ C",
                      },
                      "separator",
                      {
                        label: text("移除资源", "Remove resource"),
                        danger: true,
                      },
                    ]}
                    trigger={
                      <Button variant="secondary">
                        {text("更多操作", "More actions")}{" "}
                        <ChevronDown size={13} />
                      </Button>
                    }
                  />
                </div>
                <Tooltip
                  content={text(
                    "这是标准 Tooltip，不放置关键业务信息",
                    "This is a standard tooltip; do not place critical business information here",
                  )}
                >
                  <Button variant="ghost">
                    {text("悬停查看说明", "Hover for help")}
                  </Button>
                </Tooltip>
                <Divider label={text("抽屉与确认", "Drawer & confirm")} />
                <div>
                  <Button
                    onClick={() => setDrawerOpen(true)}
                    variant="secondary"
                  >
                    {text("打开 FormDrawer", "Open FormDrawer")}
                  </Button>
                  <Button onClick={() => setConfirmOpen(true)} variant="danger">
                    {text("打开 ConfirmDialog", "Open ConfirmDialog")}
                  </Button>
                </div>
                <FormDrawer
                  description={text(
                    "右侧滑出的标准表单容器，带统一底部操作区。",
                    "A standard right-side form container with a unified footer.",
                  )}
                  onOpenChange={setDrawerOpen}
                  onSubmit={() => setDrawerOpen(false)}
                  open={drawerOpen}
                  title={text("编辑资源", "Edit resource")}
                >
                  <Field label={text("名称", "Name")}>
                    <Input defaultValue="host-web-11" />
                  </Field>
                  <Field
                    hint={text(
                      "每行一个，格式 key=value",
                      "One per line, key=value",
                    )}
                    label={text("标签", "Tags")}
                  >
                    <Textarea rows={3} />
                  </Field>
                </FormDrawer>
                <ConfirmDialog
                  danger
                  confirmLabel={text("确认删除", "Delete")}
                  description={text(
                    "删除后不可恢复，关联任务保留审计记录。",
                    "Deletion is irreversible; related tasks keep audit records.",
                  )}
                  onConfirm={() => setConfirmOpen(false)}
                  onOpenChange={setConfirmOpen}
                  open={confirmOpen}
                  title={text("删除资源", "Delete resource")}
                >
                  <p className="muted-copy">
                    <code>host-web-11（10.0.0.11:22）</code>
                  </p>
                </ConfirmDialog>
              </div>
            </DemoBlock>
          </div>
        </DemoSection>
        <DemoSection
          description={text(
            "密集数据保持稳定对齐；跨页过滤、排序和分页由服务端 Query Binding 完成。",
            "Dense data stays aligned; cross-page filtering, sorting, and pagination are handled by server-side query bindings.",
          )}
          id="data"
          title={t("demo.data")}
        >
          <div className="metric-demo">
            <Metric
              change={text("较昨日 +3.2%", "+3.2% vs yesterday")}
              label={text("摄入速率", "Ingest rate")}
              tone="success"
              unit="k/s"
              value="42.6"
            />
            <Metric
              change={text("预算使用 60%", "60% of budget used")}
              label={text("今日摄入", "Ingested today")}
              tone="warning"
              unit="TB"
              value="1.8"
            />
            <Metric
              change={text("1 个数据中断", "1 data interruption")}
              label={text("接入异常", "Ingest issues")}
              tone="danger"
              value="3"
            />
            <Metric
              change="P95 820ms"
              label={text("模型请求", "Model requests")}
              value="1,842"
            />
          </div>
          <DemoBlock
            title="DataTable + Pagination"
            description={text(
              "资源身份、状态、来源和数据时间必须在详情中可追踪",
              "Resource identity, state, source, and data time must remain traceable in details.",
            )}
          >
            <div className="table-toolbar">
              <div className="filter-search">
                <Search size={15} />
                <Input placeholder={text("搜索资源", "Search resources")} />
              </div>
              <Button size="sm" variant="secondary">
                <Settings2 size={14} />
                {text("列设置", "Columns")}
              </Button>
            </div>
            <DataTable
              columns={[
                { key: "name", header: text("资源", "Resource") },
                { key: "type", header: text("类型", "Type") },
                {
                  key: "environment",
                  header: text("环境", "Environment"),
                  render: (row) => (
                    <Badge
                      tone={
                        row.environment === "production" ? "accent" : "neutral"
                      }
                    >
                      {String(row.environment)}
                    </Badge>
                  ),
                },
                {
                  key: "status",
                  header: text("状态", "Status"),
                  render: (row) => (
                    <Badge
                      dot
                      tone={row.statusKey === "online" ? "success" : "warning"}
                    >
                      {String(row.status)}
                    </Badge>
                  ),
                },
                {
                  key: "updated",
                  header: text("更新时间", "Updated"),
                  align: "right",
                },
              ]}
              data={[
                {
                  name: "host-web-11",
                  type: "Linux Host",
                  environment: "production",
                  status: text("在线", "Online"),
                  statusKey: "online",
                  updated: text("8 秒前", "8 seconds ago"),
                },
                {
                  name: "k8s-prod-east",
                  type: "Kubernetes",
                  environment: "production",
                  status: text("在线", "Online"),
                  statusKey: "online",
                  updated: text("12 秒前", "12 seconds ago"),
                },
                {
                  name: "win-ad-02",
                  type: "Windows Host",
                  environment: "staging",
                  status: text("心跳超时", "Heartbeat timeout"),
                  statusKey: "timeout",
                  updated: text("12 分钟前", "12 minutes ago"),
                },
              ]}
              getRowKey={(row) => String(row.name)}
            />
            <div className="pagination-demo">
              <Pagination onChange={setPage} page={page} totalPages={12} />
            </div>
          </DemoBlock>
          <div className="demo-grid two">
            <DemoBlock title="DescriptionList">
              <DescriptionList
                items={[
                  {
                    label: text("资源 ID", "Resource ID"),
                    value: <code>host_01JAK2</code>,
                  },
                  {
                    label: text("连接方式", "Connection"),
                    value: "SSH via Connector",
                  },
                  {
                    label: text("凭证", "Credential"),
                    value: text(
                      "secret_ref: sec-host-01（不回显）",
                      "secret_ref: sec-host-01 (never revealed)",
                    ),
                  },
                  {
                    label: text("数据来源", "Data source"),
                    value: text(
                      "host.get · 8 秒前",
                      "host.get · 8 seconds ago",
                    ),
                  },
                ]}
              />
            </DemoBlock>
            <DemoBlock title="Code + Diff">
              <DiffViewer
                lines={[
                  { type: "context", content: "metadata:" },
                  { type: "add", content: "  environment: production" },
                  { type: "remove", content: "  replicas: 3" },
                  { type: "add", content: "  replicas: 5" },
                ]}
              />
            </DemoBlock>
          </div>
          <div className="metric-demo">
            <StatCard
              label={text("待我审批", "Awaiting my approval")}
              tone="warning"
              value="2"
            />
            <StatCard label={text("今日已处理", "Handled today")} value="14" />
            <StatCard
              detail={text("较昨日 +5%", "+5% vs yesterday")}
              label={text("在线主机", "Hosts online")}
              tone="success"
              value="128"
            />
            <StatCard
              label={text("失败任务", "Failed tasks")}
              tone="danger"
              value="1"
            />
          </div>
          <DemoBlock
            description={text(
              "SVG 图表，无第三方依赖；悬停显示读数。",
              "Dependency-free SVG charts with hover readouts.",
            )}
            title="MetricChart"
          >
            <div className="demo-grid three">
              <MetricChart
                height={160}
                labels={["10:00", "10:05", "10:10", "10:15", "10:20", "10:25"]}
                series={[
                  {
                    name: "CPU %",
                    points: [42, 45, 38, 55, 49, 61],
                  },
                  {
                    name: text("内存 %", "Memory %"),
                    points: [70, 72, 68, 74, 71, 76],
                  },
                ]}
                showLegend
                type="line"
              />
              <MetricChart
                height={160}
                labels={["10:00", "10:05", "10:10", "10:15", "10:20", "10:25"]}
                series={[
                  {
                    name: text("摄入 k/s", "Ingest k/s"),
                    points: [30, 42, 36, 55, 48, 62],
                  },
                ]}
                type="area"
              />
              <MetricChart
                height={160}
                labels={["10:00", "10:05", "10:10", "10:15", "10:20", "10:25"]}
                series={[
                  {
                    name: text("请求数", "Requests"),
                    points: [420, 460, 390, 520, 480, 540],
                  },
                ]}
                type="bar"
              />
            </div>
          </DemoBlock>
          <div className="demo-grid two">
            <DemoBlock title="KeyValueGrid">
              <KeyValueGrid
                columns={2}
                items={[
                  {
                    label: text("主机名", "Host name"),
                    value: "host-web-11",
                  },
                  {
                    label: text("连接路径", "Connection path"),
                    value: <code>上海机房堡垒机-01 → 10.0.0.11:22</code>,
                  },
                  { label: text("协议", "Protocol"), value: "SSH" },
                  {
                    label: text("凭证", "Credential"),
                    value: text(
                      "sec-ssh-prod（不回显）",
                      "sec-ssh-prod (hidden)",
                    ),
                  },
                ]}
              />
            </DemoBlock>
            <DemoBlock className="no-padding" title="LogViewer">
              <LogViewer
                fileName="collector-install.log"
                height={220}
                lines={[
                  {
                    timestamp: "10:24:08",
                    level: "info",
                    content: "传输安装包 argus-otelcol v24.1.3",
                  },
                  {
                    timestamp: "10:24:11",
                    level: "info",
                    content: "校验 Digest sha256:9f2c…a41d",
                  },
                  {
                    timestamp: "10:24:19",
                    level: "warn",
                    content: "systemd 单元已存在，执行覆盖安装",
                  },
                  {
                    timestamp: "10:24:26",
                    level: "info",
                    content: "健康检查通过 · 已注册到 Edge Gateway",
                  },
                ]}
              />
            </DemoBlock>
          </div>
          <DemoBlock className="no-padding" title="TerminalEmulator">
            <TerminalEmulator
              height={220}
              host="host-web-11"
              lines={[
                { kind: "stdin", content: "systemctl status argus-otelcol" },
                {
                  kind: "stdout",
                  content:
                    "● argus-otelcol.service - Argus OTel Collector\n   Active: active (running) since Fri 10:24:26 CST",
                },
                {
                  kind: "stdin",
                  content: "tail -n 1 /var/log/argus/collector.log",
                },
                { kind: "stdout", content: "health_check: ok" },
              ]}
              protocol="ssh"
              readOnly
              startedAt={Date.now() - 8 * 60_000}
              state="connected"
            />
          </DemoBlock>
        </DemoSection>
        <DemoSection
          description={text(
            "空、错、加载、进度与风险状态明确区分；不会用“无数据”掩盖“无权限”。",
            "Empty, error, loading, progress, and risk states stay distinct; no-data never hides missing permissions.",
          )}
          id="feedback"
          title={t("demo.feedback")}
        >
          <div className="demo-grid two">
            <DemoBlock title="Alerts">
              <div className="form-demo">
                <Alert
                  description={text(
                    "Collector Desired 与 Effective Revision 一致。",
                    "Collector desired and effective revisions match.",
                  )}
                  icon={<Check size={15} />}
                  tone="success"
                  title={text("配置已生效", "Configuration applied")}
                />
                <Alert
                  description={text(
                    "Edge Gateway 持久队列达到 73%，建议检查出口网络。",
                    "The Edge Gateway persistent queue reached 73%; check outbound connectivity.",
                  )}
                  icon={<TriangleAlert size={15} />}
                  tone="warning"
                  title={text("队列持续增长", "Queue keeps growing")}
                />
                <Alert
                  description={text(
                    "当前角色无权读取该资源范围。",
                    "The current role cannot read this resource scope.",
                  )}
                  icon={<ShieldAlert size={15} />}
                  tone="danger"
                  title={text("权限不足", "Insufficient permissions")}
                />
              </div>
            </DemoBlock>
            <DemoBlock title="Progress + Loading">
              <div className="form-demo">
                <Progress
                  label={text("传输 Artifact", "Transfer artifact")}
                  value={62}
                />
                <Progress
                  label={text("配置收敛", "Configuration convergence")}
                  tone="success"
                  value={100}
                />
                <Progress
                  label={text("错误预算", "Error budget")}
                  tone="warning"
                  value={72}
                />
                <Divider />
                <Spinner
                  label={text("正在恢复 Run 状态", "Restoring run state")}
                />
                <Skeleton height={12} />
                <Skeleton height={12} width="72%" />
              </div>
            </DemoBlock>
            <DemoBlock title="Timeline">
              <Timeline
                items={[
                  {
                    title: text("计划检查完成", "Plan check complete"),
                    meta: "10:24:08 · 0.8s",
                    status: "done",
                  },
                  {
                    title: text(
                      "正在安装 OTLP 收集器",
                      "Installing OTLP Collector",
                    ),
                    meta: "step 3 / 5 · 62%",
                    status: "current",
                  },
                  {
                    title: text(
                      "等待健康验证",
                      "Waiting for health validation",
                    ),
                    meta: text("尚未开始", "Not started"),
                    status: "pending",
                  },
                ]}
              />
            </DemoBlock>
            <DemoBlock className="no-padding" title="EmptyErrorState">
              <EmptyState
                action={
                  <Button size="sm" variant="secondary">
                    {text("清除筛选", "Clear filters")}
                  </Button>
                }
                description={text(
                  "当前筛选下没有匹配项，数据权限范围未发生变化。",
                  "No items match the current filters; the data permission scope is unchanged.",
                )}
                title={text("没有匹配的资源", "No matching resources")}
              />
            </DemoBlock>
          </div>
        </DemoSection>
        <DemoSection
          description={text(
            "列表页统一骨架：PageShell 提供标题/描述/操作区，FilterBar 提供搜索、筛选与刷新。",
            "A shared list-page skeleton: PageShell provides title/description/actions; FilterBar provides search, filters, and refresh.",
          )}
          id="patterns"
          title={t("demo.patterns")}
        >
          <DemoBlock className="no-padding" title="PageShell + FilterBar">
            <PageShell
              actions={
                <>
                  <Button size="sm" variant="secondary">
                    {text("导出", "Export")}
                  </Button>
                  <Button size="sm" variant="primary">
                    <Plus size={13} />
                    {text("添加主机", "Add host")}
                  </Button>
                </>
              }
              breadcrumbs={[
                { label: text("资源", "Resources") },
                { label: text("主机", "Hosts") },
              ]}
              description={text(
                "企业内全部主机与其连接路径。",
                "All enterprise hosts and their connection paths.",
              )}
              title={text("主机", "Hosts")}
            >
              <FilterBar
                filters={[
                  {
                    key: "environment",
                    value: demoEnv,
                    allLabel: text("全部环境", "All environments"),
                    options: [
                      { value: "production", label: "production" },
                      { value: "staging", label: "staging" },
                      { value: "development", label: "development" },
                    ],
                    onChange: setDemoEnv,
                  },
                  {
                    key: "status",
                    value: demoStatus,
                    allLabel: text("全部状态", "All statuses"),
                    options: [
                      { value: "online", label: text("在线", "Online") },
                      { value: "offline", label: text("离线", "Offline") },
                      {
                        value: "onboarding",
                        label: text("接入中", "Onboarding"),
                      },
                    ],
                    onChange: setDemoStatus,
                  },
                ]}
                onRefresh={() => {}}
                search={{
                  value: demoSearch,
                  onChange: setDemoSearch,
                  placeholder: text("搜索主机名或 IP…", "Search name or IP…"),
                }}
              />
              <div className="badge-wall" style={{ padding: "4px 2px" }}>
                <span className="muted-copy">
                  {text("当前筛选：", "Active filters: ")}
                  {[
                    demoSearch &&
                      text(`关键词「${demoSearch}」`, `query "${demoSearch}"`),
                    demoEnv,
                    demoStatus,
                  ]
                    .filter(Boolean)
                    .join(" · ") || text("无", "none")}
                </span>
              </div>
            </PageShell>
          </DemoBlock>
        </DemoSection>
        <DemoSection
          description={text(
            "业务卡片展示来源、风险、过期、计划摘要与绑定动作；确认后不再经过模型推理。",
            "Business cards show provenance, risk, expiry, plan summaries, and bound actions; confirmation bypasses further model inference.",
          )}
          id="ai-cards"
          title={t("demo.aiCards")}
        >
          <div className="ai-demo-grid">
            <div>
              <div className="mini-message">
                <span className="agent-avatar">
                  <Sparkles size={14} />
                </span>
                <div>
                  <b>Argus</b>
                  <p>
                    {text(
                      "连接检查通过。请确认下面的不可变计划。",
                      "The connection check passed. Confirm the immutable plan below.",
                    )}
                  </p>
                </div>
              </div>
              <PreviewCommitCard
                affected={[{ detail: "10.0.0.12", name: "host-web-12" }]}
                diff={[
                  { type: "add", content: "+ resource.host host-web-12" },
                  { type: "add", content: "+ telemetry.collector v24.1.3" },
                  {
                    type: "context",
                    content: text(
                      "~ 无端口与防火墙变更",
                      "~ no port or firewall changes",
                    ),
                  },
                ]}
                expiresAt={Date.now() + 10 * 60_000}
                onCancel={() => {}}
                onConfirm={() => {}}
                planHash="sha256:9f2c…a41d"
                risk="write"
                riskLabel={text(
                  "中风险 · 需人工确认",
                  "Medium · Confirmation required",
                )}
                title={text("新增生产主机", "Add production host")}
              >
                <p>
                  {text(
                    "连接检查通过。下面的预览包含将写入的资源和 Collector 安装步骤，请确认。",
                    "The connection check passed. Review the resource and Collector installation plan below.",
                  )}
                </p>
              </PreviewCommitCard>
            </div>
            <div className="demo-side-card">
              <Card>
                <CardHeader
                  action={<Badge tone="info">system</Badge>}
                  title={text("主机概览", "Host overview")}
                />
                <CardContent>
                  <div className="four-metrics">
                    <Metric label="CPU" unit="%" value="42" />
                    <Metric
                      label={text("内存", "Memory")}
                      unit="%"
                      value="70"
                    />
                    <Metric label={text("磁盘", "Disk")} unit="%" value="61" />
                    <Metric
                      label={text("延迟", "Latency")}
                      unit="ms"
                      value="18"
                    />
                  </div>
                  <div className="spark-bars">
                    {[30, 42, 36, 55, 48, 62, 58, 73, 64, 78, 70, 83].map(
                      (v, i) => (
                        <i
                          className={i > 9 ? "hot" : ""}
                          key={i}
                          style={{ height: `${v}%` }}
                        />
                      ),
                    )}
                  </div>
                </CardContent>
                <CardFooter>
                  <Badge dot tone="success">
                    {text("数据正常", "Data healthy")}
                  </Badge>
                  <span className="push-right muted-copy">
                    {text(
                      "host.metrics · 8 秒前",
                      "host.metrics · 8 seconds ago",
                    )}
                  </span>
                </CardFooter>
              </Card>
              <CodeBlock
                code={
                  "Card Host\n  └─ sandboxed iframe\n      ├─ query_binding_id\n      └─ action_binding_id"
                }
                language="security-boundary"
              />
            </div>
          </div>
        </DemoSection>
        <footer className="demo-footer">
          <span>◉ Argus Design System</span>
          <p>
            {text(
              "所有示例均为前端模拟数据，不包含真实 Secret 或授权状态。",
              "All examples use frontend mock data and contain no real secrets or authorization state.",
            )}
          </p>
        </footer>
      </div>
    </div>
  );
}
