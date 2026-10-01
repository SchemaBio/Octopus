# SNV/Indel 注释与页面字段契约

任务列表数据来自 `result_snv_indels` PostgreSQL 表，导入源是该 attempt 归档中的 `*.snv_indel.txt`（TSV）。Parquet 用于归档、分析和导出；分页结果接口没有读取 Parquet。

当前 single/trio 的 `tasks/vep.wdl` 使用 VEP 116、merged cache；`scripts/vep_report.py` 选取并输出注释。已配置来源：

| 来源 | 流程输出 | 页面 |
| --- | --- | --- |
| ClinVar（默认 20260415，可由输入覆盖） | Sig、RevStat、DN、Star | 临床意义、审核状态、疾病和星级 |
| gnomAD v4.1 joint | AF、AF_EAS、nhomalt_XX/XY | 总体/东亚 AF、纯合计数 |
| Pangolin | gain、loss、AN | 剪接分数和流程预测标签 |
| EVOScore2 | score、AN | 分数和流程预测标签 |
| AlphaMissense v3 | AM、AMC | 分数及标签，保留多值 |
| GenCC | 疾病、遗传方式 CURIE/名称、疾病标识、证据链接 | 疾病和遗传方式 |
| VEP cache、cytoBand | 转录本、后果、HGVS、dbSNP、MAX_AF、HGNC、cytoband | 基本信息和标识 |

VEP 命令还启用 FlankingSequence、MissenseZscoreTranscript，但当前报告没有输出这两项，也没有 SIFT、PolyPhen-2、CADD、REVEL、SpliceAI、独立 ExAC 字段；页面不提供这些空占位。版本描述来自当前工作流配置，不能替代历史执行的资源版本证明。

当前 SNV 报告没有 ACMG 分级或证据列。`ClinVar_Sig`、`EVOScore_AN`、`AlphaMissense_AMC` 不作为 ACMG 自动评定。缺少评定时显示“未评定”；已有人工 ACMG 数据保留。

人群 AF 显示原始 0–1 比值，不乘 100；真实零和缺失分开。样本 VAF 单独标识，可以百分比显示。MAX_AF 来自 VEP 缓存的最大 AF，不标成独立 ExAC 频率。

`annotationValues` 保存报告实际注释原值，包含 `&` 分隔多值；兼容的数值投影字段不代替原值。`Pangolin_AN`、`EVOScore_AN` 为文本标签，数据库列迁移为 text。

历史数据使用受控命令：

```sh
octopus results-import --task UUID --attempt ATTEMPT --annotations-only
octopus results-import --task UUID --attempt ATTEMPT --annotations-only --execute
```

默认检查；执行时锁定已完成、导入成功的当前 attempt，逐一核对报告与结果的身份和数量。仅更新原值与两个标签列，保留 ID、review、report、ACMG、积分和终态。事务记录 annotation_recovery 审计，更新结果版本；重复执行没有变更时不新增审计。身份模糊或不匹配时拒绝修复，不整批重新导入。
