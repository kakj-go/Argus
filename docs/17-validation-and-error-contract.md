# 输入校验与安全错误契约

## 1. 目标

Argus 必须同时满足两个要求：用户能知道输入为什么失败，运维人员能用请求 ID 定位服务端原因；浏览器和普通 API 调用方又不能获得 SQL、堆栈、路径、内部拓扑、Secret、Token、密码片段或代码实现细节。

前端校验不是安全边界。任何可由非浏览器客户端提交的数据都必须由后端再次校验。

## 2. 错误公开边界

| 错误类别        | 客户端可见                                                           | 服务端日志                        | 约束                                     |
| --------------- | -------------------------------------------------------------------- | --------------------------------- | ---------------------------------------- |
| 字段格式/长度   | `code`、`message_key`、白名单 `params`、安全 `message`、`request_id` | 稳定错误码和请求上下文            | 不回显 Secret 或完整敏感输入             |
| 业务状态冲突    | 稳定错误码、通用安全文案、`request_id`                               | 稳定错误码；必要时记录内部资源 ID | 不公开跨租户资源是否存在                 |
| 登录失败        | `INVALID_CREDENTIALS`、通用文案、`request_id`                        | 受控认证事件                      | 不区分用户不存在和密码错误               |
| 权限/跨租户拒绝 | `AUTHORIZATION_DENIED` 或对外 404、`request_id`                      | 真实内部判定和主体上下文          | 不泄露目标资源存在性                     |
| 内部错误        | `INTERNAL_ERROR`、通用文案、`request_id`                             | 原始 error 与相同 request ID      | 不向客户端返回 SQL、堆栈、路径或内部 URL |

`api/contracts/error-codes.yaml` 登记稳定错误码和允许公开的 `safe_params`。未登记的错误码不能从 HTTP handler 发出。前端只展示服务端显式 `message`；任意 JavaScript `Error.message` 只能用于本地启动配置错误等不来自 API 的开发诊断。

## 3. 校验事实来源

1. OpenAPI：普通字段的 required、类型、长度、格式、枚举和简单 pattern。
2. 独立共享契约：密码等无法仅靠 OpenAPI 表达、且必须跨 Go/TypeScript 完全一致的复合规则。
3. 后端领域服务：租户归属、权限、唯一性、资源存在性、版本、状态机、时间关系和引用关系。
4. 前端 Zod：从上述事实生成或薄封装，只负责提交前反馈，不新增另一套产品规则。

密码当前使用 `api/contracts/password-policy.json`，由 `scripts/generate-password-policy.mjs` 生成 Go 与 TypeScript 常量。错误使用 `PASSWORD_WEAK + params.rule`，不会返回命中的用户名、邮箱片段或旧密码。

## 4. 2026-08-24 审计结果

| 范围                                        | OpenAPI/后端                                                                       | 前端                                                            | 结论与动作                                                        |
| ------------------------------------------- | ---------------------------------------------------------------------------------- | --------------------------------------------------------------- | ----------------------------------------------------------------- |
| Platform 初始化、首次登录、平台/企业账户改密 | 12-1024、Unicode 字母/数字、常见密码、身份片段、禁止复用                           | Platform 内初始化与四条身份流程消费同一生成契约                  | 已一致                                                            |
| 初始化平台名、显示名、时区、用户名长度      | OpenAPI 上限分别为 128/128/128/128                                                 | Platform Setup Gate 的 Zod 和控件消费生成约束；邮箱按 OpenAPI 可选 | 普通字段边界已一致；用户名字符集仍需统一身份输入契约            |
| 平台创建企业/管理员                         | OpenAPI 有 name 128、code 63、timezone 128、remark 2048、username/display name 128 | RHF/Zod、HTML 控件和字段级服务端错误回填消费生成约束             | 已一致                                                            |
| 企业用户/部门/角色/ServiceAccount/DataScope | OpenAPI 普遍有 name 128、description 1024、数组唯一性和 UUID                       | DataScope 及 Label Selector 的对象/标量约束均由生成文件消费      | 普通字段边界已收敛；跨字段和引用关系仍由领域层负责                |
| RoleBinding、API Key、Remote Access         | OpenAPI 有 date-time/UUID/枚举与数组/数值边界；领域层校验时间先后、引用归属和状态  | 主要写表单统一 RHF/Zod，Remote Access 使用生成边界               | 关系规则留在后端，并通过安全 `params` 指出字段/规则               |
| Host/Kubernetes/Connector                   | OpenAPI 描述格式，领域层校验网络路径、SSRF、ConnectionTest、版本与状态             | 向导只做即时可填写性判断                                        | 不能把网络或资源真实性前移到浏览器                                |
| Telemetry Query                             | 后端 parser/type/scope/budget 是权威                                               | 前端 Builder/DSL 只做基础输入                                   | 错误码已登记；后续应为 parse/type/complexity 填充已登记的安全参数 |
| API 错误展示                                | bundled OpenAPI middleware 拒绝非法请求；handler 保留安全参数和 request ID         | 可定位错误回填 Field，其他错误进入表单摘要并保留 request ID      | 已建立安全边界，后续新页面必须复用                                |

## 5. 当前实现与剩余缺口

`/api/v1` 已在进入领域 Handler 前使用嵌入的完整 bundled OpenAPI 校验 request body、path、query 和 header。校验库原文不会直接返回；失败统一转换为 `INVALID_ARGUMENT`，仅公开 `field`、`rule`、长度/数值边界和允许公开的 `format`，并附带 request ID。HTTP 直接提交 `context_window_tokens=4096` 已验证返回 minimum `8192`，且响应不包含 API Key、输入值、正则、堆栈或内部路径。

前端已完成：

- Enterprise、Platform（含初始化流程）的 217 个可见 `Field` 全部显式声明 `required/optional/none`，必填只显示红色 `*`。
- `Input`、`Textarea`、`Select` 和复合字段统一 label、description、error、required 与 invalid ARIA 关系。
- `FormDrawer` 使用真实 `<form>`；写表单以 React Hook Form `handleSubmit` 和 Zod 为提交边界。
- `form-constraints.ts` 从 bundled OpenAPI 生成对象字段和标量 Schema 约束，覆盖 required、长度、格式、枚举、pattern、数组和数值边界；密码继续使用独立共享契约。
- `presentApiFormError` 只按每个表单显式提供的 `fieldMap` 白名单定位服务端字段；未知字段进入表单摘要，Field 和摘要都保留安全公开消息与 request ID。
- 远程访问等业务义务错误由稳定 `code` 驱动交互：`REMOTE_ACCESS_MFA_REQUIRED` 打开 fresh Step-up 对话框，验证成功后自动重试原请求；用户界面不直接显示 `message_key` 或任意 `Error.message`。

剩余系统缺口：

1. 为用户名建立独立身份输入策略，统一允许字符、大小写归一化和 3-128 长度，并由后端执行。
2. 为时间关系、跨字段关系和领域状态建立 typed domain validation error；只公开稳定字段名和规则名。
3. 继续扩大真实 Kubernetes E2E 中浏览器与直接 HTTP 非法输入的覆盖，确认所有新增领域都保留安全参数、日志关联和脱敏边界。

## 6. 门禁

- 密码契约版本、边界、规则顺序和弱密码列表唯一性由 Go contract test 检查。
- 生成漂移由 `argus-dev contracts check` 检查。
- `error-codes.yaml` 的错误码、消息键和 `safe_params` 形状由 contract test 检查。
- HTTP error mapper 中出现的稳定错误码必须已登记。
- 前端用户可见 API 错误不得直接渲染任意 `Error.message`。
- AST 门禁要求每个 `Field` 显式声明 requirement，禁止手写星号、可编辑控件使用 `none`、缺少 submit 边界，以及按钮直接或间接绕过 RHF/Zod 调用写 API；checker 自身有正反例单测。
- E2E 失败诊断只保存脱敏响应、请求 ID、稳定错误码和受控服务端日志。

2026-08-24 的 M8 最终验证确认：直接 HTTP 非法输入返回安全 `INVALID_ARGUMENT`、字段/rule/边界和 `request_id`，不返回输入值、正则原文、堆栈或内部路径；前端将可定位错误回填到 Field，无法定位时显示摘要并保留 request ID。
