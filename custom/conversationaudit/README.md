# 加密会话审计扩展

该目录是自定义功能边界。上游文件仅在 `router/api-router.go` 注册管理接口和审计页面，以及在 `router/relay-router.go` 和 `router/task-plugin-protocol-router.go` 挂载采集中间件。

## 启用

1. 复制 `deploy/.env.example` 为部署目录中的 `conversation-audit.env`，填入 `CONVERSATION_AUDIT_KEY_V1`；它必须是 Base64 编码的 32 字节随机密钥。
2. 以仓库根目录的 Compose 文件为基础叠加 `custom/conversationaudit/deploy/docker-compose.audit.yml`，并通过 `env_file` 注入该密钥文件。生产环境可将此覆盖文件命名为 `docker-compose.override.yml`，以确保后续 `docker compose up` 也会保留审计配置。
3. 使用管理员登录 NewAPI 后，从管理侧栏进入“会话审计”，或直接访问 `/conversation-audit/`。页面和主服务使用同一域名与登录态，无需额外容器或 Nginx 配置。

## 升级边界

上游同步时保留整个 `custom/conversationaudit` 目录，并复核上述三个路由文件中的注册调用。默认与经典前端只各保留一个侧栏菜单入口；不要将功能写入消费日志、控制器或中继实现。

密钥轮换先将新密钥以 `CONVERSATION_AUDIT_KEY_V2` 等形式部署到全部节点，再由 Root 调用 `POST /api/custom/conversation-audit/rotate-key` 切换活跃版本。旧密钥须保留到其对应记录过期。

## 采集语义修复

大上下文请求和流式回复的采集修复方案见 [CAPTURE_REPAIR_PLAN.md](./CAPTURE_REPAIR_PLAN.md)。默认摘要模式要求请求侧只持久化最后一条真人 `user` 文本，响应侧只持久化合并后的助手可见文本；原始请求、系统提示词、历史上下文和 SSE 事件均不得持久化。


## Responses 结果与工具续接

HTTP 200 与生成成功分别判断：`response.failed` 记录为 `failed`，`response.incomplete` 和缺少终态的流记录为 `incomplete`。保留已收到的部分文字以及安全错误码；内容截断通过独立字段标记，不覆盖失败结果。非流式返回体也检查生成状态。

根审计和 Response ID 映射在同一事务中提交。工具续接暂时找不到父关联，或者追加事务失败时，先保存可查看的加密审计，并标记 `pending_response_continuation`；父/当前 Response ID 只存放在加密补偿元数据中。待关联步骤不会提前发布 Response ID 映射，避免后续内容挂到临时记录。每个节点使用单个后台循环，写入后唤醒并每 5 秒补扫，每批最多 32 条、整批数据库操作最多等待 2 秒；多轮乱序续接可能需要多个周期。

补偿按用户隔离，以请求 ID 去重，重新加密为根审计的回复分段后，在同一事务中删除临时记录。失败保留原数据，重启后继续重试；超过保留期的临时记录与元数据一起清理。旧密钥必须保留到相关记录过期。升级新增 `custom_conversation_audit_pending_responses`，必须先让主节点完成迁移，再接入新版本流量。此修复覆盖 HTTP POST `/v1/responses`，不覆盖 WebSocket 出站事件。

## 写入性能与故障边界

审计仍为同步写入，每次写入/追加事务使用独立的 2 秒数据库超时，客户端断开不立即取消审计；追加失败后的暂存事务有独立超时。审计错误只记录诊断，不覆盖接口回复。后台补偿使用有限批次，不创建每请求后台 goroutine。

Root 可查询 `GET /api/custom/conversation-audit/metrics`（个人访问令牌还须具备 `option:read`），查看 `request_parse`、`response_parse`、`encrypt`、`database`、`reconcile` 的计数、失败数、累计耗时，以及待关联数量和最久等待秒数。P95/P99 使用当前进程最近 512 次样本计算，计数从进程启动累计；不同阶段可能包含重叠工作，不应相加作为请求总耗时。`database` 是前台事务耗时（含提交和连接等待），后台事务计入 `reconcile`。数据库待关联数量是共享的，进程耗时指标则需要分别查询各实例。
