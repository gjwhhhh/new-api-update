# 会话审计语义采集修复方案

## 1. 目标与完成标准

本次修复只改变审计扩展的采集和展示语义，不改变转发给上游模型的请求，不关闭或修改任何模型缓存。

- 请求侧只加密保存本次新增的最后一条真人 `user` 文本。
- 不保存 system、developer、assistant 历史、缓存上下文、工具定义、工具结果、图片、音频、文件或 Base64。
- 响应侧只加密保存模型返回的可见 assistant 文本；流式增量合并为连续正文，不保存逐字 SSE 事件。
- 审计扩展不额外持久化原始 JSON；它只复用 NewAPI 现有的请求级 `BodyStorage`，不将原始内容写入数据库、应用日志或 Docker 日志。
- OpenAI Chat、OpenAI Responses、Claude Messages 和 Gemini GenerateContent 使用统一的审计输出结构。
- 采集失败不影响模型请求；有本次新增 user 文本，或无法关联但提取到可见 assistant 回复的请求才进入审计表；语义请求与回复都为空的 `0 / 0` 请求不入表。

## 2. 现网证据与根因

2026-08-02 对 `api.tokenflyapi.com` 对应实例的审计元数据进行只读检查，结果如下：

| 模型与接口 | 记录数 | 请求截断数 | 回复截断数 | 结论 |
| --- | ---: | ---: | ---: | --- |
| `gpt-5.6-sol /v1/responses` | 733 | 628 | 2 | 大上下文在语义提取前被截断，无法解析请求 JSON |
| `gpt-5.5 /v1/chat/completions` | 25 | 0 | 6 | 流式 SSE 事件整体达到保存上限，回复被截断 |

这些数字是检查时的快照，只用于确认问题类型，不作为长期监控基线。

当前实现有两个独立问题：

1. 请求采集先按 `MaxContentBytes`（当前 256 KiB）截断原始 JSON，再调用 `extractRequest`。超过限制后 JSON 不完整，最终只能保存 `request payload exceeded audit limit before semantic extraction` 标记。OpenAI Responses 与 Claude 大上下文请求都会受影响。
2. 流式回复把每个 SSE JSON 事件加入 `stream_events`。一个字或一个 token 对应一个事件，所以管理页面看到的是大量零散事件；事件协议字段还会消耗空间，使实际正文尚未达到 256 KiB 时审计数据已经截断。

因此，仅提高 256 KiB 限制不能解决重复保存上下文、逐字展示和空间浪费问题。

## 3. 请求采集规则

### 3.1 两级限制

将“临时解析上限”和“最终存储上限”彻底分离：

- `CONVERSATION_AUDIT_MAX_PARSE_BYTES`：原始请求临时解析上限，默认 4 MiB，建议允许在 1–16 MiB 内配置。该值只由启动环境变量提供，不进入管理设置表。
- `CONVERSATION_AUDIT_MAX_BYTES`：提取后单侧明文存储上限，继续默认 256 KiB。

处理顺序：

1. 从现有 `BodyStorage` 读取请求，读取后恢复指针，确保转发和重试不受影响。`BodyStorage` 可能按 NewAPI 自身配置使用请求级临时文件，该文件由既有清理中间件在请求结束后删除；审计扩展不创建额外副本。
2. 在临时解析上限内解析完整 JSON。
3. 按协议提取最后一条真人 `user` 文本。
4. 立即丢弃原始解析对象，不持久化原始 JSON。
5. 对提取结果做 UTF-8 安全截断。
6. 使用 AES-256-GCM 加密标准化结果并写入独立审计表。

原始请求超过临时解析上限时，不保存任何原始片段或截断 JSON。若仍能从响应侧提取可见 assistant 文本，则保存为 `0 / N` 的独立回复记录并带固定错误码；若两侧语义内容都为空，则不创建记录。

### 3.2 “最后一条 user 消息”的统一定义

只检查当前请求末尾代表本次新增输入的消息或 input item，不向前回溯历史 user。末尾项目包含真人 `role=user` 文本时，只拼接该消息中的文本块。末尾项目只有 `tool_result`、`function_call_output` 或其他工具结果时，不新建带请求正文的根记录；仅 OpenAI Responses 在携带可验证的 `previous_response_id` 时可作为同一用户回合的工具续接，将可见回复片段追加到原记录。若没有可信父关联、但响应侧有可见 assistant 文本，则保存为独立的 `0 / N` 回复记录；绝不根据用户名、时间或文本猜测其父对话。这样既避免重复保存历史 user，也不会丢失有内容的模型输出。

| 协议 | 查找位置 | 保留内容 | 明确忽略 |
| --- | --- | --- | --- |
| OpenAI Chat | `messages` 最后一个消息 | string content；`text` 类型块 | system/developer、历史消息、image/audio/file、tool message |
| OpenAI Responses | `input` 的末尾项目 | user message 的 `input_text`/`text`；纯字符串 `input` | `instructions`、`previous_response_id`、历史 output、`function_call_output` |
| Claude Messages | `messages` 最后一个消息 | user content 中的 `type=text` | `system`、历史消息、`tool_result`、image/document、`cache_control` |
| Gemini | `contents` 最后一个 content | `role=user` 的 text parts | `system_instruction`、历史 content、inline/file data、function response |

同一条 user 消息包含多个文本块时，用换行合并为一次输入。空白文本不算有效输入。

`/v1/completions` 的 `prompt` 没有角色边界，无法可靠区分系统提示、历史上下文和本次输入。为满足“不保存系统提示词”的要求，默认不保存该接口正文，只记录 `capture_error=unsupported_legacy_prompt`。替代做法是把完整 prompt 当作用户输入，但会破坏隐私目标，不采用。

`/v1/responses/compact` 返回上下文压缩结果而非用户可见的 assistant 回复，正文采集范围明确排除该接口。审计正文只覆盖 `/v1/chat/completions`、`/v1/responses`、`/v1/messages`、Gemini `generateContent`/`streamGenerateContent` 和 Playground Chat。

### 3.3 新的请求密文格式

不新增数据库正文列，复用现有 `request_nonce` 和 `request_ciphertext`：

```json
{
  "schema_version": 2,
  "capture_mode": "latest_user_text",
  "user_input": "本次新增的用户输入"
}
```

旧记录保持原密文不变。详情 API 和管理页面同时兼容旧结构与版本 2 结构，不做历史回填，也不尝试从旧密文推断本次输入。

## 4. 响应采集规则

### 4.1 流式响应

将当前“缓存原始响应后保存 `stream_events`”改为增量文本采集器。所有受支持请求均挂载 `captureWriter`：已提取到本次新增 user 文本时创建新的审计回合；OpenAI Responses 的可验证工具续接将可见回复片段追加到原记录；没有新 user 或没有可信父关联时，只要提取到可见 assistant 文本，就写入独立的 `0 / N` 回复记录。它仍原样把字节写给客户端，同时将跨 `Write` 边界的 SSE frame 放入小型待解析缓冲区，按协议提取文本增量并追加到 assistant 文本缓冲区。语义请求与回复都为空的请求正常转发但不写入审计表。

| 协议 | 需要合并的文本增量 |
| --- | --- |
| OpenAI Chat | `choice.index == 0` 的 `delta.content` |
| OpenAI Responses | 按 `output_index`、`content_index` 合并 `response.output_text.delta` 事件的 `delta` |
| Claude | 按 content block index 合并 `content_block_delta` 中的 `text_delta.text` |
| Gemini | 第一个 candidate 的 `content.parts[].text` |

不保存 SSE 的 event id、usage、ping、reasoning、tool call、结束标记和协议包装字段。单个未完成 SSE frame 的审计解析上限固定为 1 MiB，超限时丢弃该 frame、设置固定安全错误码，但不记录原始 frame。合并正文到达最终存储上限后停止向审计缓冲区追加。以上两种情况都必须继续把完整响应转发给客户端。

### 4.2 非流式响应

解析结构化响应并只提取最终可见 assistant 文本：

- OpenAI Chat：`choice.index == 0` 的 `message.content`
- OpenAI Responses：按 output/content 顺序合并 message 的 `output_text`
- Claude：`content[].text`
- Gemini：第一个 candidate 的 `content.parts[].text`

错误响应不保存上游错误 message，只保留现有白名单安全错误码。

### 4.3 新的响应密文格式

```json
{
  "schema_version": 2,
  "capture_mode": "assistant_text",
  "assistant_text": "合并后的完整模型回复"
}
```

`response_truncated` 只表示合并后的 assistant 正文超过最终存储上限，不再表示 SSE 包装数据超过限制。

## 5. 状态、错误与长度语义

- `request_length`：版本 2 请求标准化明文的字节数，不代表原始请求体大小。
- `response_length`：版本 2 回复标准化明文的字节数，不代表原始 SSE 大小。
- `request_truncated`：最后一条用户文本在提取后超过存储上限。
- `response_truncated`：合并后的 assistant 文本超过存储上限。
- `capture_error`：使用固定错误码，不写入请求、回复或解析异常原文。
- `no_new_user_text`：表示工具续跑或其他请求没有本次新增的人类文本。OpenAI Responses 可用 `previous_response_id` 精确关联到同用户原回合时，追加可见回复片段；无法关联但有可见回复时，创建独立 `0 / N` 记录并加入 `unlinked_response_continuation` 固定错误码；没有可见回复才跳过。不得回溯并重复保存历史 user。
- HTTP 失败、客户端断开或上游失败时，只要已识别出用户输入，或提取到可见回复，就保存现有内容、HTTP 状态和安全错误码；缺失的一侧密文保持为空。

持久化入口必须拒绝请求与回复都为空，作为中间件筛选之外的第二道防线；因此任何调用路径都不能创建 `0 / 0` 记录。

### 5.1 无正文采集诊断日志

默认不输出每次采集决策。排查漏采集时，临时设置启动环境变量 `CONVERSATION_AUDIT_DIAGNOSTICS=true`，重启实例后可在应用/Docker 日志中看到固定格式的诊断行。该开关不写入数据库设置表；关闭或移除环境变量并重启实例后停止输出。

每行只包含：事件、固定原因码、关联结果、Request ID、受支持接口路径、协议、HTTP 状态、原始请求字节数，以及已经抽取出的标准化请求/回复字节数。它明确不包含请求/回复正文、系统提示词、工具结果、Token、认证头、`previous_response_id` 或上游响应 ID。

| 事件 | 关键原因/关联结果 | 说明 |
| --- | --- | --- |
| `skipped` | `request_parse_limit_exceeded` | 原始请求超过临时解析上限，未保存任何原始片段。 |
| `skipped` | `request_parse_failed`、`request_body_unavailable`、`no_new_user_text` | 区分协议解析失败、请求体不可读和本次无新增用户文本。 |
| `captured_root` | `created`、`provider_response_id_absent` | 已写入新的用户回合；后者表示不能为后续工具续接建立关联。 |
| `captured_orphan_response` | `unlinked_response_continuation` | 未知父回合仍返回了可见文本，已保存为 `0 / N` 并尝试建立后续关联。 |
| `continuation` | `appended`、`parent_link_not_found`、`link_lookup_failed` | 区分正常归并、父响应来自旧记录/未知记录和数据库查询失败。 |
| `root_persist_failed` | `root_persist_failed` | 加密或审计库写入未形成根记录；详细数据库错误仍只出现在既有错误日志。 |

诊断开关适合短期定位问题；高流量生产环境确认原因后应关闭，避免产生大量元数据日志。

## 6. 实施范围

所有业务改动继续限制在 `custom/conversationaudit`：

1. `capture.go`
   - 分离原始解析限制与最终存储限制。
   - 新增四种协议的最后 user 文本提取。
   - 将响应 Writer 改为协议感知的流式文本合并器。
   - 统一生成版本 2 请求/响应结构。
2. `core.go`
   - 增加只由环境变量提供的临时解析上限及校验，数据库设置结构保持不变。
   - 调整长度、截断和空请求正文拒绝持久化语义。
3. `capture_test.go`
   - 增加跨协议、大上下文、工具循环、UTF-8 截断、SSE 分片合并回归测试。
4. `console/index.html`
   - 版本 2 记录直接展示“用户输入”和“模型回复”连续文本。
   - 旧记录继续使用 JSON 格式化展示。
5. 部署样例
   - 增加 `CONVERSATION_AUDIT_MAX_PARSE_BYTES`，不改变密钥与数据库结构。
6. 采集路由
   - 从正文采集范围移除 `/v1/responses/compact`，其余上游路由注册点不变。

不修改上游 DTO、relay adaptor、消费日志或默认日志 API；请求发送内容、缓存命中和模型计费行为保持原样。

## 7. 测试与验收

### 7.1 请求回归

- OpenAI Chat 请求包含 system、历史 user/assistant 和最后 user 时，只保存最后 user。
- OpenAI Responses 原始 JSON 大于 256 KiB、小于 4 MiB 时，仍正确保存最后 user。
- Claude 最后一个 user 只有 `tool_result` 时，不回溯历史 user；若本次有可见回复，保存为 `0 / N`。
- OpenAI Responses 末尾只有 `function_call_output` 时，不重复保存之前的用户输入；能关联时合并回复，不能关联但有可见回复时保存为 `0 / N`。
- Gemini 混合 text、inline data 和 function response 时，只保存最后 user 的 text。
- 图片、文件、音频、Base64、工具定义、工具结果和缓存控制字段均不出现在解密结果。
- 中文文本超过 256 KiB 时在合法 UTF-8 边界截断并设置 `request_truncated=true`。
- 原始请求超过解析上限时只保存固定错误码，不保存原始片段。
- 读取审计内容后 BodyStorage 指针恢复，模型分发和重试收到的字节与原请求完全一致。

### 7.2 响应回归

- OpenAI Chat 每个 SSE 事件只返回一个字符时，最终审计详情仍是一段连续文本。
- OpenAI Responses、Claude、Gemini 的 SSE 数据被拆到任意多个 `Write` 调用时仍能正确拼接。
- OpenAI Chat 多 choice 和 Gemini 多 candidate 响应只保存首选结果，不发生候选内容交叉。
- 单个 SSE frame 超过 1 MiB 时停止审计解析该 frame，但客户端仍收到完整响应。
- 非流式四种协议只保存可见 assistant 文本。
- reasoning、usage、tool call、ping 和错误 message 不进入密文。
- 合并正文超过 256 KiB 时安全截断，但客户端仍收到完整响应。

### 7.3 兼容与安全

- SQLite、MySQL、PostgreSQL 继续不修改任何上游表；版本 2 正文格式无需回填，回复合并仅新增扩展表与索引迁移。
- 管理员可查看新旧记录；普通用户、Token 和未登录用户仍无法访问详情。
- 现有 `/api/log` 和应用日志不出现对话正文。
- 加密往返、错误密钥和密文篡改测试继续通过。

### 7.4 上线验收指标

灰度发布后至少观察 24 小时：

- `/v1/responses` 因 `request payload exceeded audit limit before semantic extraction` 产生的新记录降为 0。
- 正常流式回复不再因 SSE 包装体达到 256 KiB 而标记 `response_truncated`。
- 随机抽查 OpenAI Chat、Responses、Claude 和 Gemini，各记录只包含最后 user 文本与连续 assistant 回复。
- 工具续跑请求不产生重复的历史 user 密文；`0 / 0` 不入表，而无法可靠关联但有可见回复的请求保留为 `0 / N`。
- 模型请求成功率、首 token 时间和转发响应字节与发布前无显著回归。

## 8. 跨请求回复合并（OpenAI Responses）

### 8.1 目标与边界

一个用户回合可能经过多次 `/v1/responses` 调用：首次调用包含用户输入，模型先返回工具调用；客户端提交 `function_call_output` 后，模型再返回最终可见文本。管理页面应显示为一条用户输入及一段合并后的模型回复，而不是多条无上下文记录。

合并单位是“用户回合”，不是整段会话：每次检测到新的末尾 `role=user` 文本，即使请求带有 `previous_response_id`，也必须创建新的审计记录。`previous_response_id` 也可用于普通多轮追问，若按它合并会把第二个用户问题及其回答错误地并入第一个问题。

第一阶段只自动支持 OpenAI Responses：该协议在请求侧提供 `previous_response_id`，响应侧提供 `id`，可构成精确的父子链。OpenAI 的 `conversation` 不能用于合并多个用户回合；本阶段也不额外持久化它。OpenAI Chat、Claude Messages 和 Gemini GenerateContent 的当前兼容路径没有供本扩展使用的等价跨请求回合键，默认不做启发式合并。

### 8.2 关联算法

1. 新的末尾 user 文本：创建根审计记录，保存其可见回复，并从 Responses 非流式 JSON 或 SSE `response.*` 事件提取响应 `id`。
2. 将响应 `id` 做带域分隔的 HMAC-SHA-256，不保存原始上游 ID；连同协议、用户和密钥版本写入独立关联表，指向审计记录。
3. 没有新 user 文本且请求包含 `previous_response_id`：用同一 HMAC 查找同协议、同用户的父关联。匹配成功才是工具续接。
4. 续接响应仅保存新的可见 assistant 文本为加密回复片段，并建立本次响应 `id` 到同一根记录的关联，支持多轮工具链。
5. 没有父关联、用户不一致或协议不符时，若本次有可见回复则创建独立 `0 / N` 记录，并将本次响应 `id` 关联到该记录，供后续工具续接使用；没有可见回复才跳过。绝不按用户名、Token、模型、时间窗口或文本相似度猜测关联。

每个回复片段沿用 AES-256-GCM、独立随机 nonce 和记录的密钥版本。根记录与片段的总回复大小共同受 `CONVERSATION_AUDIT_MAX_BYTES` 限制；达到上限后只标记截断，不能让工具循环绕过容量限制。回复详情按时间和片段 ID 合并展示，并标注合并片段数。

### 8.3 数据模型与兼容性

新增两张仅属于扩展的表：

- `custom_conversation_audit_response_links`：HMAC 后的上游响应 ID 到审计记录的映射，用于精确续接；不保存原始 ID、工具参数或正文。
- `custom_conversation_audit_response_segments`：续接阶段的加密可见回复片段；包含根审计 ID、请求 ID、密文、nonce、长度、截断、时间和密钥版本。

现有 `custom_conversation_audits` 不修改正文语义；其 `response_length` 汇总根回复与续接片段的长度，`response_truncated` 表示任一片段或合并总量被截断。删除根记录和保留期清理时同步删除关联与片段。历史记录没有上游关联键，不能可靠回填，也不尝试从内容、时间或缓存数据推断关系。

### 8.4 协议覆盖与替代方案

| 方案 | 覆盖范围 | 优点 | 风险/代价 | 结论 |
| --- | --- | --- | --- | --- |
| Responses 响应 ID 链 | `/v1/responses` 工具续接 | 无客户端改动，父子关系精确 | 需要新增两个扩展表和流式 ID 解析 | 第一阶段采用 |
| 客户端 `X-Audit-Turn-ID` | Chat、Claude、Gemini 等所有已配合客户端 | 协议无关、精确 | 调用方需要维护每个用户回合 ID | 后续可选扩展 |
| 按用户/模型/时间/文本猜测 | 表面上覆盖全部 | 无客户端改动 | 会串不同对话，审计结论不可信 | 明确不采用 |

Gemini GenerateContent 的多轮模式由客户端在每次调用中发送完整 `contents` 历史，响应 `responseId` 只关联该次流式响应，不能用于跨请求合并。Claude 和 Chat Completions 在本扩展当前支持的请求结构中同样不具备可安全复用的通用回合键。

### 8.5 验收

- 用户输入 -> 工具调用 -> `previous_response_id` + 工具结果 -> 最终回复：仅一条根审计记录，详情包含合并后回复。
- 两次均有新的 user 输入、第二次携带 `previous_response_id`：必须是两条根审计记录，不能合并。
- 多次连续工具续接：所有可见回复按发生顺序归入同一根记录。
- 父 ID 缺失、未知、已过期或属于其他用户：不追加到任何现有记录；有可见回复时创建独立 `0 / N` 记录，无可见回复时不创建 `0 / 0` 记录。
- 关联表中只出现 HMAC，不出现原始响应 ID、工具结果、缓存、系统提示词或原始 JSON。
- 密钥轮换、根记录删除、保留期清理、跨数据库迁移和 256 KiB 总量限制均覆盖回归测试。

### 8.6 协议参考

- [OpenAI Responses conversation state](https://developers.openai.com/api/docs/guides/conversation-state)：`previous_response_id` 可建立响应链，但后续请求仍可能包含新的 user 输入。
- [Gemini GenerateContent multi-turn](https://ai.google.dev/gemini-api/docs/generate-content/text-generation)：常规多轮由客户端每次提交完整 `contents` 历史，不能以单次 `responseId` 推断跨请求回合。

## 9. 发布与回滚

1. 在本地运行 `go test ./custom/conversationaudit ./router`，再构建主服务。
2. 使用独立测试密钥验证流式与非流式详情页面。
3. 构建不可变镜像并先部署单实例灰度。
4. 观察采集错误、截断率、请求延迟和数据库增长后再全量替换。
5. 回滚仅切回上一镜像；版本 2 使用原有密文字段，旧版本仍可解密并展示为普通 JSON，不需要回滚数据库。

## 10. 替代方案对比

| 方案 | 优点 | 缺点 | 结论 |
| --- | --- | --- | --- |
| 将当前限制提高到 1 MiB | 改动最小 | 仍保存完整上下文和 SSE；以后仍会超限；增加密文和表空间 | 不采用 |
| 转发完成后解析完整请求和响应 | 实现较简单 | 流式包装数据仍占用大量内存，客户端断开时可能拿不到完整帧 | 不推荐 |
| 请求语义提取 + 响应增量合并 | 只保存业务需要的正文；空间稳定；兼容流式 | 需要维护四种协议的提取器和回归测试 | 采用 |
