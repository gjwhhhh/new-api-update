# Responses 流前置阶段渠道重试

## 背景

`/v1/responses` 的上游可能先建立 SSE 流并发送生命周期事件，随后才通过
`error`、`response.error` 或 `response.failed` 结束请求。此前实现只要已经向客户端
转发过任意事件，就会给渠道错误标记 `SkipRetry`。

生产审计已确认一类可恢复失败的实际事件顺序：

```text
codex.rate_limits
→ codex.response.metadata
→ response.created
→ response.in_progress
→ response.failed (service_unavailable_error / server_error)
```

这类请求没有文本增量、推理结果、输出项或工具调用。前四个事件若已发给客户端，
原实现就不能安全切换渠道；但它们若尚未发给客户端，则可以丢弃并重试，不会暴露
失败尝试的 Response ID 或产生重复输出。

## 目标与边界

目标是在**尚未向客户端提交实际输出**时，复用现有渠道重试机制恢复上游流失败。

- 仅适用于 OpenAI Responses 流式请求。
- 仅由明确的上游终止错误触发；不新增主动超时或抢跑重试。
- 一旦提交实际输出或未知事件，保持原有不重试语义。
- 不记录或输出暂存事件的原始内容；诊断日志仅记录事件类型、数量和字节数。

## 状态机

```text
未提交
  ├─ 收到前置白名单事件 → 暂存
  ├─ 收到上游终止错误 → 丢弃暂存事件，返回可重试渠道错误
  └─ 收到其他事件 / 缓冲超限 → 按顺序 flush 暂存事件，进入已提交

已提交
  ├─ 收到任意后续普通事件 → 直接转发
  └─ 收到上游终止错误 → 向客户端发送终止错误，标记 SkipRetry
```

前置白名单是显式允许列表：

```text
codex.rate_limits
codex.response.metadata
response.created
response.in_progress
```

所有未知事件均视为已提交，确保协议扩展时默认安全。文本、推理、工具调用、输出项、
图像、音频及 `response.completed` 均会提交流，后续失败不能重试。

## 缓冲与重试

- 每次上游尝试最多缓冲 8 个事件或 64 KiB 原始 data；任一上限触发后立即提交。
- 前置失败不向客户端转发失败渠道的生命周期事件或终止事件。
- 处理器返回 `channel:upstream_stream_terminated`，但不设置 `SkipRetry`；外层
  `RetryTimes` 循环据此继续选择渠道。
- 每个可重试的上游流终止失败都会把当前渠道加入本请求的排除集合。若存在其他候选，
  后续尝试不得再次选择该渠道；没有其他候选时按现有错误路径结束。
- 当前 `RetryTimes=3` 时，最多执行首次请求加三次重试。预扣费在外层只执行一次，
  最终失败才退款，成功重试不重复计费。

## 不重试的情形

- 客户端连接已断开，或向客户端写入数据失败。
- 已提交事件后的上游错误。
- 未知事件后的上游错误。
- 现有渠道亲和性、额度或请求校验等其他跳过重试规则。

## 可观测性

前置失败日志应包含：

```text
event=<terminal event>
buffered_events=<count>
buffered_bytes=<count>
from_channel=<id>
```

渠道链路继续使用既有 `use_channel` 记录，例如 `51 -> 36`。会话审计可验证：

- 失败尝试只有前置事件时，客户端侧审计不应出现该失败尝试的 Response ID；
- 成功重试后，审计仅包含最终渠道的完整事件流；
- 已提交后失败时，审计保留终止事件且不会产生第二次渠道尝试。

## 验证清单

1. 白名单事件后 `response.failed`：不写 SSE，错误可重试。
2. 文本增量后 `response.failed`：写入已缓冲事件、文本和终止错误，错误不可重试。
3. 工具调用或未知事件后失败：不可重试。
4. 缓冲超限后失败：不可重试。
5. 同优先级存在多个渠道时，排除已失败渠道后选择另一渠道。
6. 通过会话审计和服务日志复现真实 `service_unavailable_error`，确认客户端只看到成功尝试。

## 发布与回滚

先在 CI 远端构建镜像，再由服务器执行 `docker compose pull` 和 `docker compose up`。
发布后观察前置失败的渠道链路、审计记录和错误率。若出现协议兼容问题，回滚到上一
个已验证镜像；不要在目标服务器执行本地镜像构建。
