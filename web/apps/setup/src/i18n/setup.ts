/**
 * 初始化向导文案（`setup.*` 命名空间）。
 * 设计依据 docs/07 §2-4：一次性初始化向导，四步完成平台初始化。
 */
export const setupZh = {
  setup: {
    badge: "首次初始化",
    title: "欢迎使用 Argus",
    subtitle:
      "完成一次性系统初始化：验证部署凭据，创建超级管理员，并可选择性接入 OpenSandbox 执行基座。初始化完成后本页面将永久关闭。",
    loading: "正在检查系统初始化状态…",
    statusError: {
      title: "无法获取系统状态",
      description: "请检查控制平面服务是否可用，然后重试。",
      retry: "重试",
    },
    initialized: {
      badge: "已初始化",
      title: "系统已完成初始化",
      description:
        "本平台已完成一次性初始化，初始化向导已永久关闭。请使用超级管理员账号登录平台控制台。",
      goLogin: "前往登录",
    },
    success: {
      badge: "初始化完成",
      title: "系统初始化成功",
      description:
        "平台超级管理员已创建，初始化向导已永久关闭。请使用刚才创建的超级管理员账号登录。",
      goLogin: "前往登录",
    },
    steps: {
      token: { title: "验证 Setup Token", description: "部署侧一次性凭据" },
      system: { title: "系统与管理员", description: "平台信息与超级管理员" },
      sandbox: { title: "OpenSandbox 基座", description: "可选，稍后可配置" },
      review: { title: "确认提交", description: "单事务提交，不可重复" },
    },
    token: {
      label: "Setup Token",
      placeholder: "stp_••••••••••••",
      hint: "由部署侧一次性生成，验证并提交成功后立即失效。",
      required: "请输入 Setup Token",
      tooShort: "Setup Token 长度至少 8 位",
    },
    system: {
      platformSection: "系统信息",
      adminSection: "超级管理员",
      platformName: {
        label: "平台显示名称",
        placeholder: "Argus Production",
        required: "请输入平台显示名称",
        tooLong: "平台显示名称不能超过 64 个字符",
      },
      defaultLocale: { label: "默认语言" },
      timezone: { label: "默认时区" },
      externalUrl: {
        label: "外部访问地址",
        placeholder: "https://argus.example.com",
        hint: "用户访问平台的入口地址，用于生成回调与通知链接。",
        required: "请输入外部访问地址",
        invalid: "请输入合法的 http(s) 地址",
      },
      username: {
        label: "登录名",
        placeholder: "admin",
        hint: "3-32 位，字母开头，可含字母、数字、下划线与连字符。",
        required: "请输入登录名",
        invalid: "登录名需字母开头，3-32 位字母、数字、_ 或 -",
      },
      displayName: {
        label: "显示名",
        placeholder: "平台管理员",
        required: "请输入显示名",
      },
      email: {
        label: "邮箱",
        placeholder: "admin@example.com",
        required: "请输入邮箱",
        invalid: "请输入合法的邮箱地址",
      },
      password: {
        label: "密码",
        hint: "至少 12 位，且同时包含字母与数字。",
        required: "请输入密码",
        tooShort: "密码长度至少 12 位",
        weak: "密码需同时包含字母与数字",
      },
      confirmPassword: {
        label: "确认密码",
        mismatch: "两次输入的密码不一致",
      },
      strength: {
        label: "密码强度",
        weak: "弱",
        medium: "中",
        strong: "强",
      },
    },
    sandbox: {
      intro:
        "OpenSandbox 为 Agent 与自动化提供隔离执行环境。本步骤可跳过，初始化后仍可在平台控制台配置。",
      enable: "启用 OpenSandbox 基座",
      endpoint: {
        label: "服务地址",
        placeholder: "https://sandbox.example.com",
        required: "请输入 OpenSandbox 服务地址",
        invalid: "请输入合法的 http(s) 地址",
      },
      credential: {
        label: "连接凭证",
        placeholder: "访问令牌 / API Key",
        hint: "凭证仅以后端 Secret 引用形式保存，不会再次明文展示。",
        required: "请输入连接凭证",
      },
      storage: {
        label: "默认执行存储位置",
        placeholder: "s3://argus-sandbox/default",
        required: "请输入默认执行存储位置",
      },
      test: {
        button: "测试连接",
        success: "连接成功",
        failure: "连接失败，请检查服务地址",
      },
    },
    review: {
      intro:
        "请核对以下信息。提交为单事务操作：任一环节失败将整体回滚；成功后 Setup Token 立即失效，本向导永久关闭。",
      tokenMasked: "已验证（提交时随表单一并校验）",
      masked: "••••••••",
      notEnabled: "不启用（稍后在平台控制台配置）",
      submit: "确认初始化",
      errorTitle: "初始化失败",
    },
  },
};

export const setupEn = {
  setup: {
    badge: "First-time setup",
    title: "Welcome to Argus",
    subtitle:
      "Complete the one-time initialization: verify the deployment credential, create the super administrator, and optionally connect the OpenSandbox execution foundation. This page closes permanently once finished.",
    loading: "Checking platform initialization state…",
    statusError: {
      title: "Unable to fetch platform status",
      description: "Check that the control plane is reachable, then retry.",
      retry: "Retry",
    },
    initialized: {
      badge: "Initialized",
      title: "Platform already initialized",
      description:
        "One-time initialization has already completed and this wizard is permanently closed. Sign in to the platform console with the super administrator account.",
      goLogin: "Go to sign in",
    },
    success: {
      badge: "Setup complete",
      title: "Platform initialized successfully",
      description:
        "The super administrator has been created and this wizard is now permanently closed. Sign in with the account you just created.",
      goLogin: "Go to sign in",
    },
    steps: {
      token: { title: "Verify Setup Token", description: "One-time deployment credential" },
      system: { title: "System & admin", description: "Platform info and super admin" },
      sandbox: { title: "OpenSandbox", description: "Optional, configurable later" },
      review: { title: "Confirm & submit", description: "Single transaction, one-shot" },
    },
    token: {
      label: "Setup Token",
      placeholder: "stp_••••••••••••",
      hint: "Generated once by the deployment and invalidated immediately after a successful submission.",
      required: "Enter the Setup Token",
      tooShort: "Setup Token must be at least 8 characters",
    },
    system: {
      platformSection: "System information",
      adminSection: "Super administrator",
      platformName: {
        label: "Platform display name",
        placeholder: "Argus Production",
        required: "Enter the platform display name",
        tooLong: "Platform display name must be at most 64 characters",
      },
      defaultLocale: { label: "Default language" },
      timezone: { label: "Default timezone" },
      externalUrl: {
        label: "External access URL",
        placeholder: "https://argus.example.com",
        hint: "The entry URL users visit; used for callbacks and notification links.",
        required: "Enter the external access URL",
        invalid: "Enter a valid http(s) URL",
      },
      username: {
        label: "Username",
        placeholder: "admin",
        hint: "3-32 characters, starts with a letter; letters, digits, _ and - allowed.",
        required: "Enter a username",
        invalid: "Must start with a letter; 3-32 letters, digits, _ or -",
      },
      displayName: {
        label: "Display name",
        placeholder: "Platform Admin",
        required: "Enter a display name",
      },
      email: {
        label: "Email",
        placeholder: "admin@example.com",
        required: "Enter an email address",
        invalid: "Enter a valid email address",
      },
      password: {
        label: "Password",
        hint: "At least 12 characters, containing both letters and digits.",
        required: "Enter a password",
        tooShort: "Password must be at least 12 characters",
        weak: "Password must contain both letters and digits",
      },
      confirmPassword: {
        label: "Confirm password",
        mismatch: "Passwords do not match",
      },
      strength: {
        label: "Password strength",
        weak: "Weak",
        medium: "Medium",
        strong: "Strong",
      },
    },
    sandbox: {
      intro:
        "OpenSandbox provides isolated execution environments for agents and automation. This step is optional and can be configured later in the platform console.",
      enable: "Enable OpenSandbox foundation",
      endpoint: {
        label: "Endpoint",
        placeholder: "https://sandbox.example.com",
        required: "Enter the OpenSandbox endpoint",
        invalid: "Enter a valid http(s) URL",
      },
      credential: {
        label: "Connection credential",
        placeholder: "Access token / API key",
        hint: "Stored only as a backend Secret reference and never shown in plain text again.",
        required: "Enter the connection credential",
      },
      storage: {
        label: "Default execution storage",
        placeholder: "s3://argus-sandbox/default",
        required: "Enter the default execution storage location",
      },
      test: {
        button: "Test connection",
        success: "Connection succeeded",
        failure: "Connection failed; check the endpoint",
      },
    },
    review: {
      intro:
        "Review the information below. Submission is a single transaction: any failure rolls everything back; on success the Setup Token is invalidated and this wizard closes permanently.",
      tokenMasked: "Verified (validated again on submit)",
      masked: "••••••••",
      notEnabled: "Disabled (configure later in the platform console)",
      submit: "Confirm & initialize",
      errorTitle: "Initialization failed",
    },
  },
};
