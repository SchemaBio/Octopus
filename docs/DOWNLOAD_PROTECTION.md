# COS 直连下载防护（2026-10-29，本地实现）

## 行为与限制

本功能仅用于 SaaS 的付费 BAM 和结果 ZIP。文件仍由用户直连 COS 下载，不经过入口服务器。新功能默认关闭；打开前必须完成下面的真实云验收。旧 `DIRECT_COS_DOWNLOADS_20261010.md` 描述的是默认关闭时的历史行为。

启用后，每份付费申请固定一个可信公网 IP，STS Policy 使用 `ip_equal / qcs:ip` 限定读取指定对象。IP 来自 Squid 校验并签名的身份，忽略浏览器自行提供的地址。IPv4/IPv6规范化；无法可靠识别公网IP时不签发、不新增扣费。首次成功签发前的失败重试不会锁死未付款申请。授权成功后不能自动换绑。

流量阈值为 `ceil(size_bytes * 13 / 10)`，使用整数避免溢出或浮点误差。8,000,000,000字节的文件对应10,400,000,000字节。按日志中的返回字节数累计，Range续传、重试、多人或重复下载都消耗观测额度；不会按唯一区间去重。字节数是观测值，不是新的计费项。

达到阈值后永久停止该申请的新签发，保留历史统计；用户须明确确认费用才能创建新付费申请。不会自动扣费、清零旧记录或自动退款。原3小时申请窗口、默认30分钟链接、60秒刷新间隔、12次签发和每请求10MiB/s限速保持。BAM保留期可以缩短窗口。

**不是严格实时上限**：COS日志可能延迟、缺失或不准确，已签发链接及已开始的传输不能通过停发立即撤销。同出口IP的多人仍可能共享。Parquet、IGV证据地址等独立读取能力不属于本功能，不能宣称所有COS出网都受1.3倍限制。COS计费仍以腾讯云账单为准。

## 配置与日志

Octopus配置：

| 环境变量 | 默认值／用途 |
| --- | --- |
| `DOWNLOAD_PROTECTION_ENABLED` | `false`；完成验收后显式启用 |
| `DOWNLOAD_CLS_REGION` | 空；COS日志主题所在地域，例如 `ap-singapore` |
| `DOWNLOAD_CLS_TOPIC_ID` | 空；专用CLS主题ID |
| `DOWNLOAD_CLS_CREDENTIALS_FILE` | 空；只读挂载的独立凭据文件路径 |

凭据文件使用JSON字段 `secret_id`、`secret_key`，临时身份可附带 `token`；权限仅限指定CLS主题的SearchLog及其必要读取权限，不复用或授予存储写权限。文件应由部署负责人以0400或0600保护、只读挂载，不进入仓库、镜像、控制台响应或普通日志。COS→CLS投递由管理员在腾讯云配置授权，**有额外日志费用**。

应用使用CLS API `SearchLog / 2020-10-16`，通过固定官方HTTPS入口及TC3签名查询；配置地域、主题及桶，不能通过请求指定任意上游URL。禁止在普通访问日志记录原始CLS记录、签名URL或STS密钥。

每分钟运行一次，有PostgreSQL连接级排他锁；手动重试与多实例不能并行推进。消费游标持久保存，增量查询重叠10分钟，每小时重查近24小时。分页未完成不推进游标，已提交日志靠桶/请求ID去重。更晚或遗漏日志可能无法观测，不承诺完整计量。

签发记录保存临时KeyId的SHA256、对象身份摘要、IP和到期时间，不存Token、临时SecretKey及完整URL。日志必须同时匹配临时KeyId、桶与对象；记录插入、申请累计和阈值事件在同一事务内完成。重复记录内容冲突会报告固定错误，不能重复累计。请求/响应日志原文仅留在云端受保护的CLS中，限制访问者及保留期。

首次没有成功查询，或最近成功查询超过5分钟，会停止新签发；恢复查询后可重试原申请。查询成功不证明日志投递正常；页面显示最后查询成功、回查时间、计数更新时间及固定错误类别。日志为空不能作为投递验收通过。

## 用户与管理员

用户在已有申请卡片查看绑定IP、已观测流量、阈值和同步时间。网络改变后，从新网络提交 `POST /api/v1/tasks/:id/downloads/network-requests`，请求体只需 `grant_id`；目标IP由认证上下文确定。相同待处理请求幂等复用，不能跨用户/组织申请。

Cuttlefish「设置→平台计费→下载防护事件」提供组织、用户、签发状态及待处理网络变更筛选；可查看详情、审核、暂停签发、重试日志同步。写入经Cuttlefish同源/CSRF/管理员校验，再经Squid签名管理身份到Octopus，不直接写数据库。

管理员接口为 `/api/v1/admin/download-protection`（经Squid时加 `/api/v1/octopus` 前缀）：

- `GET /grants`：分页列表及全局同步健康，`page`、`org_id`、`user_id`、`state`筛选；每页50条，`state=network_pending`查待处理换绑。
- `GET /grants/:grant`：申请、网络申请、事件与签发期限详情。
- `POST /grants/:grant/approve|reject|pause`：`revision`、`request_id`（审核时）、3～500字的`reason`；revision冲突返回409，不覆写新状态。
- `POST /sync`：限频重试同步，操作写审计。

批准换绑前核对当前任务执行、保留期、对象大小/ETag、原申请有效期、剩余额度和状态。审批进入等待状态，旧签名与STS凭据的最晚到期时间再加30秒后，新IP才可领取链接。原申请截止时间、累计流量和签发次数不变；没有足够剩余时间则拒绝批准。旧连接可能仍继续传输，不保证同一时刻绝对零重叠。

没有免费追加额度、清零流量、自动换绑或延长申请时间按钮。超阈值后重新购买会生成独立幂等账务引用；旧账务与申请保留。管理员暂停也只停止新签发。

## 发布与验收

1. 备份业务库、部署环境和旧镜像。新增表为 `download_issuances`、`download_usage_events`、`download_usage_syncs`、`download_network_requests`、`download_protection_events`，同时新增下载申请字段；新表仅在SaaS启动迁移。
2. 保持防护关闭，先部署Octopus→Squid→Cuttlefish→YiJian；保留Squid `SAAS_SETTINGS_PRESERVE_APPLIED=true`，不得应用或清除待计算配置。保持DuckDB和分析工作流不变。
3. 管理员开通专用COS→CLS日志投递、挂载最小权限读取凭据，确认能查询并实际匹配字段 `userSecretKeyId`、`requestId`、`bucketName`、`reqPath`、`reqMethod`、`resBytesSent`、`eventTime`。
4. 在隔离测试环境使用专用小型合成对象和测试账务桩：验证同IP206、不同IP403、Range、实际日志KeyId关联、重复日志、停发、换绑等待与重新购买确认。需从浏览器真实网络核实API与COS看到的公网IP/地址族一致；VPN、双栈或特殊出口不一致时不能放宽策略绕过验证。
5. 生产切换先暂停新报价/签发，等待已有历史申请和签名/STS凭据自然到期；不得把历史未绑定IP的申请标为已防护。随后开启防护并逐项检查。旧版诊断CLI不能证明新模式有效，保护开启时它会明确拒绝。
6. 未提供CLS、真实IP及小对象日志证据时，记录「代码验收通过、真实云验收待完成」，不得开启或宣称生产已受保护。

回退先暂停新签发，保留已付申请、账务、观测及签发记录。不要恢复旧数据库；不要换回会为受保护申请签发无IP链接的旧实现。关闭新申请功能开关不会移除已受保护申请的校验，应使用兼容版本继续处理。

本地验证：仅使用名为 `download_protection` 的回环PostgreSQL测试库，设置 `DOWNLOAD_PROTECTION_TEST_DSN` 后执行 `go test ./internal/service ./internal/handler ./internal/router ./internal/middleware ./internal/config -run 'TestDownload|TestDirectCOS|TestInterpretation|TestBusinessPermission|TestValidateStartup' -count=1`。无测试DSN时PostgreSQL用例会跳过；交付记录必须注明是否实际执行。

官方依据：[IP策略条件](https://intl.cloud.tencent.com/zh/document/product/436/46206)、[COS日志检索与字段](https://intl.cloud.tencent.com/zh/document/product/436/59486)、[单请求限速](https://intl.cloud.tencent.com/zh/document/product/436/34072)。
