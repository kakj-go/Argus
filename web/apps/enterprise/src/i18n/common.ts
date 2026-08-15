/**
 * 通用文案 + 页面/展厅命名空间。
 * Chatbox 会话页文案在 `chat.ts`（`chat.*`），外壳与导航在 `shell.ts`。
 */
export const commonZh = {
  common: {
    loading: "加载中…",
    retry: "重试",
    underConstruction: {
      title: "建设中",
      description: "该页面正在建设中，敬请期待。",
    },
  },
  pages: {
    resources: {
      eyebrow: "基础设施",
      title: "连接与资源",
      description: "通过堡垒机安全连接主机与私有网络；凭据与密钥只保存引用。",
      action: "接入资源",
    },
    kubernetes: {
      eyebrow: "基础设施",
      title: "Kubernetes",
      description:
        "集群查询按授权 Namespace Scope 收敛；变更统一进入 Preview / Confirm / Commit。",
      action: "接入集群",
    },
    observability: {
      eyebrow: "Telemetry",
      title: "可观测性",
      description:
        "Metrics、Logs、Traces 独立查询链路；控制通道与遥测摄入互不占用。",
      action: "用 AI 查询数据",
    },
    automation: {
      eyebrow: "执行治理",
      title: "自动化与审批",
      description:
        "运行、审批和变更执行均由 PostgreSQL 保存权威状态，可暂停、恢复与审计。",
      action: "创建自动化",
    },
    administration: {
      eyebrow: "企业治理",
      title: "企业管理",
      description:
        "模型、Agent、交互卡片、组织权限与企业审计；配置变更产生不可变 Revision。",
    },
  },
  demo: {
    badge: "设计系统预览",
    title: "为复杂运维，保持安静与清晰。",
    description:
      "这是 Argus 主前端的唯一组件与设计规范入口。所有状态不仅依赖颜色表达，并优先保证密集信息的层级与可读性。",
    viewCards: "查看业务卡片",
    copyToken: "复制 Token",
    foundations: "基础规范",
    actions: "按钮与徽章",
    forms: "表单输入",
    navigation: "导航与浮层",
    data: "数据展示",
    feedback: "反馈与状态",
    patterns: "页面模式",
    aiCards: "AI 与业务卡片",
  },
};

export const commonEn = {
  common: {
    loading: "Loading…",
    retry: "Retry",
    underConstruction: {
      title: "Under construction",
      description: "This page is under construction. Stay tuned.",
    },
  },
  pages: {
    resources: {
      eyebrow: "Infrastructure",
      title: "Connections & Resources",
      description:
        "Connect hosts and private networks securely through bastions; credentials and secrets remain references.",
      action: "Connect resource",
    },
    kubernetes: {
      eyebrow: "Infrastructure",
      title: "Kubernetes",
      description:
        "Queries intersect with authorized Namespace scope; changes follow Preview / Confirm / Commit.",
      action: "Connect cluster",
    },
    observability: {
      eyebrow: "Telemetry",
      title: "Observability",
      description:
        "Metrics, Logs, and Traces use an isolated query path separate from control and ingestion.",
      action: "Query with AI",
    },
    automation: {
      eyebrow: "Execution governance",
      title: "Automation & Approvals",
      description:
        "PostgreSQL stores authoritative run, approval, and execution state for pause, recovery, and audit.",
      action: "Create automation",
    },
    administration: {
      eyebrow: "Enterprise governance",
      title: "Enterprise Administration",
      description:
        "Models, Agents, 交互卡片s, authorization, and audit with immutable configuration revisions.",
    },
  },
  demo: {
    badge: "Design system preview",
    title: "Calm clarity for complex operations.",
    description:
      "The single component and design-system reference for Argus. Every state combines color with text or iconography and preserves hierarchy in dense interfaces.",
    viewCards: "View business cards",
    copyToken: "Copy tokens",
    foundations: "Foundations",
    actions: "Actions & badges",
    forms: "Form controls",
    navigation: "Navigation & overlays",
    data: "Data display",
    feedback: "Feedback & states",
    patterns: "Page patterns",
    aiCards: "AI & business cards",
  },
};
