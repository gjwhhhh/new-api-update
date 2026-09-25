# 首页品牌一致性开发计划

## 1. 文档目的

本文档记录将当前项目首页与 `codex/stellar-home-redesign` 参考分支对齐，并让其他公开页面共享一致 Header/Footer 品牌体验的开发计划。

当前状态：开发已完成，已通过类型检查和生产构建；全量 lint/format 检查仍包含仓库原有问题，首页相关文件已通过定向检查。

## 2. 分支与参考范围

- 当前开发分支：`codex/custom-prod`
- 参考工作树分支：`codex/stellar-home-redesign`
- 当前项目目录：`/Users/apple/Documents/workplace/myproj/new-api-update`
- 参考工作树目录：`/Users/apple/.codex/worktrees/49e6/new-api-update`

参考分支中与本计划直接相关的内容包括：

- `web/default/src/features/home/index.tsx`
- `web/default/src/features/home/components/brand/`
- `web/default/src/assets/tokenfly-*`
- `web/default/src/styles/brand-home.css`
- `web/default/src/styles/signal-home.css`
- `web/default/src/styles/signal-surfaces.css`
- 首页公告弹窗逻辑
- `PublicHeader`、`PublicLayout`、`Footer` 的品牌扩展能力
- 首页新增多语言文案

参考分支中与首页无关的 dashboard、表格、频道、系统设置和其他业务页面改动不在本次移植范围内。

## 3. 目标

### 3.1 首页目标

在不破坏现有自定义首页能力的前提下，将默认首页替换为参考分支的品牌首页，包括：

- 品牌化 Hero 区域；
- 模型生态展示；
- 模型与价格入口；
- Getting Started 流程；
- 统一 CTA 和品牌 Footer；
- 响应式布局、深色/浅色主题和可访问性支持。

### 3.2 全站公开页面目标

让首页、定价页、关于页、登录页、注册页等公开页面共享一致的品牌壳层：

- Header 品牌标识、站点名称、导航、语言切换、主题切换和认证入口保持一致；
- Footer 的品牌信息、链接分组和版权区域保持一致；
- 页面主体继续保留各自的业务布局，不把首页 Hero 和长页面内容扩散到其他页面；
- 公共 Header 与已认证控制台统一使用固定的 Tokenfly 品牌图标；后台配置的系统名称仍用于显示名称，Footer 仍保留后台 logo 与 Footer 内容配置能力。

已认证控制台继续使用独立的控制台布局，但其品牌图标纳入本次统一范围，确保与首页保持一致。

## 4. 设计原则

1. 选择性移植，不直接合并参考分支的全部提交。
2. 自定义首页 URL、HTML、Markdown 的优先级保持不变。
3. 区分品牌图标与品牌文字配置：公共 Header/控制台图标固定为 TokenflyBrandMark；站点名称继续按页面显式配置、系统配置和默认值处理，Footer 继续按系统配置、页面配置和默认值处理。
4. 首页专属样式使用 `.brand-home`、`.signal-home` 等作用域，避免污染其他页面。
5. 共享 Header/Footer 的新增能力使用向后兼容的可选 props 和 variant。
6. 所有用户可见文案通过 i18n 提供，覆盖项目支持的全部语言。
7. 图片和动画需要考虑首屏性能、移动端表现和减少布局跳动。

## 5. 开发阶段

### 阶段 A：确认现有契约

- 核对当前 `PublicLayout`、`PublicHeader`、`Footer` 的 API 和系统配置优先级；
- 核对首页自定义内容、公告、认证状态、注册开关和定价权限的现有行为；
- 列出参考分支首页文件与当前分支的实际差异；
- 确认没有把参考分支中无关业务改动带入当前分支。

交付物：迁移文件清单和兼容性问题清单。

### 阶段 B：建立共享品牌壳层

- 为 `PublicHeader` 增加可选品牌标识、品牌名称和样式 variant；
- 为 `Footer` 增加首页/品牌 variant；
- 为 `PublicLayout` 增加页面级 className 和 Header/Footer 品牌参数；
- 保证没有传入新参数的现有页面保持原有表现；
- 公共 Header 和控制台不再使用后台 logo 覆盖固定 TokenflyBrandMark；保留系统名称与 Footer 配置的现有能力。

交付物：公开页面可复用的品牌 Header/Footer 能力。

### 阶段 C：移植首页

- 移植 `features/home/components/brand/` 下的首页组件；
- 移植品牌 mark 和首页图片资源；
- 移植并审查三份首页样式；
- 将 `features/home/index.tsx` 的默认 fallback 切换为 `BrandHome`；
- 保留加载态、自定义 URL、HTML、Markdown 和认证状态逻辑；
- 接入首页需要的 `/pricing`、`/sign-in`、`/sign-up`、`/dashboard` 路由入口。

交付物：默认首页达到参考分支的主要视觉和交互效果。

### 阶段 D：公告和国际化

- 移植首页公告队列和排序逻辑，默认对未登录与已登录访客均显示；
- 自定义首页 URL/HTML/Markdown 与默认 BrandHome 采用同一公告可见性规则；
- 补齐 `en`、`zh`、`zh-TW`、`fr`、`ja`、`ru`、`vi` 文案；
- 执行项目现有 i18n 同步和检查命令。

交付物：公告行为有明确边界，新增文案无语言缺失。

### 阶段 E：验证和回归

- TypeScript 类型检查和前端生产构建；
- 首页未登录、已登录、注册关闭三种状态；
- 自定义首页 URL、HTML、Markdown 覆盖逻辑；
- 单条和多条公告、“今日不再显示”；
- `/pricing` 可访问和需要登录两种情况；
- 关于页、定价页、登录页、注册页 Header/Footer 一致性；
- 深色/浅色主题、键盘导航、移动端断点和图片加载；
- 已认证控制台页面无意外样式变化；
- 最终 diff 审查，确认无关功能未被带入。

## 6. 页面影响预期

| 页面范围 | 预期影响 |
| --- | --- |
| `/` 默认首页 | 使用新的 BrandHome，视觉和内容结构发生明显变化。 |
| 自定义首页 | 主体内容保持配置结果；是否启用新公告行为需要在阶段 D 明确。 |
| `/pricing`、`/about`、登录、注册 | 页面主体不变，共享品牌 Header/Footer。 |
| 已认证控制台 | 保持独立控制台布局；品牌图标统一为 Tokenfly，其他业务布局和功能不变。 |
| i18n | 仅增加首页和品牌壳层文案 key。 |
| 构建产物 | 增加首页组件、字体和图片资源，需要检查体积与首屏性能。 |

## 7. 主要风险与控制措施

### 7.1 共享布局回归

固定 Tokenfly 图标与系统名称配置容易被误解为同一层级。控制措施：在布局契约中明确区分“图标固定”与“名称可配置”，并回归所有公开页面和控制台。

### 7.2 CSS 污染

首页样式中的通用选择器可能影响其他页面。控制措施：所有首页规则以 `.brand-home` 或 `.signal-home` 为作用域，并检查打包后的样式。

### 7.3 公告行为变化

首页自定义内容也会挂载公告弹窗。控制措施：所有首页形态统一使用公告队列，并对未登录访客保持可见。

### 7.4 品牌白标能力

公共 Header 与控制台的 TokenflyBrandMark 是产品固定品牌决策，不接受后台 logo 覆盖；该决策不影响系统名称、Footer logo 和 Footer HTML 的可配置能力。

### 7.5 性能和可访问性

控制措施：图片懒加载和压缩、避免首屏阻塞动画、保留键盘焦点样式、验证语义标题层级和移动端可读性。

## 8. 验收标准

开发完成后应满足：

1. 默认首页在结构、品牌视觉和主要交互上与 `codex/stellar-home-redesign` 一致。
2. 自定义首页的 URL、HTML、Markdown 行为未被破坏。
3. 公开页面共享统一 Header/Footer，但页面主体布局保持各自职责。
4. 公共 Header 与控制台始终显示 TokenflyBrandMark；系统名称、Footer logo 和 Footer 内容仍能按现有配置生效。
5. 所有新增用户可见文案具备完整多语言 key。
6. TypeScript 检查和生产构建通过。
7. 首页、公开页面和控制台回归检查无明显视觉或功能回归。

## 9. 开发启动评审

产品决策已确认：

- 公共 Header 与控制台固定使用 Tokenfly 品牌图标，保留现状并以本文档作为后续维护依据；
- 首页公告队列对未登录与已登录访客均可见，并统一应用于默认与自定义首页。

因此不再有待确认项，可以按本计划继续开发和回归验证。

## 10. 实施结果与复核

本次已完成：

- 默认首页切换为 `BrandHome`，保留自定义 URL、HTML、Markdown 首页分支；
- 移植品牌首页组件、品牌 mark、首页图片和首页专属样式；
- 公开页面统一使用品牌化 Header/Footer 壳层；公共 Header 与控制台固定使用 TokenflyBrandMark，系统名称和 Footer 配置仍按现有规则生效；
- 公告弹窗支持未登录与已登录访客、多公告队列和 `popupOrder`；
- 为 `en`、`zh`、`zh-TW`、`fr`、`ja`、`ru`、`vi` 补齐新增文案；
- 补充首页公告类型的 `popupOrder` 字段；
- 加入品牌 Footer 所需的静态 mark 资源。

复核结果：

- `bun run build:check`：通过；
- 首页相关文件定向 `oxlint`：通过，仅保留 Footer 原有 `dangerouslySetInnerHTML` 警告；
- 首页相关文件定向 `oxfmt --check`：通过；
- `git diff --check`：通过；
- 全量 `bun run lint` 和 `bun run format:check`：未通过，报告的问题分布在仓库既有文件中，不是本次首页改动引入的阻塞错误。
