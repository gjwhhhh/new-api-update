# 渠道测试结果历史设计

## 背景

自动渠道测试目前会把成功测试写入“使用日志”，但遇到上游 `502`、`504`、连接失败、响应体不合法等情况时，会在 `testChannel` 中提前返回。失败没有可结算的 token/费用，因此不应写成消费记录；最终只保留后端运行日志和系统任务的汇总数字。

这会导致管理员无法从控制台回答两个实际问题：某一轮是否测到了某渠道，以及该渠道为什么失败。会话审计和性能指标也不能替代这项能力：前者记录的是客户端会话，后者只汇总健康度，不保留一次测试的失败原因。

## 目标与边界

目标：新增可查询、可保留、不会泄露密钥或请求内容的“渠道测试记录”，覆盖定时测试、手动全量测试和手动单渠道测试。

非目标：

- 不把失败测试伪装成使用/消费日志，不改变计费、额度、渠道成本或真实调用成功率。
- 不保存请求 Body、响应 Body、鉴权头、上游原始错误文本或 Key 内容。
- 一期不做趋势图、告警规则或逐 Key 的完整状态时间线；先提供可准确排障的逐次结果。

## 方案选择

| 方案 | 优点 | 问题 | 结论 |
| --- | --- | --- | --- |
| 在 `system_tasks.result` 存每个渠道结果 | 改动表结构少 | JSON 难分页、过滤和按渠道追溯；手动单测没有系统任务；历史会快速膨胀 | 不采用 |
| 失败也写入使用日志 | 现有页面立刻可见 | 将 0 token/0 费用的失败误解为消费，污染计费、成本和成功率 | 不采用 |
| 新增独立测试结果表和页面 | 可按渠道、时间、任务和失败类型查询，语义正确 | 需要迁移、保留任务和 UI | 推荐 |

推荐方案保留现有**成功**测试的使用日志行为，确保已有成本追踪和页面行为不突变；新表同时记录成功与失败，作为健康检查的唯一历史来源。失败继续不产生消费日志。

## 数据模型

在主数据库增加 `channel_test_results` 表，不使用日志库，也不建立对渠道表的外键或级联删除。这样渠道重命名、删除后，历史仍可追溯。

日志库虽然在语义上也能容纳历史记录，但本项目的日志库可能是 ClickHouse；若放入日志库，需要额外维护 ClickHouse DDL、TTL、删除和稳定分页语义。首期采用“默认关闭、7 天保留、分批清理”的主库方案，降低数据库方言和部署复杂度。

| 字段 | 说明 |
| --- | --- |
| `id` | `int64` 主键；同时作为相同时间戳下的稳定排序键 |
| `run_id` | 每次运行的稳定分组 ID；系统任务时等于 `task_id`，单渠道手动测试复用外层请求 ID |
| `request_id` | 每个渠道测试尝试的独立关联 ID；写入安全服务端日志，便于从页面定位原始运行日志 |
| `task_id` | 可空字符串；关联定时/手动全量测试的系统任务 |
| `channel_id`、`channel_name`、`channel_type` | 测试时的渠道快照；名称用于渠道删除后仍可读 |
| `source` | `scheduled`、`manual_batch`、`manual_single` |
| `health_check_mode` | 可空；自动测试时记录实际生效模式，手动测试为空 |
| `model_name`、`endpoint_type`、`request_path`、`is_stream` | 本次实际解析后的测试目标；`request_path` 是最终请求路径，避免自动推断端点时 `endpoint_type` 为空 |
| `status` | `succeeded`、`failed`、`cancelled`；被策略跳过不创建结果行 |
| `upstream_http_status` | 上游连接实际返回的 HTTP 状态；无 HTTP 响应时为 `0`。SSE 流内失败时可能仍为 `200` |
| `result_status_code` | 网关最终归一化的错误状态；例如上游 HTTP `200` 建立 SSE 后流内失败，记录为 `502` |
| `failure_kind` | 受控枚举：`none`、`unsupported`、`setup`、`transport`、`upstream_http`、`stream_terminated`、`response_decode`、`response_invalid`、`timeout`、`response_time_exceeded`、`cancelled`、`unknown` |
| `key_index` | 可空；多 Key 渠道仅记录被选中的索引，索引 `0` 与未选择必须可区分 |
| `state_action` | `none`、`channel_disabled`、`channel_enabled`、`key_disabled`、`key_enabled`、`stale_ignored`、`skipped_cancelled`、`action_failed` |
| `duration_ms`、`created_at` | 本次测试耗时和完成时间 |

不设置可自由写入的 `error_message` 或上游 `error_code` 字段。页面根据受控的 `failure_kind`、两个状态码和 `state_action` 生成本地化说明，例如“上游返回 HTTP 504”“上游 HTTP 200 后流内失败，网关归一化为 502”或“响应时间超过自动禁用阈值”。原始错误仅保留在包含 `request_id` 的服务端日志中，避免把上游错误体或敏感信息落库。

索引使用跨 SQLite、MySQL、PostgreSQL 都可用的 GORM 定义：`request_id`、`run_id`、`(created_at, id)`、`(channel_id, created_at, id)`、`(task_id, created_at, id)`、`(status, created_at, id)`。查询统一按 `created_at DESC, id DESC` 排序，避免同一秒产生多条记录时翻页重复或遗漏。所有字段使用普通整数、字符串和布尔值，不依赖特定数据库 JSON、数组或全文索引能力。

## 后端执行流程

1. 将 `testChannel` 的返回值扩展为内部结构化结果，明确返回实际模型、端点、最终请求路径、流式标记、上游 HTTP 状态、网关结果状态和标准化失败类型。当前的 `localErr`、`newAPIError` 继续用于既有判断，不能通过解析日志文本反推结果。
2. 每次测试开始时确定关联标识：定时和手动全量测试使用 `SystemTask.TaskID` 作为 `run_id`，每个渠道尝试再生成独立 `request_id`；手动单渠道测试复用外层中间件生成的请求 ID 作为 `run_id` 和 `request_id`。内部 Gin Context 与服务端日志使用相同 `request_id`。
3. 自动测试的状态变更改用同步的健康检查状态转换服务，并返回实际动作结果。多 Key 复用现有的原子健康检查更新；单 Key 不复用普通 Relay 的异步 `processChannelError` 禁用路径。数据库状态更新完成后才写 `channel_disabled`/`channel_enabled`；并发状态已变化写 `stale_ignored`；更新失败写 `action_failed`。通知仍可在状态更新成功后异步发送。
4. 测试耗时在调用 `testChannel` 返回后立即计算，位于所有成功/失败响应分支之前。手动测试失败不再固定返回 `time=0`，并能与历史记录使用相同耗时。
5. 每个已发起且获得结果的测试只写一行。请求自身因 Context 取消而未获得最终结果时写 `cancelled`；若已得到明确成功/失败结果，但系统任务随后失去租约，保留真实探测结果，并写 `state_action=skipped_cancelled`，不再修改渠道状态。
6. 历史写入失败只记带 `request_id` 的系统错误日志，绝不反向改变测试结果、渠道状态或系统任务状态。手动测试响应仅在历史写入成功时返回 `test_result_id`；缺少该字段时前端不展示“查看本次记录”，但测试本身仍正常返回。

系统任务仍以“任务是否正常执行完”为状态语义：一轮 `7` 个渠道中 `6` 成功、`1` 失败时，任务状态是 `succeeded`，其结果显示 `tested=7 / succeeded=6 / failed=1`，并链接到该 `task_id` 的测试记录。它不能被误读为“所有渠道成功”。

## 代码落点

这不是独立服务或独立数据库；记录写入现有**主数据库**，页面挂在现有“渠道管理”下。实现按现有 Router → Controller → Service → Model 分层，预计落在以下文件：

| 层级 | 文件 | 职责 |
| --- | --- | --- |
| Model | `model/channel_test_result.go` | `channel_test_results` 结构、分页查询、过期记录分批删除 |
| Migration | `model/main.go` | 将新模型加入主库 `AutoMigrate` |
| Service | `service/channel_test_history.go` | 受控失败类型、脱敏写入输入、结果持久化、同步健康状态转换和清理业务规则 |
| Test integration | `controller/channel-test.go` | 扩展 `testResult`，统一耗时与关联 ID，并在现有测试/同步状态转换结束后调用 Service；普通 Relay 的异步错误处理保持不变 |
| Read API | `controller/channel_test_history.go` | 历史列表和单条详情 Handler |
| Route | `router/channel-router.go` | 注册 `/api/channel/test-history` 的两个只读路由和 `ChannelRead` 权限 |
| Retention task | `model/system_task.go`、`controller/system_task_handlers.go` | 新增每日清理任务类型及跨实例租约执行器；清理是否运行不依赖记录开关 |
| Config | `setting/operation_setting/channel_test_history_setting.go` | 注册 `channel_test_history_setting`，通过后端配置更新器校验启停和保留期 |

前端新增独立功能目录 `web/default/src/features/channel-test-history/`，其中由 `api.ts`、`types.ts`、`index.tsx` 和表格/筛选/详情抽屉组件组成；它可复用 `features/channels/api.ts` 的渠道选择数据，但不把历史查询塞进现有渠道列表的状态管理。

路由新增 `web/default/src/routes/_authenticated/channels/test-history.tsx`，地址为 `/channels/test-history`。从渠道页工具栏进入，单渠道行操作可带 `channel_id` 查询参数跳转；系统任务面板 `web/default/src/features/system-info/components/system-tasks-panel.tsx` 增加按 `task_id` 跳转。不会新增侧边栏一级菜单，也不会放到“使用日志”或“会话审计”页面。

启停与保留期配置放在既有的“系统设置 → 运营 → 监控与告警”：`web/default/src/features/system-settings/integrations/monitoring-settings-section.tsx`，并同步更新其 `types.ts`、`operations/index.tsx`、`operations/section-registry.tsx` 及六种语言文件。`routeTree.gen.ts` 是生成文件，不手工编辑。

## 接口与权限

在现有 `/api/channel` 路由下新增只读接口：

| 接口 | 用途 | 权限 |
| --- | --- | --- |
| `GET /api/channel/test-history` | 分页列表 | `ChannelRead` |
| `GET /api/channel/test-history/:id` | 单条安全详情 | `ChannelRead` |

列表支持 `channel_id`、`run_id`、`task_id`、`source`、`status`、`model_name`、起止时间、页码和页大小筛选。页大小上限为 `100`；服务端始终按完成时间倒序返回。接口不返回 Key、错误原文、请求/响应正文、测试用户或计费字段。

列表响应契约为：

```json
{
  "items": [],
  "total": 0,
  "summary": {
    "tested": 0,
    "succeeded": 0,
    "failed": 0,
    "cancelled": 0
  },
  "recording_enabled": false,
  "run": null
}
```

`summary` 遵循时间、渠道、来源、模型、`run_id` 和 `task_id` 条件，但忽略当前 `status` 条件，使“全部/仅失败”切换后仍能看到同一范围的完整成功/失败分布。

当请求携带 `task_id` 时，`run` 返回仅限渠道测试页面使用的安全任务投影：`task_id`、`status`、`processed`、`total` 和完成后的汇总结果。它不返回任务 Payload、执行节点、租约、锁或错误原文。这样拥有 `ChannelRead` 的管理员可以查看自己发起的批次进度，无需访问现有 Root-only 的系统任务接口。

新增全局配置 `channel_test_history_setting`，仅 Root 可修改：

```json
{
  "enabled": false,
  "retention_days": 7
}
```

发布首期默认关闭，以避免所有升级实例在不知情的情况下增加写入量；启用后所有成功和失败测试均会记录。`retention_days` 限制为 `1` 到 `90` 天，并由后端 `ConfigMapUpdater` 校验，前端 Zod 校验只提供即时反馈，不能代替后端边界。

每天由独立、带跨实例租约的系统任务分批清理过期行。清理任务始终注册和调度，不依赖 `enabled` 记录开关；清理按 `created_at` 和固定内部批量执行，避免一次大删除长时间锁表。关闭记录后不再新增行，但已有历史仍按保留期删除。

## 控制台设计

在“渠道管理”增加“测试记录”入口，并在单个渠道的操作/详情中提供已带 `channel_id` 筛选的快捷入口。

### 入口与跳转

结果历史不是新的侧边栏一级模块，而是 `/channels` 的子页面 `/channels/test-history`。这样它与渠道操作保持在一起，也不会被误认为是消费日志。

1. **渠道页主入口**：桌面端在“创建渠道”与更多操作之间放一个次级的“测试记录”按钮（历史图标）；窄屏时将同一入口放进更多菜单，并紧邻“测试全部渠道”。点击后打开最近 24 小时的全部记录。
2. **单渠道入口**：每行的“更多操作”菜单在“测试连接”之后增加“查看测试记录”。跳转携带 `channel_id`，默认查看该渠道最近 7 天；渠道被禁用或操作者没有测试权限时仍可查看，因为读取历史不等于发起测试。
3. **单次测试完成入口**：单渠道测试接口在写入记录成功后返回 `test_result_id`。普通快捷测试的成功/失败提示提供“查看本次记录”动作；测试对话框的结果区域也提供同一按钮。它只打开详情抽屉，不强制把管理员从当前渠道列表带走。
4. **全量测试入口**：管理员点击“测试全部渠道”后，提交成功提示提供“查看本轮记录”，跳转携带新建系统任务的 `task_id`。任务还在执行时，历史页展示已经完成的渠道并定时刷新，不把尚未测试的渠道标为失败。
5. **系统任务入口**：在“系统信息 → 系统任务”的 `channel_test` 行增加“查看本轮记录”链接，同样按 `task_id` 过滤。任务的 `succeeded` 状态旁仍显示汇总 `6 成功 / 1 失败`，避免用户把任务完成理解为全部探测成功。
6. **未启用状态**：历史入口始终保留。若记录功能未启用，页面展示“尚未收集渠道测试记录”的空态；Root 可直接跳转到“系统设置 → 运营 → 监控与告警”启用，其他管理员只看到说明，不会看到不存在的历史数据。

页面的查询参数是可复制、可回退的状态：`channel_id`、`task_id`、`status`、`source`、时间范围和 `result_id`。`result_id` 用于打开对应详情抽屉，关闭抽屉只移除该参数，保留原有筛选。

### 页面布局与信息层级

页面沿用现有后台的页面标题、紧凑筛选和数据表格语言，不另做卡片式监控大盘。

1. **页头**：面包屑“渠道管理 / 渠道测试记录”、标题和一句说明“查看手动与自动测试的结果，不计入使用量”。右侧提供刷新和“返回渠道”；不把“测试全部渠道”放在此页，避免在排障页误触发一批上游请求。
2. **一行运行摘要**：在筛选栏下方以轻量文字显示当前范围内的“已测试、成功、失败、已取消”数量；失败数可点击，等价于打开“仅失败”筛选。摘要遵循除结果状态外的当前筛选条件，不因切换“全部/仅失败”而改变总分布。它不是独立的指标卡，避免把列表页做成重复的性能仪表盘。
3. **筛选栏**：时间快捷项（24 小时、7 天、30 天、全部保留记录、自定义）、渠道搜索选择器、结果分段控件（全部/仅失败）、来源和模型的折叠高级筛选，以及“清除筛选”。从渠道跳转默认带最近 7 天，从任务跳转默认带全部保留记录；其他入口默认最近 24 小时。自定义范围的开始和结束时间写入可复制的查询参数。
4. **结果表**：列为完成时间、结果、渠道、来源、模型/端点、状态码、耗时、多 Key/自动动作、任务。失败行先显示可读原因（例如“上游返回 HTTP 504”）；状态码列优先展示网关结果码，若上游码不同则展示为“上游 200 → 结果 502”。颜色只辅助成功、失败和取消状态，不单独承载含义。
5. **详情抽屉**：点击行从右侧打开，不离开当前筛选结果。抽屉分为“结果”“测试对象”“自动动作”三组，展示受控失败说明、上游 HTTP 状态、网关结果状态、时间/耗时、渠道 ID 与快照名称、模型/端点/最终请求路径/流式标记、来源、任务/运行/请求 ID、Key 索引与实际禁用或恢复动作。它提供复制请求 ID、复制运行 ID 和跳转到关联系统任务，但不显示 Key、Body 或原始上游错误。
6. **运行中状态**：按 `task_id` 查看尚未结束的批次时，页头显示“正在执行，已完成 N / M”，列表定时刷新；完成后自动停止刷新。
7. **空态**：分别处理“未启用采集”“筛选无结果”和“该任务尚未完成任何渠道”三种情况，避免把正常的空列表误导成系统故障。

默认展示最近 `24` 小时。列表采用服务端分页并以 `created_at DESC, id DESC` 保持确定顺序，解决相同完成时间下的顺序漂移；用户刷新或切换筛选时回到第一页。顶部提供“仅失败”快捷筛选和时间、渠道、来源、模型筛选；点击行打开详情抽屉，显示受控失败说明和可复制的请求/运行 ID。

系统任务面板的渠道测试任务补充“查看本轮记录”链接，而不是尝试解析并展示大块 `result` JSON。页面会明确区分：

- **任务成功**：调度器已完成本轮；
- **测试失败**：某一个渠道的上游探测失败；
- **自动动作**：本次结果是否导致渠道/Key 被禁用或恢复。

新增 UI 文案必须使用 `react-i18next`，补齐现有六种前端语言。无权限管理员不显示入口或数据，沿用渠道读取权限模型。

## 验证与发布

后端测试覆盖：

- 上游直接返回 `502`/`504`、无 HTTP 响应的传输错误、响应解析错误、超时和成功路径各恰好生成一条安全记录；失败不调用消费日志写入。
- SSE 已建立且上游 HTTP 为 `200`、随后流内错误被网关归一化为 `502` 时，同时保存 `upstream_http_status=200`、`result_status_code=502` 和 `failure_kind=stream_terminated`。
- 单 Key 与多 Key 的实际禁用/恢复均通过同步状态转换后记录；覆盖 Key 索引 `0`、并发导致的 `stale_ignored`、数据库更新失败的 `action_failed`，并确保普通 Relay 的异步禁用语义不变。
- 请求取消只记录 `cancelled`；结果已产生但任务失去租约时保留成功/失败结果并记录 `skipped_cancelled`，且不再修改渠道状态。
- 手动测试的成功和失败响应都返回真实非负耗时；只有历史持久化成功时才返回 `test_result_id`，历史写入失败不改变原测试响应。
- 定时、手动全量、手动单渠道三种来源能按 `run_id`/`task_id` 查询；渠道删除后名称快照仍可显示；`request_id` 能关联安全日志且任何 API 序列化结果均不包含 Key、Body 或原始错误文本。
- 相同 `created_at` 的多条记录按 `id` 稳定分页，无重复或遗漏；汇总遵循其他筛选但忽略 `status`，分页总数仍遵循全部筛选。
- `ChannelRead` 可读取历史及安全任务投影，但不能获得 Root-only 的任务 Payload、租约、节点或错误原文；无权限角色不能访问。
- 配置后端拒绝 `retention_days` 小于 `1` 或大于 `90`；关闭记录后不新增结果，但清理任务仍能分批删除已有过期记录。
- 在 SQLite 执行迁移、查询和清理测试，并对 MySQL、PostgreSQL 跑相同的迁移/核心仓储测试，确认索引、布尔值、时间比较和删除批次没有方言依赖。

前端验证双状态码展示、筛选与汇总语义、稳定翻页、失败详情、运行中任务刷新与停止、任务跳转、三类空态、权限隐藏、i18n 同步和生产构建。

发布顺序：先发布迁移和只读写入能力（默认关闭），再发布控制台入口；在 `server_tencent` 启用 `7` 天保留后，手动触发一轮测试，确认 #36 这类 `504` 能在测试记录中看到，但使用日志、额度和费用统计不新增假消费。
