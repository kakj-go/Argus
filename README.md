# Argus

Argus 是一个面向 AIOps 场景的多企业 SaaS 控制平面，使用 Chatbox、MCP Tool、受控 Card Skill、Connector 和 OpenTelemetry 完成资源接入、查询、诊断与两阶段变更。

当前仓库处于设计阶段，完整架构、运行流程和第一版边界见 [docs/README.md](./docs/README.md)。实现前应首先阅读 [已决策事项与系统不变量](./docs/00-decisions-and-invariants.md)；前后端与工程选型见 [第一版技术栈与代码结构](./docs/12-technology-stack-and-code-structure.md)。

## 本地运行

需要 Go 1.25 或更高版本：

```bash
go mod download
go run ./cmd/argus-server
```

默认监听 `:8080`，可通过 `ARGUS_HTTP_ADDRESS` 修改：

```bash
ARGUS_HTTP_ADDRESS=127.0.0.1:18080 go run ./cmd/argus-server
curl http://127.0.0.1:18080/healthz
```

基础检查：

```bash
make fmt
make test
make vet
```

## 前端开发

前端采用 pnpm workspace，按设计文档拆分为初始化、平台管理和企业三个门户，并共享唯一的 UI 组件与 Design Token：

```bash
corepack pnpm install
corepack pnpm dev           # 企业门户 http://localhost:4173
corepack pnpm dev:platform  # 平台门户 http://localhost:4174
corepack pnpm dev:setup     # 初始化门户 http://localhost:4175
```

企业门户默认进入 AI 会话工作台；`/demo` 为共享组件与业务卡片展厅。三个门户和共享组件均支持浅色/深色主题及 `zh-CN`/`en-US` 切换，偏好会保存在本地启动缓存中。正式接入用户 Profile 后由服务端偏好负责跨设备同步；API 客户端会随请求发送 `Accept-Language`。

前端质量检查：

```bash
corepack pnpm typecheck
corepack pnpm lint
corepack pnpm test
corepack pnpm build
corepack pnpm e2e
```
