# 冻结的结果引擎兼容契约

来源为 `parquet-query/server.py`、`svcv4.py` 与固定规则 `ef66faff51a265fef7b5c4e6439905f3aa540c46`。
开发期生成器位于 scripts/resultengine_oracle；schema/结果及 SHA-256 清单位于两个 Go 模块的 testdata。

内部 POST `/v1/query` 和 `/v1/export` 接收 table/filePath/datasetId/objectSha256/rowCount/rowId/offset/limit/search/sort/direction/filters/overlays。
query 返回 items、total、rowCount、offset、limit、columns、columnTypes、fieldProfileVersion。
原始行带 file_row_number、__ordinal、__row_id、__adjustments、__adjustment_version、__acmg；分页和排序不改变原始 ordinal。
export 返回 UTF-8 CSV，原始字段顺序、row_id、按字母排序的 effective overlay 字段；null 与空字符串转义以 golden 为准。
prepare 接收 SNP/InDel 来源身份，返回 assessmentFile/profile/rows；JSONL 记录为 rowId/profileVersion/assessment。
文件名为 datasetId-objectSha256-acmg-snv-points-v2.jsonl，临时文件完成后原子替换，既有文件可复用。

GET `/v1/svcv4/schema` 与 POST `/v1/svcv4/evaluate` 的完整结构由 schema.json、evaluate.json 冻结。
评分不得接受客户端总分，输出包含原始 inputs、疾病/MOI/GDV、确认、固定 pin/source、authoritative=false 与 score/classification/vusSubclass/state/breakdown/details/warnings。
九工作流及默认值、模型转换、额外字段拒绝、适配校验与具体 provenance 均按 oracle；不可把未提供当成有效零分。

旧服务限制：16 MiB body、4 槽、等待1秒、30秒查询、256 MB DuckDB memory limit、每查询单线程；limit 1..200、filters<=40、values<=1000、overlays<=100000。
Go HTTP 外层 SVC body<=250000 字节，Python inputs JSON<=200000 字符，cases<=100。
旧内部错误：404 not_found、413 request_too_large、429 query_capacity、400 query_failed/message（240字符）；公开错误映射保持现有 handler/service 契约。

数据路径必须为受控 cache 内的普通文件，hash 必须为64位小写hex；query datasetId 当前是字符串，prepare 要求64位hex。
哈希内容校验与原始报告行数完整性还由 ResultService 缓存/expectedRows 实施，不可因移除 HTTP 丢失。
自然染色体为数字、X=23、Y=24、M/MT=25、其它=1000；其它 contig 平局按原始行序。
数值多值排序取可解析子值最小值，missing 最后，两种方向都按 ordinal 升序打破平局。
contains 不区分大小写，equals/in 对 `&` 分项匹配；数值过滤任意子值命中；missing 为 null/空字符串/点号。
搜索按原始列与有效 ACMG 分类，不能擅自扩大为全部人工解读字段。

固定开发对照：623 上游函数向量、193 完整评分请求、65 query/prepare/export 向量。后续新增边界/非法输入应追加，不能修改旧期望以迎合 Go。
CSV 使用字节比较；JSON 数字使用数值精确比较（JSON 1 与 1.0 等价），不对评分使用 epsilon；对象键顺序不参与 JSON 对比，数组及依据字符串顺序参与。
