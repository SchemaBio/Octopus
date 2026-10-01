# Parquet 判读链路复核与修复

日期：2026-10-01。范围：Octopus、内部 Parquet 查询服务、YiJian。未申请计算实例，未修改用户积分。

## 修复内容

- 自动 ACMG 初评升级为 `acmg-snv-points-v2`：仅对错义 SNP、明确单一 ENST 转录本及有限、合法的 AlphaMissense 数值计分。多值、缺失和适用性不明不自动计分；Python 与 SQL 筛选投影有一致性测试。
- 调整写入携带 attempt、数据集版本和预期调整版本，验证原始行确实属于当前数据集。首次写入使用冲突检测，后续写入使用版本比较；并发修改返回 409。
- 自动基线与人工证据、计算分类、人工覆写分开保存；支持恢复自动基线，保留追加式审计。JSON 调整及历史对外返回对象。
- 增加历史复核、回报与 CNV 评估迁移。只迁移能够证明所属 attempt 且身份唯一的记录，无法证明的保留待核对。
- 全列筛选、排序、统计及 CSV 导出使用叠加调整后的同一结果。未调整的复核、回报字段按 false 处理；支持自动初评分值及分类筛选。原始文件下载明确标注不含人工调整。
- IGV 使用 ESM API 并通过官方 removeBrowser 清理实例；轨迹授权请求可取消，覆盖迟到初始化和关闭清理。
- 修复 PostgreSQL 保留字 table 的 SQL 引用、COS HTTPS 归档引用解析及空列表请求。查询筛选改为原生 DuckDB SQL，消除精简容器缺少 numpy 时 Python UDF 注册失败。

## 真实归档恢复

任务：`73ac68fd-4f6e-437b-8244-d16b09bbc7e1`；attempt：`5298563b-0d4a-4a28-93c9-e10f86d3b2fe`。

原始 SNP/InDel 报告有 55,393 行，而原 Parquet 只有 54,961 行，少 432 行。不能以旧 Parquet 验收通过。受控命令从完整原始报告重建，保留原对象和原归档清单，另发 `results.parquet.catalog.json`。MEI 同样发现源报告与 Parquet 行数不符，已重建。CNV 与 ROH 从清单内原始报告补建。

新增源报告行数校验：准备数据集记录 expected_rows，查询、行身份验证与导出拒绝不完整文件。转换器不跳过字段数异常的行。原归档生成器为何丢行尚未逐行定位，不能将本次恢复视为原生成链路已修复。

| 类型 | 核验行数 |
|---|---:|
| SNP/InDel | 55,393 |
| CNV segment | 64,732 |
| CNV exon | 18,414 |
| STR | 37 |
| MEI | 34 |
| MT | 309 |
| ROH | 0 |

七类准备成功，context 为 ready。各类型历史调整迁移检查均为 0：本次没有可迁移的旧调整，不代表发生了非零迁移。三条 21 字符 AlphaMissense_AMC 原值已保留。SNP/InDel 新对象 SHA256：`e6c18f1181d4c3c55f1eed21eba464faff72d405cd75d5b16032c0d8c27cf7b5`。

严格条件下，本任务正分自动证据记录为 0；不得为产生致病分类而弱化转录本匹配要求或补造证据。

## 验证

- `go test ./...` 通过；Go 与远端 Docker 构建通过。
- Parquet 查询服务 13 项测试通过，覆盖自动初評、SQL 一致性、有效调整、筛选和导出。
- YiJian 8 文件、22 项测试通过，TypeScript 检查和远端生产构建通过。
- 真实 55,393 行缓存查询引擎基准：4 并发、16 请求，P95 0.1159 秒；验证 false 筛选完整计数及临时调整后的导出一致。此数值不包含 HTTP、鉴权或浏览器耗时，未改动真实用户调整。
- 浏览器停留登录页面：真实 BAM reads、Range/206、窄屏截图、授权过期交互尚未验收，不能以组件测试替代。

## 部署与回滚

三个服务均 healthy，部署镜像：

| 服务 | 标签 | image SHA256 |
|---|---|---|
| Octopus | schemabio/octopus:parquet-results-2 | 1f6114ced92d3f2884245cee21fcdc712260ad9f46e7b2acb67e38073cdf25ae |
| 查询服务 | schemabio/parquet-query:parquet-results-2 | df6fe50b5d3e0819cb0355cf0c4873d79507fe1ec2e34933221d617de7c03c78 |
| YiJian | schemabio/yijian:parquet-workspace-2 | 452b01c427ee8beaeb19682c4be5ac58aac87a399108dc689d17845ac27a4f8e |

远端备份及阶段日志：`/home/ubuntu/schema/backups/parquet-review-20261001/`。旧镜像保留 pre-review-20261001 标签。环境配置和备份凭据未输出，权限为 600。

受控恢复命令 `results-parquet` 默认只读，`--execute` 才准备数据；`--backfill --repair-table <类型>` 显式重建。命令不运行迁移、队列或工作流，也不修改计费。执行前需等待 Octopus 启动迁移完成并 healthy；本轮提前执行曾因 expected_rows 列尚未建立失败，健康后重跑成功。
