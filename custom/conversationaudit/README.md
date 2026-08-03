# 加密会话审计扩展

该目录是自定义功能边界。上游文件仅在 `router/api-router.go` 注册管理接口和审计页面，以及在 `router/relay-router.go` 挂载采集中间件。

## 启用

1. 复制 `deploy/.env.example` 为部署目录中的 `conversation-audit.env`，填入 `CONVERSATION_AUDIT_KEY_V1`；它必须是 Base64 编码的 32 字节随机密钥。
2. 以仓库根目录的 Compose 文件为基础叠加 `custom/conversationaudit/deploy/docker-compose.audit.yml`，并通过 `env_file` 注入该密钥文件。生产环境可将此覆盖文件命名为 `docker-compose.override.yml`，以确保后续 `docker compose up` 也会保留审计配置。
3. 使用管理员登录 NewAPI 后，从管理侧栏进入“会话审计”，或直接访问 `/conversation-audit/`。页面和主服务使用同一域名与登录态，无需额外容器或 Nginx 配置。

## 升级边界

上游同步时保留整个 `custom/conversationaudit` 目录，并复核两个路由文件中的注册调用。默认与经典前端只各保留一个侧栏菜单入口；不要将功能写入消费日志、控制器或中继实现。

密钥轮换先将新密钥以 `CONVERSATION_AUDIT_KEY_V2` 等形式部署到全部节点，再由 Root 调用 `POST /api/custom/conversation-audit/rotate-key` 切换活跃版本。旧密钥须保留到其对应记录过期。

## 采集语义修复

大上下文请求和流式回复的采集修复方案见 [CAPTURE_REPAIR_PLAN.md](./CAPTURE_REPAIR_PLAN.md)。方案要求请求侧只持久化最后一条真人 `user` 文本，响应侧只持久化合并后的助手可见文本；原始请求、系统提示词、历史上下文和 SSE 事件均不得持久化。
