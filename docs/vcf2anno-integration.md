# VCF2Anno 接入与升级

VCF 注释提供 hg19/hg38 两个内置流程（builtin-vcf2anno-hg19 / builtin-vcf2anno-hg38），基础类型 vcf_annotation，模板 vcf2anno，WDL 工作流 VCF2Anno。组织可基于任一内置入口创建流程；不需要 BED、CNV 基线、FASTQ 或家系。同一份 vcf2anno.wdl 由 Octopus 按组装注入参考资源，不维护两套输入 JSON。

## 用户操作与接口

1. 在数据中心上传 .vcf、.vcf.gz 或 .vcf.bgz。自部署可通过本地导入登记 VCF；启用扫描的部署也识别 VCF，沿用扫描的属主和稳定时间限制，不参加 FASTQ 自动匹配。
2. 新建任务选择业务样本、流程和 VCF 资产。文件必须已上传完成、未过期且属于当前租户/可访问范围。
3. 单样本 VCF 自动选择；多样本填写 VCF 头中的准确样本名，每次仅处理一个样本。名称不从业务编号推断，不给样本建立长期 VCF 绑定。
4. 在结果页进行 SNP/InDel 解读、关注、报告及下载；显示预处理和性别推算信息。没有 FASTQ QC、BAM 或其他变异类型，不展示这些模块。输入无有效变异时仍正常完成。

创建 POST /api/v1/tasks、预估 POST /api/v1/tasks/estimate 增加 vcf_asset_id 和可选 vcf_sample_name，业务样本沿用 sampleId、流程沿用 pipelineId。路径由服务端解析，不接受浏览器用 WDL 路径替代资产 ID。批量模板新增可选 VCF数据ID、VCF内样本名；旧版模板仍可用于 WES。

上传使用 file_type=vcf，且恰好一个 read_type=vcf 文件。本地导入 POST /api/v1/data/local-import 使用 internal_id、vcf_path，不能混用 read1_path/read2_path；FASTQ 原有双端导入保持完整校验。VCF 上传复用限额、分片、续传、确认和保留期。活动任务对 VCF 的引用阻止用户删除。

任务详情新增 result_capabilities；结果上下文新增 capabilities 与 vcfAnnotation（preprocessing、sex_inference、summary），缺失 QC 不表示结果损坏。VCF 正规化文件及其索引可作为 IGV VCF 轨道，不制造 BAM 证据。

## 管理员参数

/api/v1/admin/workflow-configs/vcf2anno/{hg19|hg38} 复用现有管理员权限和审计。参数：
- sex_min_sites：默认 100，正整数。
- female_max_ratio：默认 0.5。
- male_min_ratio：默认 0.7。
- 要求 0 ≤ female_max_ratio < male_min_ratio ≤ 1；0 的女性阈值可正确保存。

配置按模板和组装存储，并在新执行时快照到输入；VCF 性别推算不覆盖业务样本性别。参考目录仍由后端 catalog 管理，资源须匹配 GRCh37 或 GRCh38，不执行坐标转换。

## 执行、归档与计费

本地、Slurm、LSF 共用资产准备；CVM 的 germline-v2 契约新增 vcf 角色，要求一个 VCF，不要求 FASTQ 配对。对象归属、下载大小、刷新签名地址和故障诊断继续生效。云端校验普通/gzip/BGZF VCF 的头、样本选择和 GT；规范化、REF 校验等由工作流脚本完成。

输出包括正规化 VCF/TBI、VEP VCF/TBI、SNP/InDel 报表、预处理 JSON、性别 JSON/TXT 和 summary JSON。归档读取兼容独立 JSON 文件及 Sepiida 展开的内嵌对象；下载时从内嵌元数据重建三个 JSON，并保留 Agent 转换 Parquet 后仍归档的原始报表与文字说明。自部署重建文件保存在结果缓存，SaaS 重建对象保存在当前 attempt 的归档前缀，随该归档保留策略清理。下载包不报告缺少未运行的 WES 模块。空记录报表可以导入，不创建虚构 QC。

SaaS 沿用 WES 每分钟费率、租户倍率、最低余额和 attempt 级预扣/结算/退款规则。初期预估统一 60 分钟，最终按 WES 现有计算时间窗口结算，包括现有 CVM 初始化计时政策。VCF 不走 CNV 按 GiB 收费；下载费不变。自部署不扣积分。

## 升级顺序与验证

1. 先准备包含 schema-germline 新版本的执行环境：vcf2anno.wdl、tasks/vcf2anno.wdl、更新的 tasks/germline.wdl；germline 镜像必须包含 initialize_vcf.py、infer_vcf_sex.py、更新的 vep_report.py，调用固定目录 /opt/schema-germline/scripts/，不用 File 输入脚本。VEP 116 及对应两组参考库须已准备。
2. 升级 Squid，使其接受新模板和 VCF 校验角色；配置 CVM 镜像/工作流契约支持 germline-v2。
3. 升级 Octopus，启动时 AutoMigrate 在 workflow_threshold_configs 增加 sex_min_sites、female_max_ratio、male_min_ratio。数据类型和流程基础类型为字符串扩展，不需要改写历史任务。既有 WES 参数和任务输入快照不改变。
4. 升级 YiJian、Cuttlefish；先核对模板、镜像和资源可用，再开放新流程。回退前先停止创建新类型任务，并等待现有 VCF 任务结束。

测试：
- go test ./internal/workflow ./internal/model ./internal/service ./internal/handler
- 设置指向隔离 PostgreSQL 的 EXECUTION_TEST_DSN 后执行 go test ./internal/service -run TestVCFLifecycle -count=1，覆盖两个组装和组织流程的预估、创建、引用保护、归档导入、空结果重导入、无 QC 解读上下文及业务性别不变。
- SNP/InDel 流程的初始化、性别和报表已在 schema-germline 单独验证；平台测试采用合成归档，不替代真实 VEP 注释。
- 当前开发环境缺少真实 hg19/hg38 VEP cache/完整注释资源，未执行真实参考库端到端注释，也未进行线上发布。
