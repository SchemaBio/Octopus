# SaaS 新用户示例数据

本功能仅用于 SaaS。Octopus 只有在 `DEPLOYMENT_EDITION=saas` 时才迁移示例模板/发放表、注册示例接口、运行发放工作线程；自部署不注册这些接口，不运行该线程，模板命令直接拒绝。YiJian 只有连接 Squid 时才显示示例标签和发放提示。原有 WDL 流程模板不属于本功能。

## 审核与发放

Cuttlefish 审核时默认勾选发放，读取当前启用模板并将具体版本提交给 Squid。模板不可用时不能带发放选项通过审核，可以取消勾选后审核。旧客户端未提交该字段时不发放。

Squid 的审核事务原子保存账号状态、审核时间、选择及组织唯一的发放记录。审核记录和发放记录使用同一个时间 T0。后台等待组织存储初始化完成后，通过签名机器接口投递。Octopus 保存组织唯一记录和固定的样本、任务、attempt、资产标识；多次投递、复制失败和服务重启不会重新生成标识或移动 T0。

对象复制到目标组织自己的上传/任务路径，逐项验证 SHA256 和长度。复制期间不创建业务样本、资产或任务；全部对象就绪后，同一个数据库事务登记样本、配对、完成任务、QC/结构化结果、导入批次和 BAM 保留任务。Parquet 沿用浏览器结果引擎加载。示例使用独立 `example` executor，不启动云计算、节点或 AI，不生成积分流水。可使用仍有效的 FASTQ 创建普通新分析；不能重新启动示例任务本身。

## 生命周期

| 资源 | 不可变截止时间 |
| --- | --- |
| 样本、完成任务及结果 | T0 + 720 小时 |
| FASTQ、BAM 及 BAI | T0 + 168 小时 |

样本和任务以 T0 作为生命周期创建时间，实际复制/导入时间独立记录。修改、登录、导入重试及切换模板不延长截止时间。BAM 下载和 IGV 授权沿用现有期限裁剪；FASTQ 保持现有 SaaS 下载策略。

示例 FASTQ/BAM 删除所有对象版本及未完成分片；BAM 清理不删除其他结果或平台模板。30 天清理沿用现有任务销毁机制，包含结果、人工判读、AI 记录、下载、附件和缓存。未完成发放超过七天后清理部分副本并终止，不补发。手工删除后不自动重新发放。

## 发布模板

先部署 SaaS 后端，使示例表完成迁移，再在受控 Octopus 容器中运行：

```sh
/app/octopus example-template \
  -version demo-v1 \
  -sample <专用模板组织的样本UUID> \
  -task <匹配的已完成单样本任务UUID> \
  -activate
```

源样本必须有未过期的完整 FASTQ 配对、BAM/BAI 和完整结果。命令从源组织导出独立包，只复制发布需要的对象和样本字段；不复制账务、AI 请求、审核或操作历史。发布前校验 FASTQ read ID/记录数量/质量长度、BAM/BAI 签名和参考序列数量，以及 Parquet 结构。ZIP 等归档不进入模板，输出清单改用精确的可移植引用；不允许原组织对象地址或外部 URL 残留。未支持的格式或不明确的引用会阻止发布。

平台副本保存在 `platform/example-data/<版本>/`；发布后不依赖源组织的生命周期。版本不可覆盖，未来更新使用新版本。发布需足够的临时磁盘以校验源数据；临时文件仅由该命令使用并在结束后清理。命令不启动服务、调度、计费或自动迁移。

切换现有版本或停止新发放：

```sh
/app/octopus example-template -activate-version demo-v1
/app/octopus example-template -disable
```

这些操作不影响已经排队的发放及其期限。SaaS 用户没有平台模板读取权限，获得的是独立组织副本。模板存储权限应仅授予发布/发放服务。

## 部署与验收

Squid 需显式配置 `DEPLOYMENT_EDITION=saas`，并启用已有 Octopus 机器代理。Octopus 需配置 S3/COS、`DATA_RETENTION_DAYS=7`、`BAM_RETENTION_DAYS=7`、`BAM_CLEANUP_ENABLED=true`、`RETENTION_CLEANUP_ENABLED=true` 和已核实的 `RETENTION_BACKUP_POLICY_VERIFIED=true`，才能向新审核提供可用模板。不能用设置核实标记代替实际存储/备份策略核验。

发布顺序为 Octopus → Squid → Cuttlefish/YiJian，最后发布并启用经选定的模板。模板未启用时不自动给历史组织补发，管理员仍能取消示例选项正常审核。回滚先禁用新发放，保留已发放副本的清理能力。

测试包含 SaaS/自部署接口门禁、审核 BFF 和 CSRF、默认勾选及模板不可用、组织切换、文件内容及引用校验，以及独立 PostgreSQL/版本化 S3 的真实复制、幂等、暂停恢复、原子发布、BAM 历史版本清理和七天部分副本销毁。

集成测试只接受回环地址和名为 `example_test_local` 的专用测试数据库，不使用应用 `DB_DSN`。设置 `EXAMPLE_DATA_TEST_DSN` 和 `EXAMPLE_DATA_TEST_S3` 后运行 `go test ./internal/service -run TestExampleIntegration -count=1`；未设置时该集成测试跳过。Squid 同样使用 `EXAMPLE_DATA_TEST_DSN` 验证审核、发放意图和邮件失败的事务回滚。
