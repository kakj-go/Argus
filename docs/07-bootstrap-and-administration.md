# 系统初始化与双层管理门户

## 1. 目标

Argus 第一次启动时没有任何用户和企业。系统通过一次性初始化流程让部署者设置平台超级管理员账号和密码。初始化完成后，平台超级管理员负责创建企业、创建对应企业管理员以及配置平台级 OpenSandbox；企业管理员进入独立企业门户管理本企业。

平台超级管理员不是企业的“万能管理员”，其权限应保持最小化。

## 2. 系统状态

建议维护独立的 PlatformState：

```text
uninitialized
initializing
initialized
```

只有 `uninitialized` 状态可以访问初始化接口。初始化完成后，普通 HTTP 请求不能把系统重新切回未初始化状态。

```mermaid
stateDiagram-v2
    [*] --> Uninitialized
    Uninitialized --> Initializing: 提交合法 Setup Token 和初始化表单
    Initializing --> Initialized: 事务提交成功
    Initializing --> Uninitialized: 事务失败
    Initialized --> Initialized: 正常运行
```

## 3. Setup Token

如果初始化页面直接暴露在公网，第一个访问者可能抢占超级管理员。因此首次初始化需要一个部署侧生成的一次性 Setup Token。

可选来源：

- 服务首次启动时生成并输出到受保护的控制台。
- 部署者通过环境变量或启动参数显式提供。
- 本地单机安装时，仅允许 localhost 完成初始化。

Setup Token 应具有：

- 高随机性。
- 短有效期。
- 单次使用。
- 只允许调用初始化接口。
- 日志中只在首次生成时显示一次，后续只记录摘要。

初始化成功后立即销毁 Token，并永久关闭初始化接口。

## 4. 初始化向导

建议步骤：

### 4.1 系统信息

- 平台显示名称。
- 默认语言和时区。
- 外部访问地址，用于生成邀请链接和回调地址。

### 4.2 超级管理员

- 登录名。
- 显示名称。
- 邮箱，可选但建议填写。
- 密码与确认密码。

密码至少要求：

- 长度不少于 12 位。
- 禁止常见弱密码。
- 使用 Argon2id 或等价强密码哈希。
- 密码明文不写日志、不进入审计详情。

### 4.3 基础 OpenSandbox

初始化向导可以只询问：

- 是否启用 OpenSandbox。
- OpenSandbox 服务地址。
- 连接凭证。
- 默认执行存储位置。

镜像、资源和网络策略可以在登录平台门户后继续配置。

### 4.4 初始化提交

初始化必须在一个数据库事务中完成：

1. 锁定 PlatformState。
2. 再次确认系统未初始化。
3. 校验 Setup Token。
4. 创建平台超级管理员。
5. 创建平台默认设置和审计根记录。
6. 将 PlatformState 更新为 `initialized`。
7. 消费 Setup Token。

并发初始化请求只能有一个成功。

## 5. 超级管理员登录后界面

超级管理员门户建议使用独立路由，例如 `/platform`，菜单严格限制为：

```text
平台管理
├── 企业管理
├── 企业管理员
├── OpenSandbox
│   ├── 服务连接
│   ├── 镜像
│   ├── Sandbox Profile
│   ├── 企业配额
│   ├── 活动会话
│   └── 平台用量
└── 我的账号
```

`我的账号` 只用于修改自身密码、MFA 和登录会话，不属于业务管理权限扩张。

超级管理员界面不出现：

- Chatbox 和企业会话。
- 企业模型供应商和模型凭证。
- Connector、主机和 Kubernetes。
- 企业 Secret。
- 企业 Card Skill。
- 企业 Tool Result 和业务审计正文。
- 企业原始 Metrics、Logs、Traces 和 Collector 配置正文。

活动 Sandbox 页面只展示平台运行所需元数据，例如企业、Profile、资源占用和状态；默认不展示企业文件、命令内容和输出。

## 6. 企业创建

超级管理员创建企业时填写：

```json
{
  "name": "Example Corp",
  "code": "example-corp",
  "status": "active",
  "timezone": "Asia/Shanghai",
  "sandbox_quota_profile": "standard",
  "remark": ""
}
```

企业状态：

- `active`：正常使用。
- `suspended`：禁止企业用户登录和新任务执行，但保留数据。
- `disabled`：长期停用，仍不立即删除数据。

删除企业属于高危且难恢复操作，不建议第一版提供。先提供停用和数据保留策略。

企业状态必须由授权服务在所有入口统一执行，不能只在登录页面判断：

| 能力 | active | suspended | disabled |
| --- | --- | --- | --- |
| 企业用户新登录 | 允许 | 拒绝 | 拒绝 |
| 已登录 Session | 允许 | 立即撤销 | 立即撤销 |
| 新 Run/Tool/Action | 允许 | 拒绝 | 拒绝 |
| 未执行 Pending Action/Approval | 允许 | 失效 | 失效 |
| 正在执行的危险操作 | 正常 | 请求安全停止并审计；不可中断步骤继续到安全点 | 同 suspended |
| Connector 控制连接 | 允许 | 保持心跳但拒绝新命令 | 吊销设备凭证并断开 |
| Telemetry 摄入 | 允许 | 默认继续短期摄入，按平台策略限额 | 拒绝新数据 |
| 历史数据查询 | 按权限 | 默认拒绝企业用户；平台不获得正文权限 | 拒绝 |
| 数据保留 | 套餐策略 | 保留 | 按停用保留策略 |

`suspended` 默认继续接收一段可配置宽限期的遥测，避免因商务或登录问题造成不可恢复监控缺口，但禁止企业用户查询；超过宽限期后 Ingest 按策略拒绝。平台超级管理员只能看到用量和健康元数据，不能借此读取业务正文。

## 7. 创建企业管理员

企业创建完成后，超级管理员至少创建一名 Enterprise Admin：

```json
{
  "enterprise_id": "ent-01",
  "username": "corp-admin",
  "display_name": "企业管理员",
  "email": "admin@example.com",
  "activation": "temporary_password"
}
```

支持两种激活方式：

- 一次性激活链接。
- 临时密码，首次登录必须修改。

超级管理员可以：

- 创建多名企业管理员。
- 重发激活邀请。
- 禁用企业管理员。
- 重置登录认证。
- 查看激活和登录状态。

超级管理员不能以企业管理员身份代登录，也不能看到企业管理员后续设置的模型密钥。

## 8. 企业管理员门户

企业管理员登录企业门户，例如 `/app/:enterprise`。建议菜单：

```text
Chatbox

管理
├── 组织与权限
│   ├── 用户
│   ├── 用户组
│   ├── 角色
│   ├── 数据权限
│   └── Service Account
├── AI 设置
│   ├── 模型供应商
│   ├── 模型部署
│   ├── Model Alias
│   ├── Agent Profile
│   ├── 路由与降级
│   └── 用量与预算
├── 连接与资源
│   ├── Connector
│   ├── 主机
│   └── Kubernetes
├── 可观测性
│   ├── 遥测网络组
│   ├── Collector 实例
│   ├── 采集 Profile
│   ├── 数据接入状态
│   └── 监控数据用量
├── Card Skill
├── Secret
├── Sandbox 用量
└── 企业审计
```

企业管理员可以创建自定义企业角色并下放部分管理能力，但不能修改平台 OpenSandbox 镜像、安全基线和全局资源上限。

## 9. 企业上下文选择

如果一个用户属于多个企业，登录后应明确选择企业。所有企业 API 请求必须携带由服务端会话确认的 enterprise_id，不能只相信浏览器提交的企业 ID。

切换企业时：

- 清空当前会话内的企业资源缓存。
- 重新获取权限和 Tool 列表。
- 断开企业范围的流式订阅。
- 记录企业上下文切换审计。

浏览器提交的 enterprise_id 只是选择意图。服务端 Session 保存当前 Membership 上下文，访问 Token 包含短期企业 Audience，API Key/Service Account 固定绑定 enterprise_id 和 Scope。任何资源 ID 查询都必须再次验证其真实 enterprise_id，不能只信任 Token 内的客户端参数。

### 9.1 认证基线

第一版至少提供本地账号认证，并预留 OIDC/SAML Adapter：

- 密码使用 Argon2id，登录、修改密码和恢复流程统一限流。
- 平台超级管理员强制 MFA；企业可对管理员和 critical 操作强制 MFA/Step-up Authentication。
- 浏览器使用 HttpOnly、Secure、SameSite Cookie 或同等安全 Session；变更请求执行 CSRF 防护。
- Access Session 短期有效，Refresh/长期 Session 可撤销；用户禁用、密码重置、企业停用时立即写入撤销事实并通过 Redis 快速失效缓存。
- WebSocket/SSE 建立和恢复时重新校验 Session 与企业上下文。
- Service Account/API Key 只显示一次原值，数据库保存哈希，支持到期、轮换、Scope 和最后使用审计。

## 10. 账号恢复

单一超级管理员存在不可恢复风险。第一版即使只初始化一个超级管理员，也应提供部署者控制的离线恢复命令：

```text
argus admin reset-password
```

该命令只允许在服务端主机执行，要求访问部署密钥或数据库主密钥，且写入高优先级平台审计。它不创建企业权限，也不能导出企业数据。

后续可以增加多个平台超级管理员或外部 IdP，但不应通过企业管理界面隐式产生平台管理员。

## 11. 审计域

平台审计和企业审计分离：

- 平台审计：初始化、企业生命周期、企业管理员生命周期、Sandbox Profile、镜像、配额和活动会话终止。
- 企业审计：模型、用户权限、Connector、资源、OpenTelemetry 安装与配置、Chatbox、MCP Tool、Card Action 和 Secret 使用。

超级管理员默认只能查看平台审计；企业管理员只能查看本企业审计。
