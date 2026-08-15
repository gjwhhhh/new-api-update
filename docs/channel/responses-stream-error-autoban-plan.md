# Responses 流上游错误识别与渠道自动禁用开发计划

## 状态与结论

状态：已实现，待合并发布。

基于 `origin/main` 的最新提交 `bde9b2f4` 审查：直连 OpenAI Responses 流处理器 `relay/channel/openai/relay_responses.go` 原先只处理完成、文本增量和工具完成事件。它没有将上游流内的 `error`、`response.error` 或 `response.failed` 事件转为 `NewAPIError`。当上游以 `200` 建立 SSE、在流内发送失败事件后关闭连接时，扫描器将其记为 `EOF`，控制器不会调用 `processChannelError`，所以不会重试或自动禁用渠道。

同包的 Responses 转 Chat 路径已经识别 `response.error` 与 `response.failed`；直连 Responses 路径缺少同等处理。这是本次要补齐的行为，不是 Nginx 超时配置问题。

## 目标与边界

目标：

- 将上游 SSE 内的终止失败事件识别为渠道错误，而不是成功 EOF。
- 在全局自动禁用和渠道 `AutoBan` 都启用时，让失败的渠道（多 Key 时仅失败 Key）进入现有自动禁用流程。
- 只有在尚未向客户端发送任何 SSE 事件时才允许控制器切换备用渠道。
- 已向客户端输出过事件时，禁止自动重试，避免重复回答或重复工具调用。
- 记录可关联的诊断信息，但不写入提示词、完整 SSE 负载、认证头或渠道密钥。

非目标：

- 不在 new-api 中硬编码 `codex upstream stalled` 等供应商文案。
- 不通过提高 Nginx 超时来掩盖上游已经返回的 SSE 失败事件。
- 不尝试在已输出部分内容的 Responses 流上实现通用续传；该能力需要上游协议明确支持。

## 事件契约与错误归一化

### DTO

扩展 `dto.ResponsesStreamResponse`，保留顶层错误对象：

```go
Error *types.OpenAIError `json:"error,omitempty"`
```

已实现的错误解析顺序：

1. 顶层 `error`；
2. `response.error` / `response.failed` 中的 `response.GetOpenAIError()`；
3. 没有结构化错误时，使用受控的通用错误消息，并保留事件类型用于日志。

新增同包的稳定领域函数，用于直连与转换路径共享，而不是复制分支判断：`responsesStreamEventError(event)`。它只负责识别与归一化 Responses 协议错误，不负责重试、扣费或渠道状态修改。

### 错误分类

新增错误码：

```text
channel:upstream_stream_terminated
```

使用 `types.NewOpenAIError` 生成 `502 Bad Gateway` 的渠道错误，并保留 `channel:` 前缀。现有 `service.ShouldDisableChannel` 与 `shouldRetry` 可据此前缀识别为渠道故障，不依赖管理员额外配置 500 状态码或错误关键词。

客户端返回的错误使用固定的渠道错误；若已经开始 SSE，保留原始事件的协议形状（顶层 `error` 或 `response.failed` / `response.error`），但不透传上游消息。后端日志记录事件类型、错误码和请求 ID。不要记录原始错误 payload、提示词、响应正文或凭据。

## 流状态与重试策略

在 `OaiResponsesStreamHandler` 中维护两个状态：

- `streamErr`：识别到的终止渠道错误；
- `hasForwardedEvent`：是否已经成功向客户端发送过任意非错误 SSE 事件。

处理顺序必须是：解析事件 → 识别错误事件 → 决定是否停止 → 仅对非错误事件向客户端写入。错误事件不能先写给客户端再作为成功 EOF 收尾。

| 场景 | 渠道状态 | 重试 | 客户端结果 |
| --- | --- | --- | --- |
| 首个 SSE 事件前收到上游错误 | 进入自动禁用流程 | 允许切换备用渠道 | 若备用成功，客户端继续获得单一正常流；否则返回规范错误 |
| 已发送任意 SSE 事件后收到上游错误 | 进入自动禁用流程 | 禁止 | 以规范终止错误结束当前流，不重复执行请求 |
| 正常 `response.completed` / `[DONE]` / EOF | 不变 | 不适用 | 保持现有行为 |
| 无法解析的 SSE JSON | 记录为坏响应 | 按既有坏响应策略 | 保持既有客户端兼容性 |

现有 `shouldRetry` 先判断 `types.IsChannelError`，后判断 `types.IsSkipRetryError`。为了落实“部分流禁止重试”，需要将跳过重试的判断置于渠道错误判断之前，或新增一个只影响本场景的强制跳过选项。优先采用前者，但必须补回归测试，确认已有同时带 `channel:` 与 `SkipRetry` 标记的错误本就应禁止重试。

## 实施范围

1. `dto/openai_response.go`
   - 增加顶层 `Error` 字段。

2. `types/error.go`
   - 增加 `ErrorCodeChannelUpstreamStreamTerminated`。

3. `relay/channel/openai/`
   - 将 Responses 流错误归一化为可共享的协议逻辑。
   - 改造 `OaiResponsesStreamHandler`：错误事件停止扫描并将 `streamErr` 返回给控制器。
   - 用该共享逻辑收敛 `chat_via_responses.go` 现有的重复判断，保持其已有行为。

4. `controller/relay.go`
   - 调整 `shouldRetry` 的跳过重试优先级，或引入范围更窄的强制跳过选项。
   - 不在控制器中解析供应商错误文案；控制器只接收已归一化的 `NewAPIError`。

5. 日志与审计
   - 在现有渠道错误日志中记录渠道 ID、请求 ID、事件类型、错误码和是否已经输出事件。
   - 流内错误不再被记录为 `stream_status.status=ok`。
   - 不记录原始错误 payload、提示词、响应正文或凭据。

6. 管理配置核验
   - 确认生产环境的 `AutomaticDisableChannelEnabled` 为开启。
   - 确认目标渠道的 `AutoBan` 为开启。
   - 该代码修复使 `channel:` 错误直接满足自动禁用条件，不要求修改自动禁用状态码或关键词。

## 测试计划

新增确定性表驱动测试，使用 `testify/require` 与 `testify/assert`：

1. 顶层 `error`：直连 Responses 流返回渠道错误，不作为成功 EOF。
2. `response.error`：优先使用结构化错误字段。
3. `response.failed`：优先使用 `response.GetOpenAIError()`。
4. 没有结构化错误的失败事件：返回受控通用错误，不泄露原始 payload。
5. 正常 `response.completed`、文本 delta 和 `[DONE]`：计费与客户端输出保持不变。
6. 失败发生在任何事件输出前：返回可重试的渠道错误。
7. 失败发生在已输出事件后：返回带跳过重试标记的渠道错误。
8. `shouldRetry`：跳过重试标记优先于渠道错误标记；既有渠道模型映射等用例不发生意外重试。
9. 渠道处理链路：当全局自动禁用和 `AutoBan` 开启时，新的错误码调用现有禁用流程；多 Key 渠道断言只更新命中的 Key。

已覆盖：顶层/嵌套错误、无事件前重试、已输出后停止重试、自动禁用开关、正常完成流和控制器重试优先级。

验证命令按修改范围执行：

```bash
go test ./relay/channel/openai ./controller ./types
```

完成针对性测试后，再运行受影响后端包的全量测试。日志输出写入文件并只提取关键失败项，避免在终端输出完整日志。

## 发布与回滚

1. 先在测试环境以模拟 SSE `response.failed` 验证：渠道错误日志、自动禁用、无首事件重试和部分流不重试。
2. 生产发布后监控新的错误码及 channel #30 的状态变化；确认不会再出现同类请求以 `HTTP 200 + EOF + status=ok` 结算。
3. 回滚仅回退本次代码提交；不需要变更 Nginx 配置或数据库迁移。

## 设计 Review

### 通过项

- 根因位于协议错误语义丢失，方案在协议边界修复，不把供应商文案写入渠道选择或 Nginx。
- 复用现有自动禁用、单 Key/多 Key 状态更新和重试调度，避免新增并行禁用机制。
- 明确禁止部分流重试，降低工具重复执行与客户端协议错乱风险。
- 无数据库结构变更、无跨数据库迁移风险、无计费规则改动。

### 开发前必须确认的风险

- 获取一份已脱敏的实际 SSE 错误事件结构，验证上游使用的是顶层 `error` 还是 `response.failed`；实现必须兼容两者。
- 调整 `shouldRetry` 的优先级会影响所有同时具备 `channel:` 与 `SkipRetry` 的错误；先补回归测试再改逻辑。
- 对已经发送 SSE 事件的请求，HTTP 状态码可能已提交；该场景以流内规范终止错误和后端渠道禁用为准，不能承诺改写为新的 HTTP 502 响应。
- 单次上游停滞会导致当前 Key 或渠道自动禁用。若业务希望按连续失败次数禁用，需要另行设计失败计数与恢复策略，不应在本次修复中隐式加入。
