# New API 二开说明：「订阅账号」菜单

本分支 `feat/subscription-accounts` 基于官方 [QuantumNous/new-api](https://github.com/QuantumNous/new-api) **`v1.0.0-rc.47`**（`e835b1b`）修改，
对接 CPA（CLIProxyAPI）**v8.0.51** 的管理接口 `/v8/management`。

目的：把 CPA 管理面板「认证文件」页上的 Claude 订阅额度信息，在 New API 里集中展示，并提供少量常用操作。

---

## 一、功能清单

### 1. 菜单与权限

- 管理员区「渠道」上方新增菜单 **订阅账号**（`/subscription-accounts`）。
- **仅超级管理员（role 100）可见可用**，三层拦截：
  1. 侧边栏菜单 `requiredRole: SUPER_ADMIN`；
  2. 前端路由 `beforeLoad` 非超管跳转 `/403`；
  3. 后端路由组挂 `middleware.RootAuth()`（普通用户/管理员调用返回 403）。

### 2. CPA 管理

- 「添加 CPA 订阅信息」：名称 + 远程管理地址 + 管理密码，**可添加多个 CPA**。
- 地址可填 `http://host:8317` 或带 `/v8/management`，会自动规范化；只接受 http/https。
- 管理密码 **AES-GCM 加密入库**（密钥由 `CRYPTO_SECRET`，未设置时用 `SESSION_SECRET` 派生），
  接口和页面只显示 `****`；编辑时留空或保持 `****` = 不修改。
- 「测试连接」：保存前后都可测，记录最近一次测试结果和时间（列表里的绿/红点）。
- 「同步账号」：从各 CPA 拉取认证文件列表；CPA 上已删除的账号会从本页缓存移除。
- 删除 CPA：**只删除本页保存的连接信息和缓存**，不会动 CPA 上的任何账号（需二次确认）。

### 3. 账号卡片（内容与 CPA 面板 v1.27.2 一致）

| 区块 | 内容 |
|---|---|
| 身份 | 渠道类型、套餐（Pro / Max 5x / Max 20x / Team…）、禁用/不可用标记、名称/邮箱、所属 CPA |
| 警告 | 刷新错误、需要重新登录（有才显示） |
| 元数据 | 状态、请求成功/失败数、**账号代理（只读，密码打码；无代理显示「全局代理 / 直连」）**、上次刷新 |
| 剩余重置次数 | **次数 > 0：红底提醒 + 整张卡片红框 + 置顶**（多张按次数从多到少），并列出每批次的剩余/总数和过期时间 |
| 额外用量 | 已花费 / 限额（有才显示） |
| 美元余额 | 有才显示 |
| 额度条 | 5 小时、7 天、OAuth 应用、Opus、Sonnet、Cowork、**Fable（单独一行）**；显示**剩余**百分比和重置时间 |
| 检查时间 | 最近一次查询额度的时间；查询失败显示错误 |

**额度条颜色与 CPA 面板相同**（`QuotaMeter.tsx`）：剩余 **≥ 70% 绿**、**≥ 30% 黄**、**< 30% 红**。

### 4. 卡片按钮

| 按钮 | 作用 | 二次确认 |
|---|---|---|
| 模型 | 查看该账号可用模型 | 否 |
| 刷新 | 重新查询该账号的额度 | 否 |
| 刷新 token | 让 CPA 刷新该账号 token（返回内容丢弃，不落库） | **是** |
| 启用/关闭开关 | 调 CPA 启用或禁用账号 | **是**（开、关都要确认） |
| 重置额度 | 使用一次重置次数（仅次数 > 0 可点） | **是** |

已按要求**去掉**：删除、下载、代理设置。

### 5. 刷新机制

- **全部刷新**：所有账号都查，但请求在 **5–10 秒内错开发出**，不一起发。
  冷却 **默认 1 分钟、最低 1 分钟**；冷却中按钮显示倒计时。**鼠标悬停按钮弹出设置，可改冷却时间。**
- **后台自动刷新**：开关 **默认关**；间隔 **默认 10 分钟、最少 10 分钟**；每个账号额外 **随机延后 0–10 分钟**。
  只在主节点运行。
- 每个账号的最新结果和时间都缓存在数据库，打开页面直接显示缓存，不会触发请求。

### 6. 审计日志

8 类操作写入 New API 审计日志：CPA 添加/修改/删除、账号启用、账号关闭、刷新 token、重置额度、刷新设置修改。

### 7. 安全处理

- 读取认证文件时只取 `proxy_url`，其余内容（含 token）立即丢弃。
- 列表字段走白名单，页面拿不到任何 token。
- 查询额度、重置额度都经 CPA `api-call` 代发，**走该账号自己的代理**（与 CPA 面板相同）。
- 重置额度沿用 CPA 面板逻辑：结果不明时 10 分钟内复用同一个 `request_id`，避免重复领取；成功后清除该账号的冷却。
- **SOCKS 守卫（2026-11-04）**：New API 发往 Anthropic 的每个请求（查额度、领重置卡、刷新 token）发出前，都会先从 CPA 重新读取该账号的 `proxy_url`。只有 `socks5://` 或 `socks5h://`、并且带主机和端口的才放行；空值、`direct`/`none`、`http(s)://`、格式错误一律拦截，不发请求，卡片上显示 `blocked: ...`。读取结果缓存 60 秒。原因：CPA 遇到空代理或格式错误的代理会悄悄改成直连，暴露真实 IP。

---

## 二、对原有代码的改动（4 处入口）

| 文件 | 改动 |
|---|---|
| `router/api-router.go` | 新增 `/api/subscription-accounts` 路由组（`RootAuth`） |
| `model/main.go` | `AutoMigrate` 增加 3 张表 |
| `main.go` | 启动后台自动刷新调度器 `service.StartCpaSubscriptionScheduler()` |
| `web/src/hooks/use-sidebar-data.ts` | 侧边栏菜单项 |

另：7 个语言文件 `web/src/i18n/locales/*.json` **只在末尾追加**文案；`web/src/routeTree.gen.ts` 为构建自动生成。

## 三、新增文件

```
model/cpa_subscription.go            3 张表 + 读写
service/cpa_subscription.go          CPA 客户端、加解密、快照
service/cpa_subscription_ops.go      同步/刷新/调度/开关/刷新 token/重置额度
service/cpa_subscription_test.go     单测
controller/cpa_subscription.go       接口 + 审计模板
web/src/features/subscription-accounts/        页面、卡片、弹窗、额度计算、单测
web/src/routes/_authenticated/subscription-accounts/index.tsx
```

新表：`cpa_instances`、`cpa_account_caches`、`cpa_subscription_settings`（启动时自动建表，无需手工迁移）。

## 四、接口（均需超级管理员）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/subscription-accounts/` | 总览：CPA 列表 + 账号缓存 + 设置 |
| POST / PUT / DELETE | `/instances`、`/instances/:id` | CPA 增 / 改 / 删 |
| POST | `/instances/test` | 测试连接 |
| POST | `/sync` | 同步账号列表 |
| POST | `/refresh-all` | 全部刷新（冷却中返回 `cooldown_remaining`） |
| PUT | `/settings` | 自动刷新 / 冷却设置 |
| POST | `/account/refresh` | 刷新单个账号额度 |
| GET | `/account/models` | 账号模型列表 |
| POST | `/account/status` | 启用 / 关闭 |
| POST | `/account/refresh-token` | 刷新 token |
| POST | `/account/reset-grant` | 重置额度 |

## 五、测试记录（2026-11-03，本机）

- 后端：`go vet`、`go test ./service -run Cpa`、`go build` 通过。
- 前端：`bun check -b`、oxlint、oxfmt、版权头检查、vitest（4 个用例）、`bun run build` 通过。
- 端到端（本机 New API + 本机测试用 CPA v8.0.51）：
  错误密码测试连接返回 401 提示 ✅；添加 CPA、密码显示 `****` ✅；同步账号 ✅；单账号刷新 ✅；模型列表 ✅；
  关闭→CPA 上 `disabled=true`、启用→恢复 ✅；设置低于下限被抬到 10 分钟 / 60 秒 ✅；
  全部刷新错开 8.9 秒发出、第二次被冷却拦截 ✅；保持 `****` 修改后仍能连通 ✅；
  总览接口不含任何 token ✅；审计日志 5 类已写入 ✅；普通用户访问返回 403 ✅。
- 界面：用演示数据检查了红框置顶、红底重置次数、三档颜色、悬停冷却设置、开关二次确认。
- **未测**：真实 Claude 账号的额度查询和「重置额度」（本机测试 CPA 只有 Kimi 账号）。

## 六、部署

与官方一样构建镜像即可（`docker build`），首次启动自动建表。
**建议设置 `CRYPTO_SECRET`**；若之后更换该值（或未设置时更换 `SESSION_SECRET`），已保存的 CPA 管理密码需重新填写。
