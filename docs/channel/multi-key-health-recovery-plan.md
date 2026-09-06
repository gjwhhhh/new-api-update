# 多 Key 渠道健康检查与渐进恢复开发计划

状态：已实现并通过相关回归，待发布。

## 结论

渠道 #36 的 Key 即使仍有效，也会出现 `no enabled keys`，根因是多 Key 的“生产流量选 Key”和“健康检查选 Key”共用了只能选择 `enabled` Key 的逻辑。当所有 Key 已被自动禁用时，测试在请求上游之前就失败，因此没有任何一次成功能触发自动恢复。这是一个多 Key 状态机缺口，不是 Key 有效性或上游可用性的问题。

单 Key 渠道不走该分支：它直接将渠道 Key 放入测试上下文，自动禁用的渠道仍能向上游发起检查。因此本问题只影响多 Key 渠道。

上游最新标签 `v1.0.0-rc.33` 的 `GetNextEnabledKey()` 仍在没有 `enabled` Key 时返回 `no enabled keys`，没有本方案的恢复选 Key 逻辑；仅更新到该标签不能解决问题。该行为来自上游提交 `2fa5deb91`（“当没有可用密钥时返回错误而不是第一个密钥”）。该提交正确地阻止了生产流量误用禁用 Key，但当时没有为健康检查提供独立入口；它不是本项目后续自定义改动引入的闭环问题。

## 实施结果

- 正常转发继续只通过 `GetNextEnabledKey()` 使用启用 Key；单渠道手动测试在没有启用 Key 时可以探测一个自动禁用 Key，但不隐式修改状态。
- 后台健康检查使用独立的 `GetNextHealthCheckKey()`，始终优先轮询自动禁用 Key，并使用独立恢复游标，不影响生产轮询顺序。
- 每次后台检查在事务内按“Key 内容、索引、选择时状态”校验结果仍有效，再更新该 Key、渠道状态和 Ability；管理员在请求期间编辑或切换 Key 时，过期结果会被丢弃。
- 真实转发错误也会携带选中的 Key 索引，因此重复内容的 Key 只会禁用实际命中的那一项。
- 现有多 Key 管理入口保留；本次没有新增前端文案，因此不需要修改翻译文件。

## 现状与根因

当前调用链如下：

```text
真实请求失败
  -> 自动禁用命中的 Key
  -> 最后一个 enabled Key 也被禁用，渠道状态变为 auto_disabled
  -> 单渠道测试 / 后台健康检查
  -> GetNextEnabledKey()
  -> no enabled keys
  -> 未向上游发请求，无法得到成功结果
  -> 自动启用条件永远无法满足
```

`GetNextEnabledKey()` 的“只选启用 Key”规则必须保留给正常转发。错误在于健康检查复用了它，而不是该规则本身。

现有“自动启用”开关仍然有效：只有系统设置中的 **Re-enable on success**（`AutomaticEnableChannelEnabled`）开启时，后台探测成功才会自动恢复。关闭该开关时，探测只记录结果，不把 Key 放回生产流量；管理员的显式启用操作不受此开关限制。

## 管理界面现状

默认前端已经有人工恢复入口，路径是：**渠道列表 → 目标渠道行末“…” → Manage Keys（管理密钥）→ Enable All（启用全部）**。也可以在单个 Key 的操作菜单中启用一个 Key。

对应实现位于：

- `web/default/src/features/channels/components/data-table-row-actions.tsx`：只对多 Key 渠道显示 `Manage Keys`；
- `web/default/src/features/channels/components/dialogs/multi-key-manage-dialog.tsx`：显示 `Enable All`；
- `controller/channel.go`：处理 `enable_key` 与 `enable_all_keys`。

如果实际部署页面没有该菜单，先确认运行的是默认前端而非旧镜像/旧前端，并确认该渠道的 `channel_info.is_multi_key` 为 `true`。这个入口只能人工解除当前状态，不能取代自动恢复机制。

## 目标行为

### Key 状态

| Key 状态 | 正常转发 | 单渠道手动测试 | 后台健康检查 | 探测成功后 |
| --- | --- | --- | --- | --- |
| `enabled` | 可用 | 可测 | 没有待恢复 Key 时可测 | 保持启用 |
| `auto_disabled` | 不可用 | 所有 Key 自动禁用时可测，供诊断 | 优先逐个探测 | 仅恢复该 Key |
| `manual_disabled` | 不可用 | 不可测 | 不可测 | 保持手动禁用 |

单渠道手动测试只验证连通性，不隐式修改 Key 状态；成功后仍由管理员点“启用 Key / 启用全部”恢复。这样测试按钮不会意外把一个刚被保护机制隔离的 Key 重新投入生产。

后台健康检查使用逐 Key 恢复流程。比如 A、B、C 三个 Key 都自动禁用：第一次探测 A 成功后只启用 A；下一轮仍优先探测 B 或 C，而不是持续测试 A。失败的 Key 保持自动禁用，其他已启用 Key 不受影响。

### 渠道级手动关闭

渠道级手动关闭和“所有 Key 都手动禁用”目前都借用了 `Channel.Status == manually_disabled`，无法可靠区分。实现时在 `ChannelInfo` 增加一个 JSON 字段（建议名 `manually_disabled`）记录管理员关闭渠道的意图，不需要数据库迁移。

渠道对外状态按以下规则计算：

1. `manually_disabled=true` 时，渠道保持手动关闭，后台不探测，也不因某个 Key 恢复而重新进入流量。
2. 否则，存在任意 `enabled` Key 时渠道为启用。
3. 没有启用 Key、但存在 `auto_disabled` Key 时渠道为自动禁用。
4. 全部 Key 为 `manual_disabled` 时渠道为手动关闭。

管理员打开渠道开关只清除 `manually_disabled`，不改写各 Key 的禁用原因；管理员点“启用 Key / 启用全部”才改变 Key 状态。这消除了“打开渠道开关意外放回全部自动禁用 Key”的歧义。

## 选 Key 与状态转换

选择结果必须包含索引和选择时的原始状态，不能仅在测试完成后用 Key 字符串反查：相同内容的 Key 可以重复，字符串反查会错误地更新第一个 Key。

```go
type ChannelKeySelection struct {
	Key            string
	Index          int
	OriginalStatus int
}
```

正常转发继续用 `GetNextEnabledKey()`，只返回 `enabled` Key。后台使用新的 `GetNextHealthCheckKey()`：

1. 有 `auto_disabled` Key 时，按单独的恢复游标选择其中一个，并推进游标。
2. 没有待恢复 Key 时，再按正常策略选择 `enabled` Key。
3. 永远不选择 `manual_disabled` Key。

恢复游标存入 `ChannelInfo.multi_key_recovery_polling_index`。它不能复用 `MultiKeyPollingIndex`，因为健康检查不应改变生产流量的轮询顺序。健康检查的状态落库与游标推进应在同一次受渠道锁保护的更新中完成；否则失败 Key 会在服务重启或缓存刷新后被反复选中，剩余 Key 又会饥饿。

测试结束后根据**选择时**的状态执行转换：

| 原始状态 | 测试结果 | 处理 |
| --- | --- | --- |
| `enabled` | 成功 | 仅记录测试时间与响应时间 |
| `enabled` | 应自动禁用的失败 | 只禁用该索引的 Key，重新计算渠道状态 |
| `auto_disabled` | 成功且自动启用开关开启 | 只启用该索引，清除该索引原因和时间，重新计算渠道状态 |
| `auto_disabled` | 成功但自动启用开关关闭 | 保持自动禁用，仅记录测试成功 |
| `auto_disabled` | 失败 | 保持自动禁用，更新该索引最后一次失败信息 |
| `manual_disabled` | 任意 | 不会被选择 |

状态更新前还要校验当前索引的 Key 内容和状态仍与选择结果一致。若管理员在测试期间编辑或手动切换了该 Key，丢弃过期测试结果并记录诊断日志，不能把旧请求的结果写到新 Key 上。

## 被审查的未提交修复

当前工作区已经有一版未提交代码，能够让手动测试在“没有 enabled Key”时回退选择一个自动禁用 Key。它解除最小死锁，但不具备发布条件，开发时不要在此基础上直接合并。

| 优先级 | 发现 | 影响 | 处理 |
| --- | --- | --- | --- |
| P0 | `GetNextTestKey()` 只有在不存在启用 Key 时才回退；恢复一个 Key 后会一直测试该启用 Key。 | 其余自动禁用 Key 永远无法逐步恢复。 | 后台选择改为始终优先 `auto_disabled` Key，并使用独立恢复游标。 |
| P0 | `performChannelTests()` 用测试前的渠道总状态决定是否调用 `EnableChannel()`。 | Key A 恢复后渠道变为启用，Key B/C 即使探测成功也不会恢复。 | 用 `ChannelKeySelection.OriginalStatus` 决定逐 Key 转换。 |
| P0 | 被动恢复模式只挑选渠道总状态为 `auto_disabled` 的渠道。 | A 恢复后渠道总状态变为启用，B/C 不再进入被动恢复任务。 | 被动恢复模式改为“存在任一 `auto_disabled` Key”的渠道也要入选。 |
| P1 | 测试选择复用了 `MultiKeyPollingIndex`。 | 健康检查会改变生产轮询顺序。 | 增加独立恢复游标。 |
| P1 | 状态更新只靠 `usingKey string` 找索引。 | 重复 Key 会更新错误的条目。 | 将 `ContextKeyChannelMultiKeyIndex` 传到 `ChannelError` 与状态更新 API，按索引更新。 |
| P1 | `enable_key`、`enable_all_keys`、`disable_all_keys`、删除 Key 和渠道总开关各自直接写状态。 | 全部手动禁用后渠道可能仍显示启用；渠道级手动关闭也可能误清自动禁用记录。 | 统一经过一个状态归并函数，并持久化渠道级手动关闭意图。 |
| P1 | 健康检查通过 `processChannelError()` 异步更新禁用状态。 | 选择、结果与下一轮状态没有同步边界，测试汇总也可能先于状态落库。 | 健康检查调用同步的逐 Key 结果应用服务；真实转发保留原有异步错误处理。 |
| P2 | 当前新增测试只证明“全禁用时能选到一个 Key”。 | 没有覆盖逐步恢复、被动恢复、重复 Key、缓存刷新和管理员并发修改。 | 按下方回归矩阵补齐。 |

当前代码中“正常流量不使用自动禁用 Key”“手动禁用 Key 不参与探测”“编辑 Key 后按内容重整索引状态”的方向可以保留，但需纳入统一状态归并和并发校验。

## 实施清单

1. **模型与状态归并：已完成。** `ChannelInfo` 已增加 `manually_disabled` 和 `multi_key_recovery_polling_index`，渠道开关、Key 管理、真实请求自动禁用和健康检查都通过统一归并规则计算渠道状态。
2. **显式选择结果：已完成。** 健康检查上下文和 `types.ChannelError` 均携带 Key 索引，状态更新不再只依赖 Key 字符串。
3. **独立健康检查选择器：已完成。** 生产、手动测试和后台健康检查使用各自的选 Key 策略。
4. **同步应用检查结果：已完成。** 健康检查结果在事务内校验、更新并同步 Ability；缓存会在渠道可用性变化后重建。
5. **调度语义：已完成。** `passive_recovery` 会持续选择仍存在自动禁用 Key 的渠道，即使该渠道已有其他 Key 恢复。
6. **管理操作收口：已完成。** Key 的启用、禁用、删除、编辑与渠道开关都重算渠道状态；编辑 Key 时会保留未变 Key 的状态并清理移除 Key 的元数据。
7. **界面与文案：无需改动。** 默认前端已有 Key 管理和显式启用入口；本次没有新增用户可见文案。
8. **诊断与发布：待发布。** 日志不会记录 Key 内容；发布时使用 `custom-prod-*` 触发远端构建，服务器仅拉取并重启容器。

## 回归验证

### 单元测试

- 全部自动禁用时，正常转发返回 `no enabled keys`，健康检查仍能选择 Key。
- A/B/C 全部自动禁用：连续三次失败探测分别选到 A、B、C；A 成功后下一次仍从 B/C 选择。
- 有启用 Key 且有自动禁用 Key：生产只选启用 Key，健康检查优先选自动禁用 Key。
- 手动禁用 Key 在单渠道测试、定时检查和被动恢复中都不会被选择。
- 被动恢复模式在 A 恢复、B/C 仍自动禁用时继续选中该渠道。
- 相同内容的两个 Key 按索引独立禁用和恢复。
- 管理员在测试期间替换、删除、启用或手动禁用选择的 Key 时，旧结果不会覆盖管理员新状态。
- 渠道级手动关闭、单 Key 启用、启用全部、禁用全部、删除 Key 后，渠道状态和 Key 状态一致。

已覆盖：生产/手动/健康检查三种选择策略、三个自动禁用 Key 的逐个恢复、被动恢复调度、过期结果拒绝、Ability 状态同步、重复 Key 按索引禁用，以及渠道和 Key 的手动状态归并。

### 集成测试

- 用 `httptest` 上游记录请求，确认全自动禁用时健康检查确实向指定 Key 发出请求，而不是本地返回 `no enabled keys`。
- 验证 A 成功、B 持续失败、C 成功的多轮检查：A/C 逐步回归生产，B 保持隔离。
- 验证内存缓存开启和关闭时，恢复游标、数据库状态、能力缓存和下一轮选择一致。

### 验收与观测

在测试环境用三个 Key 演练“2 个恢复、1 个持续失败”，确认正常请求从不落到持续失败 Key。上线后观察：

- 每轮探测的渠道 ID、Key 索引、原始状态、结果；
- 自动恢复和自动禁用的 Key 数量；
- `no enabled keys` 是否只出现在正常转发路径；
- 被动恢复模式下仍有待恢复 Key 的已启用渠道是否持续被探测。
