# ADR: 显式资源数据授权

## 状态

已采纳。

## 背景

旧模型把 DataScope、RoleBinding.data_scope_ids 和动态标签选择器混在一起，导致资源列表、远程访问、审批和遥测对授权结果的解释不一致，也无法可靠展示继承来源。

## 决策

1. 使用 data_authorization_grants 保存显式授权关系。主体类型为 user、department、role、service_account；资源类型为 host、kubernetes_cluster。
2. RoleBinding 只表示角色与主体的功能权限绑定，不再携带数据范围。
3. 用户有效资源是用户直接授权、所属部门授权和已绑定角色授权的并集。ServiceAccount/APIKey 使用 ServiceAccount 直接授权及其角色授权。
4. 不支持 Deny，也不支持从用户侧排除继承资源。用户只能移除自己的直接授权；继承授权必须在角色或部门上修改。
5. 创建操作只检查功能创建权限，不要求已有资源授权。资源创建事务成功后，为实际创建主体写入该资源的只读授权。
6. Kubernetes 只授权 Cluster，Namespace、Pod、Workload 等下级对象继承 Cluster 授权。
7. 资源标签保留用于展示、搜索、普通列表筛选和保存视图，标签变化不改变授权结果。删除标签选择器及其规范化、评估和影响分析。
8. 授权修改使用主体授权版本和 expected_version 做乐观并发控制；变更写入审计并发送失效通知。

## 交互

角色、用户、部门和 ServiceAccount 列表均提供“数据授权”入口。弹框使用 Host、Kubernetes 两个 Tab 和统一双栏资源移动组件，显示直接或继承来源；继承资源不可移除。角色授权保存前显示受影响成员数量并要求风险确认。

正式 REST 入口为 GET/POST /api/v1/enterprise/data-authorizations/{subject_type}/{subject_id}。GET 接受 resource_type 查询参数并返回资源名称、授权状态和来源；POST 接受 resource_type、resource_ids、remove、expected_version，服务端在事务中批量变更并触发版本失效。OpenAPI、Go 和 TypeScript 类型已通过生成工具同步。

## 后果

授权查询更容易缓存和审计，所有资源入口可复用同一结果。迁移允许删除旧 DataScope 绑定表和动态选择器字段，不提供历史兼容层。
