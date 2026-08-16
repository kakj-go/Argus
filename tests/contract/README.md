# Contract tests

本目录验证 OpenAPI、JSON Schema、protobuf、Preview/Commit、Card Bridge、
Agent Context、可信遥测身份和旧前端契约只减不增基线。

每个 OpenAPI 原生 DTO、JSON Schema 根节点和 `$defs` 都会生成并验证正常、
边界和非法 Fixture；关键协议继续保留手写 Fixture。额外语义测试覆盖选择器
规范化/哈希、合法状态迁移、PendingAction 三层存储隔离、SSE 终止、Bridge
Origin/nonce/序号/Binding/消息大小、ContextSnapshot 的 ToolCall、PendingAction、
Execution 完整事件组切点、原始事件不变性和递归私有字段过滤。

旧前端契约基线使用文件路径、匹配行内容 SHA-256 和出现次数的精确清单。
M1 删除旧引用时无需更新基线；只有确认重建基线时才运行：

```text
make contract-update-legacy-baseline
```

常用命令：

```text
make contract-lint
make contract-generate
make contract-check
make contract-breaking
```

首次合并 M0 时 `origin/main` 没有旧契约，兼容性测试会建立基线；此后删除字段、
收窄类型、删除枚举/错误码/状态迁移或破坏 protobuf 会导致检查失败。
