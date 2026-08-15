/**
 * Chatbox 会话页文案（会话流、输入区、卡片、确认流、上下文面板）。
 * key 结构：`chat.<区域>.<名称>`，双语资源在本文件内成对维护。
 */
export const chatZh = {
  chat: {
    assistant: "Argus",
    you: "你",
    welcome: {
      title: "Argus 智能会话",
      description:
        "用自然语言查询资源状态、分析异常，或描述一个运维任务。所有生产变更都会生成不可变预览，确认后才会执行。",
      examples: {
        hostCreate:
          "新增一台 Web 主机（10.0.9.20），走上海机房堡垒机-01 并接入监控",
        hostOverview: "看一下生产主机的整体状态和告警概览",
        addHost: "新增主机 10.1.2.3 并安装 OTLP 收集器",
        restarts: "最近一小时有没有异常重启？",
      },
    },
    header: {
      rename: "重命名会话",
      renamePlaceholder: "输入新的会话标题",
      archive: "归档会话",
      save: "保存",
      cancel: "取消",
      toggleContext: "上下文面板",
      untitled: "新的会话",
    },
    trace: {
      title: "{{count}} 个工具调用",
      expand: "展开工具调用",
      collapse: "收起工具调用",
      running: "运行中",
      success: "完成",
      failed: "失败",
    },
    card: {
      aiGenerated: "AI 生成",
      system: "系统",
      loading: "卡片加载中…",
      untitled: "未命名卡片",
      openCreated: "前往交互卡片详情",
    },
    action: {
      confirm: "确认执行",
      cancel: "取消",
      executing: "执行中…",
      awaitingApproval: "已确认，等待审批",
      viewTask: "查看任务",
      failed: "操作失败",
      risk: {
        read: "只读",
        write: "写入",
        dangerous: "危险",
        critical: "关键",
      },
      summary: "变更摘要",
      affected: "影响对象",
    },
    result: {
      success: "执行成功",
      failed: "执行失败",
      action: "卡片操作",
    },
    composer: {
      placeholder: "询问资源状态，或描述你想完成的运维任务…",
      send: "发送",
      stop: "停止生成",
      attach: "添加附件",
      mention: "引用资源（堡垒机 / 主机）",
      mentionTitle: "引用资源",
      commandTitle: "命令",
      createInteractiveCard: "创建交互卡片",
      createInteractiveCardHint: "AI 创建禁用草稿",
      noModel: "没有可用模型，请联系管理员配置或调整额度。",
      noMatch: "没有匹配项",
      hint: "Enter 发送 · Shift+Enter 换行 · @ 引用资源",
      disclaimer:
        "Argus 可能会出错。生产变更始终需要确定性权限检查与人工确认。",
    },
    model: {
      select: "选择模型",
      reason: {
        disabled: "已停用",
        unhealthy: "健康检查失败",
        compatibility_failed: "兼容性测试失败",
        department_quota_exhausted: "部门额度已用尽",
        user_quota_exhausted: "个人额度已用尽",
        unavailable: "不可用",
      },
    },
    context: {
      title: "会话上下文",
      collapse: "收起面板",
      expand: "展开面板",
      references: "引用的资源",
      emptyReferences: "当前会话还没有引用资源或卡片。",
      permissions: "我的权限",
      roles: "角色",
      noRoles: "未分配角色",
      permissionsCount: "{{count}} 项权限",
      recentActions: "最近动作",
      emptyActions: "暂无审计记录",
    },
    error: {
      sendFailed: "消息发送失败，请重试。",
      loadFailed: "会话加载失败。",
    },
  },
};

export const chatEn = {
  chat: {
    assistant: "Argus",
    you: "You",
    welcome: {
      title: "Argus Intelligent Chat",
      description:
        "Ask about resource health, analyze anomalies, or describe an operations task in natural language. Every production change produces an immutable preview and runs only after your confirmation.",
      examples: {
        hostCreate:
          "Add a web host (10.0.9.20) via the Shanghai-DC-01 bastion and enable monitoring",
        hostOverview: "Show the overall status and alerts of production hosts",
        addHost: "Add host 10.1.2.3 and install the Collector",
        restarts: "Any abnormal restarts in the last hour?",
      },
    },
    header: {
      rename: "Rename conversation",
      renamePlaceholder: "Enter a new title",
      archive: "Archive conversation",
      save: "Save",
      cancel: "Cancel",
      toggleContext: "Context panel",
      untitled: "New conversation",
    },
    trace: {
      title: "{{count}} tool calls",
      expand: "Expand tool calls",
      collapse: "Collapse tool calls",
      running: "Running",
      success: "Done",
      failed: "Failed",
    },
    card: {
      aiGenerated: "AI generated",
      system: "System",
      loading: "Loading card…",
      untitled: "Untitled card",
      openCreated: "Open interactive card details",
    },
    action: {
      confirm: "Confirm and run",
      cancel: "Cancel",
      executing: "Executing…",
      awaitingApproval: "Confirmed, awaiting approval",
      viewTask: "View task",
      failed: "Operation failed",
      risk: {
        read: "Read",
        write: "Write",
        dangerous: "Dangerous",
        critical: "Critical",
      },
      summary: "Change summary",
      affected: "Affected",
    },
    result: {
      success: "Succeeded",
      failed: "Failed",
      action: "Card action",
    },
    composer: {
      placeholder: "Ask about resource health or describe an operations task…",
      send: "Send",
      stop: "Stop generating",
      attach: "Attach file",
      mention: "Reference a resource (Connector / host)",
      mentionTitle: "Reference a resource",
      commandTitle: "Command",
      createInteractiveCard: "Create interactive card",
      createInteractiveCardHint: "AI creates a disabled draft",
      noModel:
        "No model is available. Ask an administrator to configure one or adjust budgets.",
      noMatch: "No matches",
      hint: "Enter to send · Shift+Enter for newline · @ to reference",
      disclaimer:
        "Argus can make mistakes. Production changes always require deterministic authorization and confirmation.",
    },
    model: {
      select: "Select model",
      reason: {
        disabled: "Disabled",
        unhealthy: "Health check failed",
        compatibility_failed: "Compatibility test failed",
        department_quota_exhausted: "Department budget exhausted",
        user_quota_exhausted: "Personal budget exhausted",
        unavailable: "Unavailable",
      },
    },
    context: {
      title: "Conversation context",
      collapse: "Collapse panel",
      expand: "Expand panel",
      references: "Referenced resources",
      emptyReferences:
        "No resources or cards referenced in this conversation yet.",
      permissions: "My permissions",
      roles: "Roles",
      noRoles: "No roles assigned",
      permissionsCount: "{{count}} permissions",
      recentActions: "Recent actions",
      emptyActions: "No audit records yet",
    },
    error: {
      sendFailed: "Failed to send the message. Please retry.",
      loadFailed: "Failed to load the conversation.",
    },
  },
};
