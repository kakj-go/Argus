# M5：交互卡片闭环

## 目标

让系统 Card 和企业 Card 在不暴露 Tool/Token/私有参数的前提下展示裁剪数据、执行绑定查询和触发确定性动作。

## 前置条件

- M1 Card Host 安全基座完成。
- M4 Tool Result、ActionBinding 和 Action Executor 完成。

M4 已提供公开 `result_ref`、PendingAction/Approval/Execution、隐藏 Commit Catalog 和异步一次性结果领取。M5 只消费这些公共边界，不读取私有 Plan/Token，也不把 Enrollment 一次性结果注入 Card 数据绑定。

## 任务

- [x] `M5-MANIFEST-01` 实现版本化 Manifest、内容哈希、CSP 能力和不可变 CardVersion。
- [x] `M5-RUNTIME-01` 将 M1 已完成的独立 Origin、`window.argusCard`、CSP/内容哈希和 MessagePort Bridge 接入服务端 CardVersion 与 RenderPlan 校验，补齐运行时发布、缓存和治理；不得重做第二套浏览器传输协议或允许 Card 绕过 Binding ID API。
- [x] `M5-BINDING-01` 实现 DataSlot、QuerySlot、ActionSlot、SlotBinding 和 Schema Catalog。
- [x] `M5-RENDER-01` 实现 RenderPlan 校验、`tool_call_id + path` 来源追踪和最小数据投影。
- [x] `M5-SYSTEM-01` 先交付只读资源列表、PendingAction 预览和遥测基础系统 Card。
- [x] `M5-ACTION-01` 实现 Card → Host → ActionBinding → Action Executor，iframe 不直接调用 Tool。
- [x] `M5-ENTERPRISE-01` 实现企业 Card 创建、默认禁用、Revision、Demo、验证和启用门禁。
- [x] `M5-GOVERNANCE-01` 实现停用、历史版本引用、审计和权限目录投影。
- [x] `M5-WEB-01` 完成 Card 列表、预览、Slot Binding、Demo 和会话内渲染。

## 测试

- 错误 Origin、伪造 nonce、重复/乱序消息、超大消息和销毁后消息被拒绝。
- CSP 阻止未声明脚本、网络、字体、图片和动态代码能力。
- 伪造 Binding ID、跨用户/企业/会话使用和过期 Binding 被拒绝。
- 默认/空/错/大数据、双主题、双语言和可访问性 Demo 全部通过。
- 浏览器网络、DOM、日志和缓存中不存在私有 Token、参数或任意 Commit Tool 名称。

## 退出标准

- 系统 Card 可以安全完成真实 Tool Result 展示和 PendingAction 确认。
- 企业 Card 只有通过全部门禁才可被 Render Skill 发现。
- 不支持 Card 的客户端仍可使用安全文字降级流程。

## 不包含

- 个人 Card。
- iframe 任意 Tool 调用或任意外网。
- M1 已完成的 Card Host、独立 `card-runtime` 构建、精确 Origin 握手、CSP、nonce、序号、大小和 Binding ID 浏览器门禁。

## 完成证据

截至 2026-08-17，M5 已完成。旧 Shell Harness 最终成功运行号为 `20260817211415-4363`，脱敏证据位于 `artifacts/m5-e2e/20260817211415-4363`。该运行覆盖 M2-M4 回归、两版企业 Card 的八场景浏览器验证、系统/企业选择、explicit resource authorization 撤权与重新物化、Action Binding 幂等、非创建人审批、Commit/Verify、回滚、Redis 清空和 Server 重启恢复；`m4-runtime-state.txt`、`m5-card-state.txt` 与 `cleanup.txt` 均已生成，运行结束后临时 Namespace、PVC 和 Lease 均无残留。当前官方入口为 `go run ./cmd/argus-dev e2e run --suite m5`。
