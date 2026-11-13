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
| ~~刷新 token~~ | **2026-11-04 卡片上已去掉**（CPA 后台会自动刷新）；后端接口 `/account/refresh-token` 保留 | — |
| 启用/关闭开关 | 调 CPA 启用或禁用账号 | **是**（开、关都要确认） |
| 重置额度 | 使用一次重置次数（**2026-11-04 起仅剩余次数 > 0 才显示**） | **是** |

已按要求**去掉**：删除、下载、代理设置。

### 4.1 卡片改版（2026-11-04）

- 标题后恢复**套餐小标签**（如「Max 20x」）。
- 鼠标停在卡片上 **0.3 秒**，右侧弹出**详情框**（`components/account-details.tsx`）：文件名、所属 CPA、状态、代理（打码）、上次刷新 token、最近请求、成功/失败、**近 30 天 token 分项**（输入 / 输出 / 缓存读写，来自 `/stats?days=30`）、消费、各额度重置时间，以及额外用量 / 美元余额等（有才显示）。
- SOCKS 徽章**不再悬停弹框**（地址已在详情框），点击仍可查看完整地址并复制。
- 成功 / 失败数 **≥ 10,000 显示 k / M**（截断不四舍五入），带虚线下划线，悬停显示精确数字。
- 查看完整 SOCKS 地址时，若该 CPA 未保存管理密钥，提示「该 CPA 未保存管理密钥，请编辑该 CPA 重新填写管理密钥」（原报 `unsupported secret format`；2026-11-04 由英文改为中文）。

### 5. 刷新机制

- **全部刷新**：所有账号都查，但请求在 **5–10 秒内错开发出**，不一起发。
  冷却 **默认 1 分钟、最低 1 分钟**；冷却中按钮显示倒计时。**鼠标悬停按钮弹出设置，可改冷却时间。**
- ~~后台自动刷新~~：**2026-11-06 已去掉**（页面开关、分钟输入、`runCpaAutoRefresh` 都删了）。数据库里旧的 `auto_refresh_enabled` / `auto_refresh_interval_minutes` 字段保留不用，不影响升级。
- 额度、套餐**只能手动刷新**；单卡「刷新」会先同步该账号的凭据信息（`RefreshCpaAccountWithCredential`）。
- 每个账号的最新结果和时间都缓存在数据库，打开页面直接显示缓存，不会触发请求。

### 5.1 实时数据（2026-11-06）

| 项 | 频率 | 说明 |
|---|---|---|
| 凭据同步 `cpa_subscription_credential_sync.go` | **每 5 秒** | 读 CPA `/credentials`（只读 CPA 内存，不发往 Anthropic）：成功 / 失败数、最近请求、状态、对话额度。没变化不写库；账号锁被占用就跳过；不删除账号 |
| 用量采集 `cpa_subscription_usage.go` | **每 5 秒**（原 15 秒） | 仍需每台 CPA 单独打开「采集用量」 |
| 页面刷新 | **每 5 秒**（全部刷新进行中 3 秒），切回窗口立即刷新 | — |

### 5.2 卡片显示对话额度（2026-11-06）

- 来源：CPA 每次对话（含流式，结束后）把 `Anthropic-Ratelimit-Unified-*` 响应头存到账号的 `quota.signals`，`/credentials` 返回。New API 不发任何额外请求。
- 前端 `lib/quota.ts`：`buildPassiveQuota` 解析 5h / 7d 的 Utilization（0～1）、Reset（unix 秒）、Status；`buildEffectiveQuota` 在对话数据比手动刷新更新时替换同名的 5h / 7d 窗口，其余窗口（Opus、Sonnet 等）保留刷新结果。
- 显示：额度条下方小字 `components/quota-source-line.tsx`（「来自对话」绿点 /「来自刷新」灰点 + 相对时间）；Status=rejected 显示红色「已限流」；重置时间已过视为满额并显示「已重置」。号池「最紧张账号」统计也改用这份数据。
- 拿不到的：Opus / Sonnet 等细分额度、套餐类型、重置次数，仍靠刷新。Fable：2026-11-06 生产实测，CPA 按模型存的 `claude-fable-5` 对话头只有 Credits / Overage / Reset，**没有使用百分比**，所以 Fable 额度仍靠刷新。

### 5.3 每日随机刷新（2026-11-06，`service/cpa_subscription_daily_refresh.go`）

| 规则 | 值 |
|---|---|
| 查什么 | 只查 profile（套餐类型）+ `cedar_ember`（重置次数），**不查额度接口** |
| 次数 | 每账号每天随机 1 次或 2 次，各 50% |
| 时间 | 全天 0～24 点随机；两次至少隔 4 小时；当天剩余不足 4 小时只排 1 次 |
| 检查频率 | 每 30 秒看一次是否到点；同时到点的账号在 20 秒内错开 |
| 错过 | 服务停机等错过超过 10 分钟的时刻不补 |
| 跳过 | 账号停用、正在全部刷新时；没配 SOCKS 或 SOCKS 不通 → 记录错误后跳过，**不直连** |
| 存储 | 计划存在账号快照 `auto_refresh_day / plan / done / last_at / error`，悬停详情显示「下次自动刷新」「今天已完成」或「上次自动刷新 · 已跳过或失败」 |

### 5.4 周期对比 / 近 7 天成功失败 / 失败原因 / 重置卡显示（2026-11-11）

| 项 | 说明 |
|---|---|
| 卡片提示条 | 原来只显示一条（socks > missing > 重置卡），改为**叠加显示**；重置卡提示改紫色 + 🎫。「重置额度」按钮未配 SOCKS 仍置灰（不变；5.5 起直连号可点） |
| 成功 / 失败 | 有采集数据时改为**近 7 天**（New API 数据库），否则回退 CPA 原数据。30 天 token / 费用不动 |
| 失败原因 | 解析 CPA 用量记录的 `fail.status_code / code`，按小时存 `cpa_usage_fail_hourly`（保留 90 天）。5 类：429 限流、502/0 代理、401/403 认证、5xx 上游、其他。卡片悬停 + 详情框显示，含最近一次失败；**只从本功能上线后开始统计** |
| 详情「重置卡」栏 | 每张卡：剩余 / 总次数、到期、状态（已暂停 / 可用 / 暂不可用）；查询出错显示在此栏 |
| 周期对比 | 统计页**底部新区块**（原页面无页签结构，未按设计稿做成页签）。窗口：seven_day + seven_day_opus / sonnet / fable。表 `cpa_quota_cycle` 长期保留 |
| 周期口径 | token = 输入 + 输出 + 缓存写入，缓存读取单列；结束方式：自然到期 / 手动重置 / 套餐变更（插分隔行）；resets_at 漂移 ≤ 1 小时视为同一周期；被拒或用量 ≥ 99.5% 记为打满；用量 < 20% 不推算满额；和上期比 ≤ −5% 标红 |
| 回填 | 历史周期由已有每小时用量回填，标「回填，仅供参考」 |
| 数据来源 | 只统计经过 CPA 的用量；**不新增任何发往 Anthropic 的请求** |

新接口：`GET /api/subscription-accounts/cycles`。新文件：`model/cpa_subscription_cycle.go`、`service/cpa_subscription_cycle.go`(+`_test`)、前端 `components/cycle-section.tsx`、`components/stats-panel.tsx`、`lib/cycle.ts`、`__tests__/cycle.test.ts`。`model/main.go` 的 AutoMigrate 增加 2 张表。

### 5.5 重置额度按钮 / 失败原因悬停框（2026-11-11）

| 项 | 说明 |
|---|---|
| 直连号可领卡 | `proxy_url` **完全为空**的账号（直连号），领卡请求仍经 CPA `api-call` 发出，由 CPA 直连。填了但格式不对仍拦截；**刷新 token 仍必须 SOCKS**。`service/cpa_subscription_ops.go` 新增 `resetGrantGuard`，结果和审计日志多一项 `direct` |
| 按钮状态 | 只有 SOCKS 不可用（非直连号）时置灰；灰按钮外包一层，悬停能看到原因 |
| 提前提示（用户选 B） | 用上次刷新的重置卡数据判断：不符合条件 / 冷却中 / 卡暂停 / 未开始 / 已过期 / 暂不可用 / 额度没用满（对话显示已限流也算用满）。**只提示不置灰**，点击后由服务端判断。`lib/quota.ts` 的 `resetButtonHint` |
| 确认框（方案 C 居中式） | 新组件 `components/reset-confirm-dialog.tsx`，用弹窗组件自带的小尺寸居中样式（size='sm'）：红底 ↻ 图标 / 标题 / 邮箱（太长省略，悬停看全名和文件名）/「剩余 2 次 → **1 次** · 直连（橙）或 SOCKS 代理（绿）」/ 小号灰字「将清除该账号的限流，操作不可撤销」；取消、继续平分一行。公共 ConfirmDialog 不改，启用/停用确认框不变 |
| 失败原因悬停框 | 原来内容被挤成横向多列、文字竖排，改为上下排列，0 次的行变淡；去掉「统计从本功能上线那天开始」底注 |
| 卡片统计行（方案 A） | 原来一行塞不下，拆两行：第一行「最近请求 X ····· 消费 X」，第二行「近 7 天 成功 X · 失败 X」（失败数仍可悬停看原因），字号不变 |
| 右上角代理标识（方案 B） | 直连号（`proxy_url` 为空）显示黄色「● 直连」，并去掉标题旁的「直连」标签和黄色提示条；代理填了但格式不对改为**红色**「● SOCKS」，黄条「socks5 代理格式无效，额度查询已拦截」保留；SOCKS 正常绿色、连不上红色不变。`components/socks-badge.tsx` |

测试：`TestResetGrantGuard`、`__tests__/reset-button.test.ts`。

### 5.6 账号轮休 / 安全闸 / 监控告警 / RPM·TPM（2026-11-12）

**升级后的默认状态：轮休总开关关闭、演练模式关闭、401/403 自动停用关闭、企微 / 钉钉推送关闭。不打开就不会有任何账号被自动下线。**

#### 轮休（`service/cpa_subscription_rotation.go`）

- 做法：到点的账号在 CPA 上**停用**（与手动开关同一个接口，不发往 Anthropic），休息够了再启用。只会叫回带轮休标记的账号；被 SOCKS 守卫、401/403 或手动停用的账号**永远不碰**。
- 每 30 秒左右算一次。决策函数 `planCpaRotation` 是纯函数（不碰 CPA、不碰数据库），便于测试。
- 下线原因及优先级：排班 > 单号保护（短时 429 过多 / 额度快用完 / 当天工作时长到上限）> 普通轮休（连续工作 N 小时）。
- 上限：每台 CPA 同时休息不超过「最多休息比例」（夜间时段可用单独比例），且始终保留「最少在线数」；超出的排队，页面显示排队原因。
- 繁忙叫回：号池近期 429 过多或人均 RPM 过高时提前叫回休息中的账号；额度快用完而休息的账号优先叫回；额度已打满的不提前叫回。
- 设置弹窗 7 个页签：基本 / 安全闸 / 繁忙叫回 / 夜间 / 排班 / 单号保护 / 账号；每条规则旁显示「当前命中 N 个号」。

#### 安全闸（打开轮休不会一下子下线很多号）

| # | 规则 | 默认 |
|---|---|---|
| 1 | 只统计打开开关**之后**的工作时长（`rotation_enabled_at`），之前的不算 | — |
| 2 | 分批下线：每台 CPA 每步最多下线几个 | 1 个 |
| 3 | 两步之间至少间隔 | 5 分钟 |
| 4 | 下线前先看负载：剩下的在线号按近 1 小时最忙一分钟算，人均 RPM 不超过上限才下线 | 开，上限 8 |
| 5 | 同上，人均 TPM 上限 | 0 = 不查 |
| 6 | 号池最近几分钟内出现过 429 就暂缓 | 5 分钟 |
| 7 | 演练模式：只计算、只在页面标「本应休息」并写系统日志，**不停用任何账号** | 关 |

- 打开总开关必须走「预览 → 确认」：`POST /rotation/preview` 用未保存的设置试算，返回第一步会下线谁、谁排队及原因、每台 CPA 下线前后的负载，不改任何设置。
- 应急按钮「全部叫回并暂停」：`POST /rotation/stop`，关闭轮休并立即启用所有因轮休休息的账号，写审计 `cpa_subscription.rotation_stop`。
- 排队原因代码：`min_online`、`percent`、`busy`、`batch`、`capacity`。

#### 每分钟用量与 RPM / TPM（`service/cpa_subscription_minute.go`，新表 `cpa_usage_minute`）

- 从已有的用量采集里顺带按分钟汇总，保留 7 天；**不新增任何请求**。
- **TPM = 输入 + 输出，不含缓存读写**，与 New API 使用日志口径一致。小时级统计（账号对比表、30 天 token）仍含缓存，页面已注明。
- 卡片显示最近一分钟和 5 分钟平均；监控表有「近 1 小时峰值 TPM」列和合计行。
- 图表 `GET /rate-series?range=1h|12h|24h|7d`：1 小时为面积图；更宽的范围柱 = 该时段最忙一分钟、线 = 平均；成功 / 失败分色，429 单独计。

#### 监控告警（菜单「监控告警」，在「订阅账号」下方、「渠道」上方，仅超管）

- **401 / 403 自动停用**（开关 `auth_stop_enabled`，**默认关**）：转发请求返回 401 / 403 的账号在 CPA 上停用并产生告警，不会自动恢复。
- **企微 / 钉钉群机器人推送**（`service/cpa_subscription_notify.go`）：四类事件各有开关（SOCKS、401/403、CPA 连不上、轮休）；webhook 和钉钉签名密钥加密保存，不回传浏览器；`POST /alerts/test` 发测试消息。两个通道默认关。

#### 其他

- 周期对比改为全账号总览表（原来一次只能看一个号）。
- 使用日志的时间筛选加「昨天 / 上周 / 上月」（`web/src/features/usage-logs/components/compact-date-time-range-picker.tsx`）。
- 审计新增：`rotation_rest`、`rotation_wake`、`rotation_stop`、`account_shift`、`alert_test`。

新接口：`POST /alerts/test`、`GET /rate-series`、`POST /account/shift`、`POST /rotation/preview`、`POST /rotation/stop`。
新文件：`model/cpa_subscription_minute.go`、`service/cpa_subscription_minute.go`(+`_test`)、`service/cpa_subscription_notify.go`、`service/cpa_subscription_rotation.go`(+`_test`)；前端 `monitor.tsx`、`components/{monitor-panel,rate-chart,rotation-bar,rotation-preview,rotation-settings-dialog,emergency-stop}.tsx`、`lib/{rate,rotation}.ts`、`__tests__/{rate,rotation}.test.ts`、路由 `routes/_authenticated/subscription-accounts/monitor.tsx`。
`model/main.go` 的 AutoMigrate 增加 1 张表 `cpa_usage_minute`；`cpa_subscription_settings` 增加若干列（启动自动加，无需手工迁移）。

测试：`go test ./service/`、vitest 67 条通过。本机测试环境实测预览、应急按钮、演练模式。**未在真实号池上跑过轮休，生产首次开启建议先用演练模式观察一两天。**

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
| `main.go` | 启动后台调度器 `service.StartCpaSubscriptionScheduler()`（凭据同步、用量采集、每日随机刷新） |
| `web/src/hooks/use-sidebar-data.ts` | 侧边栏菜单项 |

| `web/src/features/dashboard/components/overview/overview-dashboard.tsx` | 概览页**去掉设置引导**（「开始使用」三步、「首个 API 请求」、「推荐操作 / 保持平台就绪」、折叠进度条、标题栏「设置引导」按钮）；测试 `__tests__/setup-guide.test.tsx` 改为断言不再出现（2026-11-04） |

另：7 个语言文件 `web/src/i18n/locales/*.json` **只在末尾追加**文案；`web/src/routeTree.gen.ts` 为构建自动生成。

## 三、新增文件

```
model/cpa_subscription.go            3 张表 + 读写
service/cpa_subscription.go          CPA 客户端、加解密、快照
service/cpa_subscription_ops.go      同步/刷新/调度/开关/刷新 token/重置额度
service/cpa_subscription_test.go     单测
service/cpa_subscription_credential_sync.go   每 5 秒同步凭据信息（2026-11-06）
service/cpa_subscription_daily_refresh.go     每日随机刷新套餐 / 重置次数（2026-11-06）
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
| PUT | `/settings` | 冷却设置（自动刷新字段已不用） |
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
