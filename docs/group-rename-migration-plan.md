# 分组无感知重命名方案

## 1. 背景与结论

当前项目的“分组”不是独立实体，而是被持久化在用户、API Key、渠道、套餐和多份配置中的字符串。系统设置页直接把 `vip` 改为 `premium`，等价于在倍率配置中删除 `vip`、新增 `premium`；它不会迁移任何旧引用。

因此，分组改名必须是一个显式的、受控的迁移操作，不能通过比较保存前后的 `GroupRatio` JSON 自动推断。自动推断无法区分“改名”与“删除旧分组后新建另一个分组”，有把用户错误迁移到无关分组的风险。

本方案在不重构为 `group_id` 的前提下，实现“`old_name -> new_name`”的无感知改名：用户所属分组、已有 API Key、渠道路由、套餐和网页 Playground 选择都会继续落到新分组；改名期间请求短暂等待而不是路由到旧配置或返回分组不存在。

## 2. 目标、范围与非目标

### 2.1 目标

以 Root 管理员把 `vip` 重命名为 `premium` 为例，完成后应满足：

1. 所有原本属于 `vip` 的用户，数据库和 Redis 缓存都成为 `premium`，不需要重新登录。
2. 固定使用 `vip` 的 API Key 自动改为 `premium`；客户端不需要重新创建、替换或修改 Key。
3. 原本由 `vip` 路由的渠道、能力、倍率、可用分组、限流和套餐规则都继续作用于 `premium`。
4. Playground 本地存储的 `vip` 选择会被映射为 `premium`，不会静默回退到 `default`。
5. 改名时进行中的请求可以按已经建立的上下文完成；尚未开始渠道选择的请求最多等待迁移完成，不出现旧/新配置混用。
6. 历史日志、用量、已完成任务保留旧分组名，确保账务和审计可追溯。
7. 全过程记录管理审计日志；失败时数据库和运行时配置均保持改名前状态。

### 2.2 本次范围

- 新增 Root 专用的“分组重命名”预检与执行接口。
- 在 default 与 classic 前端的分组倍率页面增加重命名入口、影响预览与二次确认。
- 收敛分组新增、删除和改名的配置入口，防止 JSON 编辑器、classic 主题或通用 Option API 绕过迁移。
- 为浏览器 Playground 提供短期别名映射，修复 localStorage 中的旧选择。
- 迁移所有会影响用户权限、请求路由、计费倍率、充值和订阅后续行为的当前引用。
- 迁移后刷新 Redis、Token、渠道和定价缓存。
- 为单节点和多节点部署规定一致性策略与发布条件。

### 2.3 非目标

- 不修改终态任务、日志、用量和性能指标中的历史分组值；仍在排队、执行或待结算的任务属于业务状态，不适用此规则。
- 不允许把多个旧分组合并为一个新分组；该需求应另行设计“分组合并”工作流。
- 不在本次将所有字符串分组引用全面替换为不可变 `group_id`。该架构是长期优化方向，但会改变外部 API、配置模型和数据迁移范围。

## 3. 当前模型与问题边界

“用户所属分组”“请求使用分组”和“前端当前选择”是不同概念，重命名必须分别处理。

| 概念 | 当前存储/来源 | 改名后风险 | 处理方式 |
| --- | --- | --- | --- |
| 用户所属分组 | `users.group` | 用户权限、默认路由与充值倍率仍指向旧名 | 原子迁移并失效用户缓存 |
| API Key 固定分组 | `tokens.group` | Key 被判为已弃用或不能路由 | 原子迁移并失效 Token 缓存 |
| Playground 当前选择 | 浏览器 localStorage | 现有逻辑会回退 `default` | 返回别名映射并先映射再请求模型 |
| 渠道路由分组 | `channels.group`、`abilities.group` | 新组没有渠道，模型列表/转发失败 | 更新渠道、重建能力和渠道缓存 |
| 分组规则 | `options` 内多份 JSON | 倍率、可用性、自动路由和限流断裂 | 在同一迁移中改写所有键和值 |
| 套餐分组 | `subscription_plans`、`user_subscriptions` | 后续升级、到期降级回到失效分组 | 迁移计划与有效订阅快照 |
| 未终态异步任务 | `tasks.group`、`private_data.billing_context` | 差额结算重新查旧组倍率，可能错误扣费/退款 | 迁移活跃任务并优先使用提交时倍率快照 |

当前管理员配置页的分组行编辑仅修改 `GroupRatio`、`TopupGroupRatio`、`UserUsableGroups` 等配置值；它没有重命名接口或数据迁移入口。对应代码位于 `web/default/src/features/system-settings/models/group-ratio-visual-editor.tsx`。

## 4. 产品交互与权限

### 4.1 入口

在“系统设置 → 计费/模型 → 分组倍率”每个非保留分组行的操作列新增“重命名”。`default` 与 `auto` 为保留名称，不展示该操作。

单独编辑名称的输入框仍可用于尚未保存的新分组草稿；已存在的分组名称修改必须拦截，提示使用“重命名”操作。删除已存在分组也必须走受控的“停用/迁移”工作流。这样能避免用户绕过迁移路径。

这不是仅靠前端拦截的约束。新增 `PUT /api/group/config`，以“完整分组配置快照 + revision”一次性保存 `GroupRatio`、`TopupGroupRatio`、`UserUsableGroups`、`GroupGroupRatio`、`AutoGroups`、特殊可用组配置和分组限流；default 与 classic 的分组页均使用该接口。通用 Option 更新接口拒绝直接改写会定义分组身份或授权图的 `GroupRatio`、`UserUsableGroups`、`GroupGroupRatio`、`AutoGroups` 与特殊可用组配置，避免用 JSON 编辑器绕过迁移。`TopupGroupRatio` 与 `ModelRequestRateLimitGroup` 保留各自设置页的独立编辑能力，但写入与重命名会共用同一个分组迁移闸门；改名操作仍在一个事务中重写全部七项配置。该接口允许新增分组与修改不改变既有分组键集合的规则；删除或改名任何既有分组一律拒绝，并提示改用分组迁移或停用接口。

### 4.2 预检与确认

点击后打开对话框，输入新名称并调用预检接口。预检必须展示：

- 将迁移的用户数、固定分组 API Key 数、渠道数、能力记录数。
- 将更新的套餐计划数、有效订阅数、分组规则项数和限流规则项数。
- 是否有冲突、保留名称、无效字符或待处理的分组迁移。
- 明确说明：历史日志和已完成任务的分组名称不会被修改。

执行按钮要求输入完整旧分组名确认，且仅 Root 可用。普通管理员即使拥有渠道或用户管理权限，也不能进行分组迁移；它跨越用户权限、计费和订阅边界。

### 4.3 用户侧表现

- 用户无需登录、重新创建 Key 或手动修改 API Key 分组。
- 已打开的网页在下一次读取可用分组时把本地旧值改为新值；不弹出打扰性提示。
- 进行中的流式/异步任务保持提交时的账务快照；之后的新请求使用新分组。

## 5. API 设计

接口放在现有 `/api/group` 路由下，均使用 `middleware.RootAuth()`，并额外采用 `CriticalRateLimit`。

### 5.1 预检

`POST /api/group/rename-preview`

请求：

```json
{
  "old_name": "vip",
  "new_name": "premium"
}
```

成功响应：

```json
{
  "success": true,
  "data": {
    "old_name": "vip",
    "new_name": "premium",
    "revision": "sha256:...",
    "affected": {
      "users": 128,
      "tokens": 203,
      "channels": 4,
      "abilities": 18,
      "subscription_plans": 2,
      "active_subscriptions": 16,
      "config_references": 11
    },
    "warnings": []
  }
}
```

`revision` 是基于旧名、新名及所有待写配置快照的摘要。执行时必须携带它，以防管理员在预检和确认之间修改了分组设置。

### 5.2 执行

`POST /api/group/rename`

```json
{
  "old_name": "vip",
  "new_name": "premium",
  "revision": "sha256:...",
  "confirmation": "vip"
}
```

成功后返回实际影响计数和迁移版本。后端审计事件为 `group.rename`，参数只记录旧/新名称、计数和版本，不记录任何敏感配置值。

### 5.3 用户别名读取

新增 `GET /api/user/group-aliases`（`UserAuth`）。响应只返回仍在有效期内、且当前用户有权使用目标分组的映射：

```json
{
  "success": true,
  "data": {
    "vip": "premium"
  }
}
```

Playground 在读取本地配置后先应用映射、保存新值，再查询 `/api/user/groups` 与模型列表。这样不会产生一次失败的旧分组请求。

### 5.4 分组配置原子保存

`PUT /api/group/config` 使用 `AdminAuth`，但若键集合发生删除或改名则必须拒绝，要求 Root 改走迁移流程。请求携带当前配置的 `revision` 和完整配置快照；服务端在一个事务中校验、写入所有相关 Option，再一次性应用运行时配置。

这个接口替代分组页对 `/api/option` 的逐项调用。它必须返回新 revision，并保留“新增空分组”“修改倍率/描述/可用性”等日常管理能力，不能因为收敛入口而削弱正常配置功能。

## 6. 数据与配置迁移清单

迁移服务必须维护一份集中、可测试的引用清单。新增分组相关字段时，必须同时更新该清单和回归测试。

### 6.1 必须迁移的关系数据

| 对象 | 字段 | 迁移规则 |
| --- | --- | --- |
| 用户 | `users.group` | 精确等于旧名时替换为新名 |
| API Key | `tokens.group` | 精确等于旧名时替换为新名；空值与 `auto` 不变 |
| 渠道 | `channels.group` | 逗号分隔列表按元素精确替换、去重并保持原顺序 |
| 渠道能力 | `abilities.group` | 不直接更新复合主键；删除受影响渠道能力后，按更新后的渠道重新生成 |
| 套餐计划 | `subscription_plans.upgrade_group`、`downgrade_group` | 精确替换 |
| 用户订阅快照 | `user_subscriptions.upgrade_group`、`prev_user_group`、`downgrade_group` | 精确替换，保证到期结算不会回退旧名 |
| 非终态任务 | `tasks.group` | 精确替换；只处理 `NOT_START`、`SUBMITTED`、`QUEUED`、`IN_PROGRESS` 等未结算状态 |

不能对渠道组使用 `REPLACE(group, old, new)`。例如把 `vip` 替换为 `premium` 时，`vip-plus` 不应变化；必须解析逗号列表、逐项比较、校验长度后再写回。

### 6.2 必须迁移的选项配置

| Option key | 旧名可能所在位置 |
| --- | --- |
| `GroupRatio` | 顶层 map key |
| `TopupGroupRatio` | 顶层 map key |
| `UserUsableGroups` | 顶层 map key |
| `GroupGroupRatio` | 顶层“用户所属分组”key 和内层“使用分组”key |
| `AutoGroups` | 字符串数组元素 |
| `group_ratio_setting.group_special_usable_group` | 顶层 key，及内层普通、`+:`、`-:` 前缀分组 key |
| `ModelRequestRateLimitGroup` | 顶层 map key |

选项 JSON 均使用 `common.Unmarshal` / `common.Marshal`，不能在业务代码中直接调用 `encoding/json`。解析后必须验证：新名称没有与已有键冲突、倍率仍为合法非负数、限流值仍在合法范围。

### 6.3 活跃任务与历史记录

终态任务、`logs.group`、`quota_data.use_group`、`perf_metrics.group` 以及已写入的审计内容不修改。这些字段描述事件发生时的真实分组；修改会破坏账单核对和故障追溯。性能指标查询需在别名有效期内将旧/新分组聚合展示，避免管理端趋势图在改名时断档。

未终态任务不是历史记录，必须在事务内把 `tasks.group` 改为新名。所有新建任务都必须写入完整的 `TaskBillingContext`；异步差额结算必须优先使用其中的 `GroupRatio`，不允许重新按 `task.Group` 查询当前倍率。对于升级前遗留且缺少快照的未终态任务，结算读取旧名时先经别名解析为新名，再按新配置计算；该兼容路径在没有此类任务后移除。

### 6.4 字段容量与数据库迁移

分组名最长 64 字符的约束只有在所有持久化字段都能容纳它时才成立。当前 `tasks.group` 为 `varchar(50)`，`channels.group` 还是包含多个逗号分隔名称的 `varchar(64)`；直接改名可能截断、失败或让一个渠道的组合分组超长。

本方案采用“先扩容、后启用”的路径：将 `tasks.group` 扩为 `varchar(64)`，将 `channels.group` 扩为 `varchar(4096)`，然后才开放重命名入口。`varchar(4096)` 保留 MySQL 5.7 可用的默认值语义，同时能容纳多个 64 字符分组的逗号列表。迁移必须在 `model/main.go` 中按 SQLite、MySQL、PostgreSQL 分支实现：SQLite 的类型亲和性本身不强制 varchar 长度；MySQL 与 PostgreSQL 使用各自有效的列类型迁移。预检还必须校验迁移后每个字段值的长度；任何未完成的字段迁移都阻止执行重命名。

## 7. 后端实现设计

### 7.1 分层与职责

- `dto/group.go`：预检、执行、影响计数和别名响应 DTO。
- `controller/group.go`：参数绑定、Root 权限、错误响应和审计记录，不包含迁移细节。
- `service/group_rename.go`：预检、名称校验、配置转换、编排迁移和运行时刷新。
- `model/group_rename.go`：仅负责跨表查询、事务内数据更新、Option 行写入和别名持久化。
- `model/group_rename_alias.go`：别名表查询、清理与运行时缓存接口。

`GroupRenameAlias` 必须同时注册到 `migrateDB` 和 `migrateDBFast` 的 `AutoMigrate` 列表；字段扩容迁移在模型 AutoMigrate 前运行。新增表/列和索引须在 SQLite、MySQL 5.7.8+、PostgreSQL 9.6+ 都有确定行为。

名称校验统一拒绝空白、超过 64 字符、逗号、控制字符、`default`、`auto` 和与任何既有分组重复的新名称。旧名必须存在于 `GroupRatio`；新名称必须不存在于 `GroupRatio`，避免合并语义和复合主键冲突。

### 7.2 别名表

新增 `group_rename_aliases`：

| 字段 | 说明 |
| --- | --- |
| `old_name` | 主键，旧分组名 |
| `new_name` | 规范分组名 |
| `expires_at` | 30 天后过期 |
| `created_at` | 创建时间 |
| `created_by` | 执行 Root 用户 ID |
| `migration_version` | 用于排障和多节点同步 |

别名只用于兼容浏览器本地选择和 `/pg` 请求体中的旧 `group`。服务端必须在分组可用性/权限检查之前将旧名解析为新名，再按新名重新做授权和渠道校验；别名绝不能扩大任何用户的可用分组。解析时最多跟随一跳，禁止别名链；若目标又被重命名，写入时压平到最终名称。过期别名返回“分组不可用”，不永久保留失效协议。

### 7.3 原子事务

执行服务按以下顺序工作：

1. 获取分组迁移独占锁，拒绝或等待其他迁移完成。
2. 在事务内用 `lockForUpdate(tx)` 锁定旧名与新名对应的 Option 行、待迁移渠道和订阅记录；SQLite 自动退化为不加 `FOR UPDATE`。
3. 重新读取并验证 `revision`、名称冲突、预检计数和引用完整性。
4. 更新所有关系数据、改写完整的 Option JSON，并写入别名记录。
5. 删除受影响渠道的 `abilities`，依据更新后的 `channels.group` 在**同一个事务**重建能力。不得直接更新 `abilities.group`，它是复合主键的一部分。
6. 提交事务；任何一步失败立即回滚，运行时状态尚未变更。
7. 提交成功后应用新的配置快照、刷新缓存，最后解除请求闸门。

不得调用 `model.UpdateOption` 多次完成这项迁移：该方法每次单独提交，异常时会留下半迁移配置。应新增可复用的 `UpdateOptionsBulkWithTx(tx, values)`，使 Option 行和业务表共享同一个数据库事务；成功提交后再一次性调用运行时配置应用逻辑。通用 Option 更新在没有迁移上下文时不得删除或重命名仍有引用的分组。

### 7.4 缓存与会话刷新

提交后必须完成以下动作才可返回成功：

1. 在事务内一次性预取受影响用户 ID 与 Token key；提交后按固定批次 pipeline 删除用户和 Token Redis 缓存，避免按用户调用 `InvalidateUserTokensCache` 形成 N+1 查询。
2. 清理渠道亲和路由缓存；它的 key 可以包含使用分组。亲和用量统计作为历史指标保留旧名，查询层在别名有效期内聚合旧/新名称。
3. 调用 `model.InitChannelCache()`，它会同步失效定价缓存；不再额外在持有渠道缓存锁时调用 `InvalidatePricingCache()`，以遵守现有锁顺序。
4. 失效用户可用分组及别名的前端查询缓存。
5. 不读取 `session["group"]` 作为分组权限或路由真相；鉴权和 Playground API 应以用户缓存/数据库中的当前值为准。现有 session 字段仅可作为展示兼容字段，并在下一次认证响应中刷新。

## 8. 无感知一致性与多节点策略

### 8.1 单节点

新增进程内 `GroupMutationGate`：分组相关的“读取用户组 → 检查可用组 → 选渠道”路径持有读锁；迁移持有写锁直到数据库提交、Option 快照和渠道缓存均已更新。正常请求只增加一次内存读锁；改名期间请求等待，不会看到一半新用户和一半旧渠道。

### 8.2 多节点

第一期只允许在线实例数为 1 的部署执行重命名。实例数以 `SystemInstance` 的 90 秒心跳为准；预检或执行时发现第二个在线实例立即拒绝，不能抱着“随后实现多节点协议”的假设继续执行。Redis 用于共享缓存或后续协调时，必须先配置一致的 `CRYPTO_SECRET`。

多节点支持是独立的启用前置条件：必须启用 Redis 并完成下面的协议、故障演练和负载均衡摘除能力后，才允许在多节点打开该功能。流程如下：

1. Root 节点取得 Redis 分布式锁 `group-rename:lock`，创建迁移版本。
2. 向存活节点广播 `prepare(version)`；各节点关闭本地请求闸门并确认已进入等待状态。
3. 所有节点确认后，Root 执行数据库事务。
4. Root 广播 `apply(version)`；各节点重新载入相关 Option、别名、渠道与定价缓存，清理 Redis 用户/Token 缓存，并确认完成。
5. Root 收到全部确认后广播 `resume(version)`，各节点恢复请求；管理接口才返回成功。

若任一节点在第 2 步超时，事务不开始；若第 4 步失败，该节点必须从负载均衡摘除后再恢复服务，不能让它继续处理请求。迁移事件和 ACK 记录至少保留 24 小时，供重启节点补偿应用。必须定义请求闸门最大等待时间、超时响应和运维告警；超过该时间不允许无限阻塞客户端。

正在进行的请求可以使用开始时已建立的 `RelayInfo`、分组倍率和任务账务快照完成，不强制中断；闸门只覆盖尚未解析分组或选择渠道的新请求。

## 9. 前端实现设计

### 9.1 管理页

修改默认和 classic 两套分组管理页：

- 已保存的行禁用直接改名和直接删除；新增“重命名”菜单项。
- 新建行仍允许直接输入名称。
- 使用 React Query 管理配置快照、预检和执行 mutation；分组配置保存调用 `/api/group/config`，执行成功后失效系统设置、用户、渠道、套餐和定价相关 query。
- 所有新增文案使用 `useTranslation()` 与现有英文键，不硬编码用户可见文本；运行 `bun run i18n:sync` 补齐语言文件。

### 9.2 Playground

在 `use-playground-options` 读取用户分组前查询 `group-aliases`。如果 localStorage 当前组命中映射，先调用 `updateConfig('group', newName)`，再发起模型列表查询。若映射请求失败，沿用当前安全回退逻辑；不能把旧名直接提交给后端。

API Key 创建/编辑页不需要额外 UI 迁移，因为服务端已经更新 `tokens.group`。重命名完成后刷新打开的 Key 表单所依赖的 `user-groups` 查询即可。

项目仍支持 classic 主题，因此不能只改默认前端。classic 的管理页也必须移除直接改名/删除入口；`web/classic/src/hooks/playground/useDataLoader.js` 同样需要在加载 `USER_GROUPS` 前应用别名，并以 `/api/user/self` 的最新 `group` 覆盖 localStorage 中陈旧的用户信息；找不到别名时才沿用现有的首个可用组回退。两套主题的行为必须通过同一 API 合约保持一致。

## 10. 测试与验收

### 10.1 后端确定性测试

新增 `service/group_rename_test.go` 与必要的 `model` 测试。所有新 Go 测试使用 `require` 完成前置和致命断言，使用 `assert` 检查多项结果。

| 场景 | 关键断言 |
| --- | --- |
| 完整迁移 | 用户、Key、渠道、能力、套餐、有效订阅和所有配置引用均从旧名变新名 |
| 渠道精确替换 | `vip` 被改名但 `vip-plus` 不变；重复分组被去重且顺序稳定 |
| 规则嵌套迁移 | `GroupGroupRatio` 与特殊可用组中的外层、内层及 `+:`/`-:` 均正确改写 |
| 缓存 | 受影响用户缓存与 Token 缓存失效；重读后使用新组 |
| 路由 | 改名后的固定分组 Key 能列模型、选择渠道并完成一次受控 relay 前置路径 |
| 套餐生命周期 | 改名后购买升级、订阅到期降级不写回旧名 |
| 活跃异步任务 | 改名期间仍在运行的任务以 BillingContext 快照结算；遗留无快照任务经别名解析，不按缺失旧组回退默认倍率 |
| 回滚 | 在模拟的 Option/能力/订阅写入失败后，所有表和 Option 保持改名前状态 |
| 冲突与保留名 | 新名已存在、`default`、`auto`、逗号、过长名称全部拒绝 |
| 配置绕过 | JSON 编辑、classic 页面与通用 Option API 无法绕过 `/api/group/config` 删除或改名仍被引用的分组 |
| 容量迁移 | 64 字符分组及多分组渠道不会被字段长度截断；SQLite、MySQL、PostgreSQL 均通过迁移验证 |
| 并发 | 迁移期间分组解析请求阻塞并在恢复后只看到新快照 |
| 别名 | 本地旧值和 `/pg` 旧请求在有效期内解析新组；过期后拒绝 |

数据库相关测试至少在 SQLite 覆盖完整事务语义；CI/发布前应在 MySQL 5.7.8+ 和 PostgreSQL 9.6+ 的兼容矩阵运行，确认没有数据库特定 SQL、JSON 类型或锁语法。

### 10.2 前端测试

- 重命名预检展示准确的影响计数，确认文本不匹配时执行按钮禁用。
- 执行成功后系统设置查询失效、表格展示新名称。
- Playground 以 `vip` 作为 localStorage 初始值且别名为 `vip -> premium` 时，首个模型请求使用 `premium`。
- default 与 classic 两套 Playground 都会把旧本地选择映射为新名称，且不会把旧 `user.group` 写回 localStorage。
- default 与 classic 管理页都不能通过普通保存、删除或 JSON 编辑绕过迁移流程。
- 未命中别名时保留现有“优先 default，否则第一项”的回退行为。

变更 TS/TSX 后执行：

```sh
cd web/default
bun run typecheck
bun run lint
bun run i18n:sync
bun run build
```

### 10.3 发布验收清单

1. 在预发布创建含用户、固定 Key、逗号多分组渠道、套餐和有效订阅的样本数据。
2. 用 Root 将 `vip` 重命名为 `premium`，确认预检数量与实际一致。
3. 保持一个普通用户已登录、一个 API Key 客户端持续请求、一个 Playground 页面带旧 localStorage 值，执行改名。
4. 确认三类会话均无需人工处理即可继续成功；改名前的用量日志仍显示 `vip`，改名后的新日志显示 `premium`。
5. 在多节点环境验证 prepare/apply/resume ACK、节点重启补偿和超时阻断。

## 11. 回滚、观测与运维

### 11.1 回滚

事务提交前，任意错误自动回滚。提交后不提供“数据库自动回滚”按钮：管理员应执行一次反向重命名 `premium -> vip`，它经过同样的预检、审计和缓存刷新流程，避免人工 SQL 破坏一致性。

若需要灾难恢复，改名前的数据库备份与 `group.rename` 审计中的迁移版本可用于定位；审计不保存完整 Option JSON，敏感或大配置应依赖常规数据库备份恢复。

### 11.2 指标与告警

增加下列结构化日志和指标：

- `group_rename_total{result}`、`group_rename_duration_seconds`。
- `group_rename_affected_users/tokens/channels/subscriptions`。
- `group_rename_node_ack_lag_seconds`、`group_rename_waiting_requests`。
- `group_alias_resolved_total`、`group_alias_expired_total`。

告警条件：事务回滚、节点 apply 超时、别名解析到不存在的目标组、迁移后固定 API Key 的“分组已弃用”错误显著上升。

## 12. 实施顺序与完成标准

1. 先完成字段扩容与 `GroupRenameAlias` 的三数据库迁移，并验证回滚路径。
2. 建立引用扫描、通用 Option 保护与预检服务，补齐所有配置映射的纯函数单测。
3. 实现单节点事务迁移、活跃任务账务快照修复、能力重建、批量缓存刷新与审计。
4. 接入 Root API、默认与 classic 管理页的预检/确认交互。
5. 实现别名表、用户别名接口与两套 Playground localStorage 自动映射。
6. 实现 `GroupMutationGate`、端到端回归测试和性能验证；第一期在多节点时拒绝执行。
7. 另行实施 Redis prepare/apply/resume 协议、节点故障演练和负载均衡摘除，验收后才启用多节点改名。
8. 在预发布按第 10.3 节验收后发布。

只有当“已登录用户、固定分组 API Key、带旧 Playground 本地选择、渠道和有效订阅”均在无需人工操作的情况下正确迁移，且失败场景能完整回滚、历史账务不被改写时，才能认为本功能完成。
