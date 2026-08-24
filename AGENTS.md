# 项目说明
目前项目还在开发阶段，一些重构和修改不用考虑历史数据的兼容，可以直接清理

## 约束

1. 先阅读本文件和与任务相关的 `docs/` 文档；总索引见 `docs/README.md`。如果我要求的实际开发内容和文档对不上可以说明下，如果没有疑问应该将文档更新
2. 前端文件代码行数不要超过2000行
3. 后端文件代码行数不要超过2000行
4. 前端代码要保证 ui，风格，样式，组件保持一致，前端应该是统一的组件库，样式库这样来保证整体风格，字体，大小，ui，排列一致性
5. 前端代码和后端代码的开发新功能和 bug 解决要从底层架构和全局来思考而不是为了临时解决和缝合代码
6. 所有代码要考虑高内聚低耦合等开发规范
7. 如果实现需要改变已确定的架构边界，先说明原因和影响，并同步更新相关设计文档。
8. 整体功能需要有 E2E 端到端自动化测试，整体流程测试具体测试可以 k8s 创建临时使用的 namespace 来实现，测试完成之后删除，测试期间可以先将正常部署的命名空间所有服务停止来缓解资源压力

## 前端约定

- 应用与共享包：`web/apps/{enterprise,platform,setup}` + `web/packages/{ui,design-tokens,api-client,auth,card-host,observability}`；通用组件只进 `@argus/ui`，业务应用不维护平行组件库。
- API 模式：Enterprise 与 Platform 两个门户必须显式设置 `VITE_API_MODE=mock|real`；Platform 内含首次初始化流程。未知模式或 real 缺少必要 URL 时 fail closed，不得回退 mock。mock 数据持久化在 localStorage，Playwright 每个用例独立 browser context 即得到干净种子数据。
- i18n：每个业务模块一个 `src/i18n/<module>.ts`，导出 `<module>Zh` / `<module>En`，在 `src/i18n/index.ts` 的模块清单中注册即生效；通用文案放 `common.ts`。默认 `zh-CN`，偏好持久化在 `argus.locale`。
- 样式：组件类名统一 `.argus-*` 前缀；颜色、字号、间距、圆角只引用 design token（`var(--*)`），禁止硬编码；页面级样式放 `src/styles/*.css`。
- E2E：`web/apps/enterprise/e2e/`，`cd web/apps/enterprise && pnpm e2e`（Playwright 自动启动 dev server :4173）；`playwright.config.ts` 已剥离本机代理环境变量，loopback 请求始终直连。
