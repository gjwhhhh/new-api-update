# 上游 v1.0.0-rc.42 合并计划与执行记录

## 目标与基线

本次集成将 `origin/codex/custom-prod` 的自定义生产逻辑合并到上游标签
`v1.0.0-rc.42`，目标提交为 `6370b29424168039e94d40d610191e7d2e65dbf4`。
自定义基线为 `4a19c68dd230`。合并保留历史，使用独立 worktree 和 `--no-ff` 合并，
最终提交信息使用中文。

上游 `main` 与目标标签的范围已在执行前固定；GitHub/Go 网络刷新使用命令级代理
`http://127.0.0.1:7897`。不静默切换到比 `rc.42` 更新的 Tag 或 `upstream/main`。

## 结构决策

- Classic 前端停止维护并从构建入口移除。
- 接受上游 `web/` 根目录结构，将自定义 `web/default` 功能迁移到 `web/src`。
- 上游的新 Router、`relaykit`、无状态认证、访问令牌 scope 和计费链路作为基线；
  自定义业务在新边界上重新接入。
- 生产镜像仍必须由远端 CI 构建；服务器只执行 `docker compose pull` / `up`。
- 保留 `new-api` 和 `QuantumNous` 受保护身份信息，不修改品牌、归属和元数据。

## 自定义能力保留范围

后端保留并适配：渠道多 Key 状态/恢复、渠道健康检查和测试历史、渠道亲和缓存、
性能指标、分组原子配置与无感知重命名、订阅访问分组、加密会话审计、充值中心配置、
邮箱绑定提醒、用户名排除筛选、Token 显式分组，以及 Responses 流提交前重试、提交后
不重放和完整计费结算。

前端迁移并接入：AI 工作台、渠道状态/测试历史、性能、充值中心、品牌首页和公告、
邮箱提醒、订阅访问组、分组比例可视化/重命名、渠道采样与健康检查设置、所有六种语言。

## 执行阶段

1. **冻结基线**：刷新远端引用，确认自定义分支无新增提交，校验目标 Tag 哈希，保留回滚引用。
2. **生成合并基线**：在独立 worktree 执行 `git merge --no-ff --no-commit main`，先处理目录
   迁移和删除冲突，再按基础设施、认证、Relay、计费、前端领域处理内容冲突。
3. **后端适配**：完成模型/迁移、认证授权、路由 scope、Relay/Responses 生命周期、计费/退款
   和审计接入；所有数据库路径同时兼容 SQLite、MySQL、PostgreSQL。
4. **前端适配**：完成 `web/src` 路由、导航、API 类型、权限、i18n、构建和测试迁移；删除
   `.rej`、旧目录和冲突标记。
5. **验证与签收**：执行 Go 根模块、`relaykit`、`tokenkit`，前端 typecheck/test/build、
   i18n、格式和版权检查；逐项核对能力保留矩阵。
6. **提交与发布**：暂存全部解析结果，创建一个中文合并提交；后续推送由 CI 构建
   `custom-prod-*` 镜像，测试环境只拉取镜像并执行迁移/冒烟。

## 验证门槛

- `go test ./...` 通过。
- `GOWORK=off go test ./...` 在 `relaykit/`、`tokenkit/` 通过。
- `web`: `bun run typecheck`、`bun run test`、`bun run build`、`bun run i18n:sync`、
  `bun run format:check`、`bun run copyright:check` 通过。
- `git diff --check` 通过；无 `<<<<<<<`/`=======`/`>>>>>>>`、`.rej`、`.orig`，无参与构建的
  `web/default` 或 `web/classic` 路径引用。
- 产品验收覆盖：签到额度入钱包及换算、Token 显式分组、分组重命名引用一致、订阅访问权限、
  充值中心外链/内嵌、健康检查/多 Key 恢复、Responses 预扣费/结算/退款、普通用户审计隔离。

## 当前执行结论

Classic 停止维护决策已落地；自定义业务已按新上游边界接入。Go 根模块、`relaykit`、
`tokenkit` 全量测试通过；前端 177 个测试文件、2176 个测试通过，typecheck、i18n、
格式、版权检查和生产构建通过。合并提交 `f09dea68cb1c1b2d14e1c0d046f1e0ac59e261e2`
已创建，工作树洁净。

`bun run lint` 仍会报告上游 rc.42 既有的严格规则债务（主要是数组索引 key、嵌套三元表达式、
循环依赖和安全 lint 规则），本次未进行无关业务重构；其余合并门槛均已通过。
