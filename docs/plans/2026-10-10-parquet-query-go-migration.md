# parquet-query 全职责 Go 迁移实施计划

> 实施时使用 executing-plans 工作流按模块执行；本次只制定计划，不执行代码改写、对照生成、性能测试或服务器发布。

**Goal:** 最终仅由 Octopus Go 进程完成数据集校验、自动初评准备、查询、CSV 导出以及当前 SVC v4.0 schema/评分，运行时无 Python、DuckDB 或 CGO DuckDB 依赖。

**Architecture:** 新增 `internal/resultengine` 和 `internal/svcv4`，以固定 Python 实现作为开发期 oracle。先冻结兼容基准，再逐模块实现和比较，首次切换保留旧容器及显式回滚；业务验证通过后，在独立提交中移除旧客户端、配置与容器。

**Tech Stack:** Go、现有 xitongsys/parquet-go、标准库 JSON/CSV/文件 I/O；Python/Pydantic/DuckDB 仅用于开发期生成对照及运行上游测试。

## 0. 当前部署状态与实施约束

2026-10-10 前一轮部署已暂停：构建驱动及其 Docker build 客户端已停止，运行容器没有切换、没有清理镜像，旧服务健康。
远端源码已更新到 YiJian `b202ddd`、Octopus `cf91aca`、Sepiida `858e3e3`；Squid/Cuttlefish 未变。
远端旧工作区修改已保存为 stash，并备份到 `/home/ubuntu/schema/backups/svcv4-20261010T051418Z`。
此前部署流程已在 `.env` 写入三个待构建镜像标签；暂停后不再修改服务器。下次执行部署前必须比较当前配置、备份 `.env` 及 `old-images.json`，不能直接使用待构建标签启动 Compose。

本计划覆盖八类 Parquet 表：snv-indel、cnv-segment、cnv-exon、str、mei、mt、upd、roh。
CNV 数据查询和导出在范围内，CNV 医学评定体系不改。
YiJian 浏览器 DuckDB/WASM 计算保持现状；“去掉 DuckDB”只指服务器运行时。
公开 API、已有双版本评定、assessmentVersion 上下文、行 ID、COS 对象及历史报告不迁移、不重算。
阶段中发现旧实现问题应单独记录，不能借重构改变医学规则或偷偷修正对照结果。

## 1. 已确认的代码边界

| 职责 | 当前实现 | 目标实现 |
|---|---|---|
| schema、来源检查、筛选、排序、CSV | `parquet-query/server.py` | `internal/resultengine` |
| AlphaMissense 自动初评与 JSONL 准备 | 同上 `auto_acmg` / `prepare_automatic_acmg` | resultengine 准备模块 |
| SVC schema 与评分适配 | `parquet-query/svcv4.py` | `internal/svcv4` |
| 固定医学模型及评分函数 | `parquet-query/vendor/svcv4/src/svcv4_model` | Go 移植，Python 保留作 oracle |
| 查询、prepare、按行验证、导出 HTTP 客户端 | `internal/service/result_parquet_query.go` | 直接调用 Go 模块 |
| SVC HTTP 客户端与采用校验 | `internal/service/result_svcv4.go` | 直接调用 Go 模块，采用校验保持 |
| Parquet 现有读取器 | `internal/service/parquet_reader.go` | 抽取/复用独立 reader，避免 service 循环依赖 |
| 路径约束 | `internal/pathsafe` | 继续复用 |

当前 reader 从首行推导列定义，空表丢列，必须改为文件 schema。
当前 Python 查询按人工调整后的有效字段过滤/排序；原始行和调整信息随后由 ResultService 规范化。
不能在 Go 中改成“先筛选原始行，再应用调整”。

## 2. 分模块任务及提交

每项均先添加会失败的兼容测试，再实现最小模块、运行该模块测试，最后形成独立提交。
计划中的文件名为拟定位置；以第一项冻结的契约为准，不在本文虚构尚未验证的评分代码。

### Task 1 — 冻结契约、对照样例和旧性能基线

新增 `docs/PARQUET_QUERY_CONTRACT.md`、`scripts/resultengine_oracle/`、`internal/resultengine/testdata/`、`internal/svcv4/testdata/`。
保留原始 Python 源码、上游测试、MIT LICENSE 和 commit 来源。

- 逐项记录内部 `/v1/prepare`、`/v1/query`、`/v1/export`、`/v1/svcv4/schema`、`/v1/svcv4/evaluate` 请求/返回值和公开 API 的错误映射。
- 当前基线：请求体 16 MiB，分页最大 200，最多 40 个过滤项、每项最多 1000 个值、最多 100000 个 overlay；4 个工作槽、等待 1 秒、查询 30 秒、DuckDB 每请求 256 MB/单线程。prepare 当前全局串行。
- SVC 外层体积限制 250000 字节、inputs 序列化限制 200000 字符、最多 100 个独立病例；确认、修订 pin、重复病例/家系、有限数值等适配校验全部纳入。
- 记录 Python/Pydantic 的缺失/null/默认值、类型转换、额外字段、枚举与错误行为；不能只测试正常 Go 类型输入。
- 以固定规则 commit `ef66faff51a265fef7b5c4e6439905f3aa540c46` 生成输入/期望结果，含完整 details/provenance/warnings，不只保存分类。
- 将上游当前 495 项测试逐项映射到 Go 单元向量或适配层向量；新增所有分类/分支阈值的边界两侧用例。
- 保存当前 schema 响应为静态 JSON 兼容资源及 SHA-256 清单。确认其并非仅复制上游 schema 目录：响应还包含适配器的 workflow、枚举和 unsupported 清单。
- Parquet fixture 包含空表、不同 schema、编码/压缩、缺列、null/空字符串/点号、多值注释、坏 footer、行数不符、路径及哈希错误、旧版/新版 overlay、重复 overlay。
- CSV 保存字段顺序、转义、空值、换行、JSON 字段和排序样例。字节比较的可归一化差异必须列白名单，不得归一化丢失字段或行顺序。
- 性能 corpus 使用固定种子脱敏数据，保留分布、字符串长度、多值密度、row group/压缩结构及 overlay 密度；先在旧服务测量冷/热缓存、查询/导出/prepare、并发及普通 API 延迟。
- 输出 `docs/PARQUET_QUERY_BASELINE.md` 和机器可读 benchmark manifest，填写真实测量值后再确定验收上限，本步骤不预设 Go 性能结论。

验证：Python 上游/适配/服务测试；两次重新生成 golden 文件应一致；manifest 能确认 pin、文件哈希和生成环境。性能测量不写真实患者数据到 Git。
提交：`test: freeze parquet and SVC compatibility baselines`。

### Task 2 — 抽取读取器并验证格式兼容性

新增 `internal/resultengine/reader.go`、`schema.go`、`reader_test.go`；修改 `internal/service/parquet_reader.go` 为兼容包装。

- 从 footer/schema 提取列名、原始顺序与物理/逻辑类型，零行和 offset 越界仍返回字段。
- reader 提供按批读取原始 ordinal；每批/读取边界检查 context，准确返回取消或超时。
- 验证现有库对真实归档格式的字节列、UTF-8、数值/null、Snappy/ZSTD/GZIP、字典/数据页及大 row group 的支持。
- 测量库是否按列或 row group 全量解码；“批量 API”不能直接当成有界内存证明。大 row group 也须验收。
- 若库不能满足格式或内存要求，在该任务记录证据，再决定采用另一纯 Go reader；不得引入 CGO DuckDB。
- 原始数据不能通过 JSON float64 中转导致整数精度丢失；既有公开输出类型以 oracle 为准。

验证：`go test ./internal/resultengine ./internal/service -run Parquet`；格式/空表/取消及坏文件向量；基础 reader benchmark。
提交：`refactor: extract schema-aware streaming parquet reader`。

### Task 3 — 数据集身份、完整性和自动初评 prepare

新增 `internal/resultengine/dataset.go`、`identity.go`、`prepare.go`、相应测试。

- 文件必须是 cache root 内的普通文件，resolve 符号链接、禁止逃逸；接入现有缓存下载/hash 校验，避免每次查询重复完整哈希。
- 区分对象 hash、dataset ID、DataVersion、footer 行数和原始报告 expected rows；可信元数据变化后失效，防止缓存被替换。
- 保留 `SHA256(datasetId + '/' + objectSha256 + '/' + originalOrdinal)`；过滤、排序、分页均不能重编号。
- prepare 仅用于 SNP/InDel；保留 `acmg-snv-points-v2`、JSONL 行结构、UTF-8、文件名及复用行为。
- 保留 AlphaMissense 原有适用性、阈值、自动证据和 pending 说明；不得把新的浏览器初评算法混入旧 prepare 兼容实现。
- 临时文件同目录写入，按数据集加锁、唯一临时名，成功后原子发布；错误/取消清理。不能让同数据集并发生成破坏已有结果。
- 继续满足已有单文件 512 MiB、单行扫描上限等 service 消费限制；新安全限制与旧正常请求的关系须明确测试。

验证：prepare JSONL 对照、重入/并发/取消、不完整报告、路径攻击、hash 变化及行 ID 一致。
提交：`feat: validate result datasets and prepare assessments in Go`。

### Task 4 — 有效字段投影和筛选

新增 `internal/resultengine/projection.go`、`filter.go`、`query.go` 及对照测试。

- 统一 effective projection，先叠加 overlay；自动/人工/覆写/所选 SVC 的优先级严格对照。
- 保留 legacy 缺省、两版独立证据、acmgTrial、VUS 子类及 VUS 五分类兼容。
- 表类型、列白名单、搜索范围、字段类型/profile 和不支持操作的错误保持兼容。
- 原样实现 `&` 多值 any-match、contains 大小写语义、equals/in、数值比较/between、missing/not-missing。
- 显式测试 bool/null 转字符串、非法数值、重复 overlay 主键及缺失字段；不要用通用 Go 格式化替代 DuckDB/Python 字符串语义。
- 单次扫描汇总 total 并保留需要的行；普通无排序分页只保留当前页，仍扫描以得到准确 total。
- service 当前从数据库全量加载 overlays，也纳入内存预算；必要时用流式/分批或临时索引，保持读取到同一调整修订的语义。

验证：八类表的 total、columns/types、items、ordinal/row ID、adjustment version 和公开规范化输出逐项对照。
提交：`feat: project and filter effective result rows in Go`。

### Task 5 — 有界排序、分页、CSV 导出和并发隔离

新增 `internal/resultengine/sort.go`、`spill.go`、`export.go`、`scheduler.go` 和性能/清理测试。

- 排序键：自然染色体、数值多值最小项、缺失最后、请求方向，最终以原始 ordinal 升序稳定打破平局；不新增 contig 字典序规则。
- 小页面可按内存预算选 bounded top-k，但大 offset 不得无限扩大堆；切换为分块排序+外部归并。
- 导出重用同一 projection/filter/comparator，逐行 CSV；文件头顺序和原始/有效重复列行为按旧实现冻结，不自行去重。
- tempfile 放独立受控目录，限定总字节、文件数、归并打开句柄数；取消、错误和完成立即清理，启动时只回收本模块遗留且无活跃所有者的文件。
- 大文件导出不由 `io.ReadAll` 或全量 rows 中转；遵循下载流错误和断连取消语义。
- 重查询/导出/prepare 有独立槽和内存/临时盘预算，等待期间可取消；不锁住普通 Octopus API。SVC 评分采用单独轻量预算。
- 上限由 Task 1 基线决定并写入验收表；测试限流、排队、超时和资源耗尽，不静默回退到 Python 掩盖失败。

验证：顺序/分页/CSV 对照；大 offset、巨型行、并发导出、取消/磁盘不足/读写错误；峰值 RSS 与临时文件清零。
提交：`feat: bound result sorting and CSV export resources`。

### Task 6 — SVC 类型、schema、计分原语和分类

新增 `internal/svcv4/types.go`、`decode.go`、`schema.go`、`schema.json`、`primitives.go`、`classification.go` 及 golden 测试。

- 规则 pin 不变；可增加实现标识，但避免改变 existing JSON schema 或把实现版本当规则版本。
- 以三态 optional 类型表达未提供/null/value，零分不得用零值推断成缺失；按 oracle 实现具体字段 null 与缺省默认值。
- `json.RawMessage`/UseNumber 等用于保持输入信息；显式复现 Pydantic 接受/拒绝和转换，不因 Go 严格类型缩小正常兼容范围。
- 非有限数、非法枚举、额外字段、大小限制按固定适配行为校验；保存时仍忽略提交的结果及“authoritative=true”。
- 静态 schema 使用 `go:embed`，GET 返回兼容对象，不在运行时调用 Python 或生成不同表单。
- 移植 result、points/multiplier/router 等原语和分类/VUS 边界；浮点运算顺序、舍入和指数公式对照原实现。
- 分类及缺失状态严格相等；不得以 epsilon 让阈值两侧归为同类。中间浮点差异须定位解决，并形成说明后才允许进入下一门槛。

验证：schema 全结构、输入校验、原语、边界 golden；禁止依赖 Python 的 Go-only 测试也必须通过。
提交：`feat: add pinned SVC types schema and scoring primitives`。

### Task 7 — 九类变异影响路径

新增 `internal/svcv4/impact/`，依赖顺序按上游 `_common`、`_spl_common` → 独立路径 → missense 聚合。
完整移植 missense、nonsense、frameshift、canonical_splice、intronic_synonymous、exon_deletion、exon_duplication、start_lost、stop_lost。
missense 包括 amino-acid、splice、compare 等已实现分支，不仅移植入口函数。
保留 route、子项、GDV 影响、封顶/互斥、provenance 和全部已知假设说明。
inframe_indel、non_coding 继续拒绝为暂不支持，不新增评分表。

验证：各模块对应上游用例和适配 golden，逐项核对 details 和缺失/零分。
提交可按无义/移码/剪接、起止密码子/外显子、missense 分为三个独立提交。

### Task 8 — 人群、临床病例、病例对照、家系及聚合

新增 `internal/svcv4/population.go`、`clinical.go`、`case_control.go`、`locus.go`、`aggregate.go`、`evaluate.go` 和测试。
按 POP → 单先证者 CLN → 病例聚合/病例对照 finalize → LOC_PHE/LOC_SEG → 总分类顺序移植。
保留 POP_FRQ gating、CLN 互斥、家系封顶、重复病例/家系适配校验和 upstream 所有默认值。
原始 inputs、disease/moi/GDV、confirmed、revision/source、authoritative=false 和结果结构保持兼容。
确认状态只能由请求的明确 true 保留；评定缺失仍不能采用，不改写存储中的旧确认状态。

验证：adapter 全请求 golden、上下游模块组合、所有封顶/互斥、非整数和缺失情况；Go 与 Python 差异为零。
提交：`feat: complete pinned SVC clinical family and aggregate scoring`。

### Task 9 — 接入 ResultService，保留迁移期开关

修改 `internal/service/result_parquet_query.go`、`result_svcv4.go`、`internal/config/config.go` 及初始化/相关测试。
新增轻量 engine 接口和 legacy adapter，迁移阶段分别明确选择 result engine 与 SVC engine，默认旧实现。

- prepare/query/row-verification/export/schema/evaluate 全路径改用同一选择层，不能漏掉只读/报告辅助路径。
- 一次请求固定实现，不在错误时自动切换；shadow 对照不重复写文件、不写数据库、不改变用户确认。
- 保存仍服务端重算，并使用既有任务/数据集锁、版本冲突、mutation id、请求 fingerprint、理由、审计和事务。
- 列表/筛选/CSV/报告只读取所选版本，已有评定和规则 pin 保留。报告 v2 契约约束及历史快照不变。
- 浏览器授权/参考证据/自动初评路径保持原样；避免把 browserLocalAssessment 跳过准备的机制误改为服务端重算。

验证：service/handler/router 全套；显式覆盖只读、并发、重复请求、空理由人工解读、两版互不覆盖、切换采用、历史快照。
提交：`refactor: route result and SVC operations through internal engines`。

### Task 10 — 全链路对照与性能验收

更新 `docs/PARQUET_QUERY_BASELINE.md`、新增 `docs/PARQUET_QUERY_GO_ACCEPTANCE.md`。

- golden 评分分类/VUS/缺失/确认/schema 无差异，分项、聚合、封顶和依据可逐项追溯。
- 查询 total、offset/limit、完整分页 row ID 与顺序、导出字段和值无差异。
- 对照范围不仅算法模块，也包含 ResultService 规范化与报告快照字段。
- 在相同资源、同数据、同缓存条件下记录 Go/Python 耗时、RSS、分配、CPU、临时盘、并发及普通 API p50/p95/p99。
- 测试大于内存数据、宽列长字符串、10万 overlay、大 offset、取消、任务并发和外部排序失败清理。
- 填写基线阶段约定的数值上限，逐项列通过/失败；不以平均耗时掩盖尾延迟、OOM 或普通 API 退化。
- Go-only 环境无 Python/DuckDB 服务也必须通过。YiJian 默认不修改，只有兼容测试证实必要时单独提交最小调整。

验证命令：`go test ./...`、`go test -race ./internal/resultengine/... ./internal/svcv4/... ./internal/service`；新模块 `go test -bench . -benchmem`；Python 固定测试及 oracle 差异检查；YiJian 类型检查和现有 Vitest。
提交：`test: verify Go engine parity and resource budgets`。

### Task 11 — 首次 Go 发布，保留回滚窗口

新增 `docs/PARQUET_QUERY_GO_MIGRATION.md`；调整迁移期 Compose override 与目录权限。

- 发布前核对暂停部署遗留标签，保存源码/配置/镜像 ID；镜像以新 Go migration 提交标记。
- Octopus 取得 assessments 目录写权限，验证现有文件读权限；缓存/COS 不重建。临时排序目录单独配额。
- 双 engine 都通过 Task 10 后才显式选 Go，旧 Python 容器及旧镜像暂保留；只重建需要的服务。
- 验证真实任务浏览、筛选、导出、schema、评分预览、普通 API；禁止为验收修改既有临床评定或历史报告。
- 写明退回 legacy 两个开关的具体操作、健康检查和原镜像/配置；回滚不触碰评定数据。
- 观察窗口及请求量阈值在发布前记录，可建议至少覆盖一次实际业务高峰，但不能宣称时间经过等于业务验收通过。

提交：`deploy: switch Octopus to Go engines with explicit rollback`。

### Task 12 — 移除旧运行依赖和废弃镜像

仅在首次发布业务验收完成后执行，单独提交，不能与首次切换合并。
删除 legacy HTTP adapter、迁移开关和 `PARQUET_QUERY_URL`/ServiceURL；保留 reference/cache/assessment 本地目录配置。
修改 `deploy/saas-parquet-query.override.yaml` 为 Go-only 结果目录配置：删除 parquet-query 服务、专用网络和 depends_on，保留必要目录权限初始化，避免删除 Octopus 的 sepiida/default 网络。
移除 Python 运行镜像构建入口；Python 源码/测试继续作为开发工具保存，镜像构建不得 COPY 或启动它们。
更新部署/开发/回滚文档与健康检查，验证 `CGO_ENABLED=0` 构建，Octopus 单容器无需 Python 服务能完成全部职责。
旧 URL 配置若残留应给出明确迁移诊断，不能默默继续调用远端旧服务。
清理镜像仅针对已识别的 SaaS 废弃镜像，排除运行/停止容器使用的镜像和必要回滚镜像；不执行 volumes/system 全局 prune。

提交：`chore: remove Python query runtime and document Go-only deployment`。

## 3. 最终交付清单与切换门槛

交付模块化 commits、规则/样例 manifest、契约文档、差异结果、真实性能结果、资源配置上限、迁移/回滚操作和最终 Go-only Compose 验收记录。
任何评分、查询顺序、数据身份或历史快照差异未解决时，维持旧实现；不能以“主要路径通过”宣布完成。
最终验证镜像、进程、依赖和配置四个层面：不存在运行时 Python/DuckDB 调用、外部评分 HTTP、旧 query 容器依赖，也没有重新解释旧评定。
