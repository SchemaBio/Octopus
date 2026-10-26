# SepiLens 接入交接

2026-10-11。接入目标为 SepiLens `0.2.0-rc2` 发布 API。本轮未修改 SepiLens 源码、引擎批准门禁或结算约定，未调用真实模型、开启收费或远端部署。

## 镜像与运行

保留普通 Octopus 构建，增加可选组合镜像：

```sh
docker build -t octopus:ai-base .
docker build --build-context sepilens_release=../SepiLens/dist/rc2 \
  --build-arg OCTOPUS_IMAGE=octopus:ai-base \
  -f Dockerfile.sepilens -t octopus:sepilens-integration .
```

发布目录需有指定版本及架构的 `.tar.gz`。构建验证归档和二进制 SHA256，保留知识数据、LICENSE 和第三方声明，不自动下载或更新知识包。

| 架构 | 归档 SHA256 | 二进制 SHA256 |
|---|---|---|
| amd64 | `a510828f7ca8c1cbd0129f7237c04001b09d23bd976d42c0ab1968406d03af7d` | `c6d68f400bc2467cd775c6a6df2e28b062725895f419ee00b5fde8f896f9a342` |
| arm64 | `c416c0e3f623a834836d0175b78105a0ad7b27c1734f8ba0329ad8bfd435588a` | `26073c22f10598fff2e90ae864fa3980d80c21d67f3c467397872cf51a19e433` |

更新发布包时同步传入 `SEPILENS_VERSION`、对应架构的归档与二进制 SHA256 构建参数，并重新验证合约。禁止浮动 latest 和跳过校验；生产基础镜像也应使用明确构建身份。

SepiLens 固定监听 `127.0.0.1:18081`，不发布到公网；由 Octopus 管理启停，最多重启 5 次，退避 1/2/4/8/16 秒。AI 故障不影响普通任务服务的 `/health`。镜像为 UID 1000 非 root 用户。

| 环境变量 | 用途/默认值 |
|---|---|
| `AI_ASSISTANCE_MASTER_KEY_FILE` | 稳定 secret 文件，base64 编码的 32 个随机字节；加密平台敏感负载和 SepiLens 数据 |
| `AI_ASSISTANCE_TOKEN_FILE` | 高强度专用服务 Token 文件，仅服务端使用 |
| `AI_ASSISTANCE_MANAGED_PROCESS` | 组合镜像为 true；外部 SepiLens 显式改为 false |
| `AI_ASSISTANCE_BASE_URL` | `http://127.0.0.1:18081`；可配置受信外部服务地址 |
| `AI_ASSISTANCE_BINARY` / `AI_ASSISTANCE_DATA_DIR` | `/app/sepilens/sepilens` / `/data/sepilens` |
| `AI_ASSISTANCE_INSTANCE_ID` | 托管模式必填，随唯一数据卷保留的稳定身份；Compose 示例使用 SEPILENS_INSTANCE_ID |
| `AI_ASSISTANCE_BUNDLE_DIR` / `AI_ASSISTANCE_RULES_DIR` | 可选只读知识包及批准材料目录，平台不生成批准材料 |
| `AI_ASSISTANCE_ALLOW_PRIVATE_ENDPOINTS` | 默认关闭，仅显式 true 放行私有模型端点 |
| `AI_ASSISTANCE_BILLING_ENABLED` | 仅控制其他正式 AI 判读功能的收费，默认关闭；SaaS HPO 候选提取独立按每次 1 积分收费 |

Token/主密钥也支持无 `_FILE` 的环境变量，文件优先。生产使用 secret 文件，不编入镜像。外部 SepiLens 的 principal 需要提交请求及管理 provider 权限；用户与机构隔离仍由 Octopus 实施，浏览器不可直接使用其任务 ID 访问上游。

挂载的 secret 文件须允许容器 UID 1000 读取，且只授权所需用户；不能仅供 root 读取，也不应向其他用户公开。

`deploy/sepilens.override.yaml` 提供卷与 secret 挂载示例；另将基础 Compose 的 Octopus image 指向组合镜像。每个副本必须使用独立卷，禁止共享 SQLite。托管进程使用操作系统目录锁，异常退出自动释放。扩容可使用独立管理的外部 SepiLens。

备份包括 Octopus PostgreSQL、SepiLens 完整 SQLite 数据、主密钥、二进制及知识/批准包。替换主密钥不能代替密钥轮换；跨数据库迁移的回滚按两端备份恢复流程执行。

## 平台 API 与映射

RC2 验收目标为“五类入口完成接线；HPO 候选提取和 SVC 草案实验可验证调用；ACMG/Loss/Gain 验证批准门禁及开放后的调用链；真实收费保持关闭”。接口接线不等于正式医学启用。

能力返回 mode=candidate/experimental/formal。HPO 使用独立候选可用标志；SVC 仅在 status=experimental 且 experimental_available=true 时开放草案入口，医学 enabled 仍为 false。YiJian 的 SVC AI 与 ACMG AI 均由 Octopus 在任务及样本权限校验后补入临床字段白名单和位点注释，发送 evidence_package.ai_context；用户无需补充疾病、遗传模式、样本类型或变异来源。SVC 明确提交 mode=experimental、options.allow_experimental=true；未知信息保留 unknown，不推断为 germline_confirmed。浏览器手动草案不作为已核验的 draft_assessment 发送，独立的手动草案计算流程保持原样。AI 临床整理摘要及候选条款从 agent_proposal.ai_assessment 展示，实验结果保留在 agent_proposal.experimental_result；不提供正式规则分类、不写入人工判读、不结算。SepiLens 的独立结构化草案接口仍校验锁定版本、MOI 和材料适用性。

增加管理员 POST `/settings/test`（Cuttlefish `/api/manage/ai-settings/test`），保留 CSRF 和测试原因审计。测试使用完整配置，空 Key 由服务端补全；必须由管理员明确点击费用确认，测试可能产生模型商费用，平台不扣积分。逐项解析 supported/unsupported/unknown；HTTP 200 不表示所有能力通过，supported_protocols 也不证明模型支持 Schema 或流式。不会在保存配置时自动调用模型。

增加 DELETE `/requests/{requestId}`，仅允许权限内、已终态且完成对账的请求删除结果。结果立即从平台移除，上游 job 删除由原实例后台重试；无法确认执行结果的 uncertain 请求不自动删除上游或其 profile。

沿用 JWT、受信 Squid 身份及 CSRF。开源前缀 `/api/v1/ai-assistance`；SaaS 前缀 `/api/v1/octopus/ai-assistance`。返回 `{data: ...}`，错误 `{error: ...}`。

| 方法与相对路径 | 行为 |
|---|---|
| GET `/settings` | 开关、服务/模型状态、协议、是否有 Key、收费状态、逐功能能力及禁用原因；模型端点仅管理员可见 |
| PUT `/settings` | 管理员保存 `enabled,baseUrl,apiKey,model,protocol,clearKey,reason`；SaaS 原因至少 6 字 |
| POST `/requests` | 使用 `Idempotency-Key` 提交，返回 202 和 `requestId,status,result?,errorCode?` |
| GET `/requests/{requestId}` | 查询当前用户/机构请求；重新校验关联任务权限，读取免费 |
| POST `/requests/{requestId}/cancel` | 明确取消；停止页面观察不取消任务 |

输入包含 `feature,taskId,reference,diagnosis,variant,calculator,clinicalContext,confirmedCharge`。clinicalContext 包含 diseaseId、inheritance、sampleType、variantOrigin、hpoTerms。变异评估必须关联有权限且未锁定解读的任务，沿用原有跨副本 advisory-lock 编辑保护。

- hpo → hpo_extract，POST `/v1/hpo/extract`。同步调用，禁止 local fallback，仅接受非降级的 execution_mode=model 结果。
- acmg/svcv4 → acmg_amp_2015/svc_v4，POST `/v1/variants/snv/evaluations`。
- cnv_gain/cnv_loss → cnv_gain_2020/cnv_loss_2020，POST `/v1/variants/cnv/evaluations`；显示坐标显式使用 1-based-inclusive，不静默减一。
- 参考基因组归一为 GRCh37/38；SNV 坐标、ref/alt、基因、转录本、HGVS、后果及 gnomAD AF 映射为上游字段。只发送选定位点，不发送 BAM、签名地址或样本编号。
- 白名单注释和计算器草稿作为未验证 curated_snapshot 来源材料，不接受客户端 verified、facts、score_applied 或人工审核身份。
- 变异返回 202 后记录上游 job_id，后台及前端每 2 秒分别查询上游和平台请求；上游幂等键使用平台请求 UUID。
- 保留 agent_proposal/rule_engine_result/reviewer_decision；classification 仅来自规则结果，不自动覆盖或保存人工判读。
- HPO 仅 present 自动追加，其他极性提示复核，再用现有术语库校验和去重。

模型协议支持 openai_responses/openai_chat。每次有效配置同步至新的 `/v1/providers/platform-{新UUID}`，输入及 profile 在平台事务中锁定。空 Key 保留，显式清除立即阻止新请求；无在途请求后后台删除旧 profile。同步或审计失败不发布配置。

## 持久化与积分

AutoMigrate 新建 ai_assistance_settings/settings_events/profiles/requests；配置 Key、变异输入和结构化结果以 AES-GCM 加密；HPO 原文只驻留本进程内存，落盘仅有带密钥的摘要及必要结构化结果，绑定设置/请求标识，原文不进入日志。请求唯一约束为机构范围、用户和幂等键摘要；同键不同输入返回 409。快照固定模型与上游材料，重试不改变时间戳或配置版本。

两名 worker 通过数据库租约领取请求。已知 job_id 可恢复查询，异步提交网络错误复用原键；HPO 中断且结果不确定时标为 uncertain，禁止自动重新调用模型。关闭页面仅停止观察，后台继续执行和结算。

HPO 队列最多 128 条内存输入，只由原进程执行；浏览器断开无影响，明确取消会终止 HTTP 调用。进程崩溃后原文无法恢复，7 分钟租约窗口后标为 uncertain，不自动重放。执行结束即释放原文。Unicode 跨度以 codepoint 标记传回，前端结合当前输入转换为 UTF-16，越界跨度丢弃。

请求绑定 upstream_id。托管进程必须配置 AI_ASSISTANCE_INSTANCE_ID，身份随唯一数据卷保留，不能用于另一个目录；外部服务用固定 URL 识别。其他副本不会接管不属于自己的请求，404 保持等待恢复，不标为失败。共享 PostgreSQL 扩容优先使用统一外部 SepiLens；托管配置目前是平台全局配置，不能作为每机构自动 provision 的接口。

平台在接收时锁定输入、不可复用的 profile UUID、服务版本和可探测的规则散列；提交/重放前检查版本变化，响应丢失后遇到版本变化或冲突标 uncertain，不换键重跑。RC2 没有按幂等键查 job 或按请求指定数据版本的 API，实际 profile/规则/数据锁定仍在上游接受 POST 时完成；结果保存其规则/数据散列。升级应先排空在途请求，不能承诺跨数据变更重放成功。

取消上游终态返回 409 时读取其持久化结果；已完成结果优先，点击取消不直接退款。缺少归属的旧在途 job 标为 uncertain 待人工核对。旧 HPO 原文密文在恢复/终态清理时删除，不用原文重跑；旧数据库和备份中的密文不会因升级自动消失。

结构化结果默认保留至终态后 30 天，之后清除输入/结果密文并标 expired；关联任务被删除时取消在途请求，终态结果联动清理。后台向原 SepiLens 实例重试 DELETE job，保留请求 ID、摘要、关联身份和账目作为幂等/对账墓碑。备份须设置不超过 30 天的滚动保留，定向删除须同步清除含相关数据的备份，恢复后先执行清理；本轮不操作线上备份。

当前用一个服务 admin principal（tenant=octopus-service）同步配置和执行请求。SepiLens 记录服务身份，真实用户、机构、关联任务和请求 UUID 由平台关联；机构隔离在平台权限层实施。不接受浏览器传入 tenant/role，也不声称上游拥有每机构 principal。

发布包原样打包并核对架构及归档/二进制散列；规则和批准材料只读挂载，由 SepiLens 校验与实际二进制、规则和数据的绑定。更换发布包、架构或重新编译后必须重新验证批准材料，不能复用其他二进制的批准结论。

**2026-10-25 HPO 生产计费调整**：SaaS 临床信息中的 HPO 候选提取每次预扣 1 积分，独立于 `AI_ASSISTANCE_BILLING_ENABLED`。服务端要求确认扣费；YiJian 操作前弹窗提示，可按当前用户、机构在 sessionStorage 中记住“本次浏览器会话不再提示”，后续请求仍携带扣费确认。恢复已保存的候选结果及本地术语匹配不调用模型、不收费。自部署模式不收平台积分；历史免费请求不会被重新计价。

收费通过共享密钥保护的 Squid `/api/v1/overlay/ai/reserve|settle|refund` 预扣 1 分并结算/退还。HPO 仅对非降级、真实模型生成的有效候选结果结算；失败、取消、降级、空建议和 uncertain 退还。其他正式判读仍受原收费开关控制，需同时满足 production、delivered、settlement_eligible=true 和有效模型建议；SVC 实验、insufficient_data 及 evidence_only 不启用收费。取消竞争以持久化终态为准。

HPO 升级顺序为 Squid → Octopus → YiJian（Cuttlefish 同步更新说明），无需新增数据库迁移或环境变量。新前端发现 HPO 计费未就绪时禁用该操作；新后端拒绝未确认扣费的新请求，避免升级期间无提示扣费或免费调用。其他 AI 的收费开关不变。

Squid 使用独立 ai_billing_requests 状态和原子幂等账本；billing_basis=ai_assistance，reference_id=ai:{请求ID}，related_task_id 单独关联任务。分析任务重试/失败退款不会包含 AI 费用。退款墓碑拒绝迟到预扣，后台恢复结算不依赖浏览器。

## 验证方式

- 两前端生产构建、相关 Go 包回归、轮询和管理路由测试。
- 设置专用临时 PostgreSQL 的 `AI_ASSISTANCE_TEST_DSN`，运行 Octopus `go test ./internal/assistance` 与 Squid `go test ./internal/service -run 'TestAIBilling|TestHPOBilling'`，用例创建并清理独立 schema。HPO 覆盖确认、重复请求、成功扣费、失败/降级/取消退款、扣费失败不调用模型和缓存读取；账务覆盖并发幂等与退款墓碑。
- Linux 交叉编译 `go test -c ./internal/assistance`，在组合镜像设置 `AI_RUNTIME_SMOKE=true` 执行 TestManagedSepiLensRuntime，验证实际二进制、能力门禁、目录锁、重启、退出及持久化。
- 本轮已在本地 amd64 组合镜像通过上述运行验证，并以缺失 SepiLens 二进制启动完整 Octopus，确认普通 `/health` 仍返回 healthy。临时 PostgreSQL 和验证容器已清理，未使用线上数据。
- 真实厂商模型联调、结算约定对齐和医学引擎批准继续由后续独立验收完成。

## CNV Gain/Loss AI

`AI_ASSISTANCE_CNV_BILLING_ENABLED` 默认 false，独立控制 Gain/Loss 每次 1 积分；与 Squid 同名开关和 SepiLens `SEPILENS_CNV_BILLING_ENABLED` 一起启用。正式规则批准开关不变。能力以 `ai_available`、`billing_available` 映射，不用正式规则 disabled 状态阻止候选请求。

CNV 使用现有权限校验后的任务/样本上下文，无需必填疾病和遗传模式。必须提供明确 coordinateSystem；BED 转一基闭区间，CN=0 保留；未知格式、负数、空区间和非整数拒绝。仅提交白名单变异信息，不将浏览器原报告分级、旧评分或 calculator 作为受信规则事实。

候选交付复用预扣/结算/退款和幂等生命周期；失败、取消及未具备资格的交付退款。人工保存的 section4.aiReviewed 使用独立来源和整数百分位分值，禁止与手工病例数叠加；后端重新计算，不相信浏览器总分与分类。criteria.aiReview 保存原建议、来源、请求标识和采用记录。无 schema 迁移。

CNV 评估快照允许最多 2 MiB，以容纳有界 AI 条款、引用及人工采用记录；超限仍拒绝，避免原 64 KiB 上限阻断合法 AI 结果保存。

深度报告中的非负有限小数 copyNumber 作为连续估算值保留在 ai_context.variant_annotations.copyNumber / copyNumberEstimate，不写入整数 cnv.copy_number、不四舍五入。整数（含 0）继续传入 copy_number；负数、非有限或超出安全整数范围的值拒绝。估算值不证明嵌合、胚系或临床适用性。
