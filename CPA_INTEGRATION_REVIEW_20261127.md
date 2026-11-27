# NewAPI 与当前容器版 CPA 对接评审

日期：2026-11-27（Asia/Tokyo）。NewAPI 分支 `feat/subscription-accounts`，更新前已从远端快进到 `ba39b10d446ba067d27429f59fee0d9197fd3148`。CPA 接口参考本机 `cpa-source-save-20261126-r2` 当前源码。此次只修改 NewAPI 工作副本；未部署、未调用真实账号、未修改 CPA 参数或凭据。

## 已更新

| 项目 | 原有情况 | 本次处理 |
|---|---|---|
| 容器代理标识 | 无 proxy_url 可能显示 Direct | 容器账号显示 Container，说明代理由容器管理；不能凭无地址推断直连或网络健康 |
| 刷新结果 | HTTP 200 被视为完成，忽略 ok/partial | 检查执行回执；部分失败显示具体 profile/usage 错误，同时保留成功查询的额度 |
| 重置卡缓存 | 较旧 usage 快照仍可覆盖卡片信息 | 与额度使用同一有效时间戳准入，旧快照不能覆盖较新记录 |
| Fable 数据 | 缺少 seven_day_oi 读取，可能只显示旧别名 | 补直接窗口，并与已有 limits / iguana_necktie 去重 |
| 额度合并 | 新的手动快照缺某窗口时会丢掉已有聊天观测 | 按窗口保留有效结果；同窗口取较新来源，仍保留容器到期不自行归零的行为 |
| 异常额度 | 负数比例可进入显示；极大重置时间能使页面异常 | 拒绝范围外利用率，无效重置时间显示未知，不抛出渲染异常 |
| 订阅标签 | 只识别部分历史标签 | 增加明确上报的 Ultra、Free、Team；保留历史标签与原价表，不推测 Ultra 价格 |
| 调度快照 | container_dispatch、route_revision 在字段过滤时丢失 | 保留中央 CPA 已返回的调度状态及版本；未新增容器直连或额外上游轮询 |

## 沿用的接口与流程

- 模型：客户端 → NewAPI 原有协议处理 → 中央 CPA → 账号容器。没有修改模型请求头、正文、身份字段或计费代码。
- 账号与被动额度：每 5 秒读取中央 CPA `/v8/management/credentials`。只导入已有快照，不触发 Anthropic 请求。这个频率与容器公网出口检查无关。
- 手动刷新：中央 CPA `/v8/management/account-environments/action` 的 `account-update`；查询仍由账号容器执行。
- 令牌刷新、代理查看：分别为同一中央入口的 `auth-refresh`、`proxy-view`，保留原逻辑。
- 模型列表、启停：保留 `credentials/models`、`credentials/status`。
- 用量统计：保留中央 CPA 的用量队列与 auth_index 关联。CPA 需启用原有 usage-statistics-enabled；本次未改其配置。
- 重置卡：读取上报快照；仅当 CPA 明确声明 reset_grant=true 时可显示消费入口。当前 CPA 声明 false，不绕过能力检查调用旧接口。

## 已验证

- 后端 CPA/容器相关测试：46 项通过，实际 CPA 集成用例 1 项因未配置隔离测试服务而跳过。
- 订阅账号前端：8 个文件、59 项通过；新增测试先复现了额度窗口丢失、时间转换异常及 Fable 字段遗漏。
- TypeScript：1,447 个文件、2 个项目，无类型错误。
- 修改的前端文件 lint 通过；前端生产构建通过；Mac ARM64 后端完整编译通过。
- 管理接口测试使用本地 httptest 虚构账号；验证没有调用旧 api-call 或下载订阅凭据。
- 未改数据库模型、迁移、SQL 或计费算法。后端管理回归使用测试 SQLite，未运行真实 MySQL/PostgreSQL 部署验收。

## 部署前仍需明确

1. 本次没有连接内网 NewAPI。源码随本次提交归档；推送仓库不等于内网部署，线上不会因此自动出现这些改动。
2. 一小时模型任务：源码默认 RELAY_TIMEOUT=0（无总时限），但 RELAY_RESPONSE_HEADER_TIMEOUT=1800、STREAMING_TIMEOUT=300。部署时建议显式使用下面的专用配置，并同步检查反向代理、负载均衡器及客户端的超时；不能只改其中一层。本次没有执行真实一小时任务。

   ```dotenv
   RELAY_TIMEOUT=0
   RELAY_RESPONSE_HEADER_TIMEOUT=7200
   STREAMING_TIMEOUT=7200
   ```

   以上是模型链路配置，不需要把管理页的短查询也拖成两小时；网络错误、断开及用户取消仍可提前结束。
3. 当前 NewAPI 原版错误路径仍经过 RelayErrorHandler 和状态码映射；跨协议回复会转换。因此不能承诺客户端收到与 Anthropic 完全相同的响应字节。请求正文透传选项也不等于错误与回复完全透传。本次保持原处理，未擅改错误、重试和计费行为。
4. CPU、内存、磁盘历史仍在 CPA 账号管理页；本次未把这套监控复制进 NewAPI，也没有增加高频容器资源抓取。
5. 完整终验需在内网部署候选后确认中央 CPA 可达、管理权限、模型路由、流式取消和用量关联。当前本地通过不代表该部署已验收。
6. 真实查询与重置卡消费没有测试，也没有为测试重复刷新现有账号。新增 Ultra 仅作为上报标签，原成本估算表没有对应价格，不能据此把估算的零值当作免费订阅。
