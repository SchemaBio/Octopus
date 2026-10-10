# SNP/InDel 双版本评定

## 固定规则与计算

SVC v4.0 是草案／非权威参考实现，不能标为正式 2027 指南。当前规则固定为
`clingen-data-model/svcv4-model@ef66faff51a265fef7b5c4e6439905f3aa540c46`：
https://github.com/clingen-data-model/svcv4-model/blob/ef66faff51a265fef7b5c4e6439905f3aa540c46/docs/reference/scoring.md

源码、schema、样例和测试保存在 `parquet-query/vendor/svcv4`，保留 MIT LICENSE。
`svcv4.py` 调用原始评分、聚合、互斥、封顶和分类函数，不新增医学评分规则。
inframe_indel、non_coding 未提供评分实现，暂不支持。
返回 warnings 包含上游病例适用性、GDV 等假设以及各评分路径的 provenance，采用前须人工核验确认。
全部缺失返回未评定（score/classification 为 null）；有效零分仍是已评定，可为 VUS-low。

GET `/api/v1/tasks/:taskId/results/assessment/svcv4/schema`
和 POST `.../evaluate` 使用现有任务权限，通过 Octopus 内部 `internal/svcv4` 纯 Go 模块计算；规则提交及 JSON schema 不变。
保存时重新计算，忽略客户端提交的总分和分类；无需在线访问 ClinGen。

## 数据与采用

既有位点调整接口增加两个字段：

- `svcv4Assessment`：疾病、MOI、GDV、原始 inputs、服务端 result、revision/source、confirmed。
- `activeAcmgVersion`：`legacy` 或 `svcv4`，缺省为 legacy。

`assessmentVersion` 仍是原有自动初评上下文版本，与规则版本无关。
旧版证据、覆写和新评定独立存储。切换和证据修改要求理由，新版必须有有效分类并完成确认才能采用。
人工解读允许空理由、允许清空，仍复用原有 BeforeJSON/AfterJSON、操作人、时间审计。
并发版本、requestId 幂等、事务、任务完成只读限制保持原有机制。
旧数据无需转换或重建 Parquet；采用结果在查询、筛选、CSV、浏览器本地查询中动态投影。
VUS 子类放在 `acmgVusSubclass`，五分类仍为 VUS，兼容旧 VUS 筛选。

HistoryReport 增加当前/报出时版本与子类四个字段；旧空版本按 legacy 展示。
当前分类可更新，报出时分类/版本在再次取消并报出前保留原值。
新生成报告快照保存双版输入和所选 classification、activeAcmgVersion、acmgVusSubclass、acmgTrial。
已经生成的快照和报告不随后续切换修改。

## 部署与报告契约

构建并更新 YiJian、Octopus；结果准备和 SVC 评分由 Octopus Go 进程完成。Python requirements 及固定参考源码仅供开发期对照测试，不进入运行镜像。迁移说明见 `PARQUET_QUERY_GO_MIGRATION.md`。
无需新增服务器，Squid 现有 tasks 代理已覆盖新接口。Octopus AutoMigrate 自动增加历史索引字段。
CNV 仍使用已有 ACMG/ClinGen 体系。

采用新版的报出位点要求报告服务 contract 为 `report-snapshot-v2`。
legacy-v1 仅读取归档文件，无法读取所选评定，生成时会明确拒绝，避免输出错误分类。
v2 报告模板应读取 interpretation 内的上述字段，并印出所选版本、VUS 子类和“试行／非权威参考”标记。
未来升级应新增规则版本，保留旧记录和旧结果；不能直接替换本次固定源码或重算旧评定。

## 验证

`go test -p 1 ./...`；Python 使用 UTF-8 执行：
`PYTHONPATH=parquet-query:parquet-query/vendor/svcv4/src:parquet-query/vendor/svcv4 python -X utf8 -m pytest parquet-query/vendor/svcv4/tests parquet-query/test_server.py parquet-query/test_svcv4.py -q`。
包含固定提交的 495 项上游测试及集成测试，覆盖评分边界、缺失/零分、互斥、封顶、采用校验和 SQL/CSV 投影。
