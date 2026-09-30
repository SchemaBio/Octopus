# 结果页 IGV 测序证据配置

结果工作台通过 Octopus 读取当前任务执行尝试的归档清单，并向已经获得任务权限的浏览器签发短时 COS 读取地址。归档路径、对象键和签名地址不会出现在结果上下文接口、页面 URL、日志或持久化浏览器缓存中。

## 接口

- `GET /v1/tasks/:id/results/context` 返回固定到当前 `executionAttemptId` 的导入状态、参考身份、成员、全量统计和按成员的 QC。
- `GET /v1/tasks/:id/results/igv` 只返回可用轨迹的身份、索引可用性和参考配置，不返回对象路径或签名地址。
- `POST /v1/tasks/:id/results/igv/urls` 接受当前会话 `version` 与已声明的 `trackIds`，签发读取地址。归档内容变化时返回冲突，客户端必须重新读取会话。

浏览器只能请求当前归档清单中声明的 BAM、VCF 或 BED 轨迹。BAM 必须有 BAI，VCF 必须有 tabix 索引；缺少索引的轨迹会标为不可用，不能退化为整文件下载。

## 参考资源

参考资源可以通过静态 FASTA/FAI 地址，或已鉴权的同源 Range 入口提供。两种方式均读取实际分析使用的参考。静态方式缺少任一地址时，结果页会显示参考不可用并关闭 reads 判读。支持的环境变量如下：

| 参考 | 必填 | 可选 |
| --- | --- | --- |
| hg19 | `IGV_HG19_FASTA_URL`、`IGV_HG19_FAI_URL` | `IGV_HG19_ALIAS_URL`、`IGV_HG19_CYTOBAND_URL`、`IGV_HG19_GENE_TRACK_URL`、`IGV_HG19_GENE_TRACK_INDEX_URL` |
| hg38 | `IGV_HG38_FASTA_URL`、`IGV_HG38_FAI_URL` | `IGV_HG38_ALIAS_URL`、`IGV_HG38_CYTOBAND_URL`、`IGV_HG38_GENE_TRACK_URL`、`IGV_HG38_GENE_TRACK_INDEX_URL` |

参考身份来自该次任务的输入快照，而不是页面默认值。`hg19`、`GRCh37`、`hs37` 映射到 hg19；`hg38`、`GRCh38` 映射到 hg38。未知身份不会猜测为任一版本。

`IGV_TRACK_URL_EXPIRE` 控制轨迹签名有效期，默认十分钟。前端在剩余不足一分钟时刷新当前轨迹地址。

参考桶没有浏览器 CORS 时，配置 `IGV_REFERENCE_PROXY_BASE_URL=https://yijian.schema-bio.com/api/v1/octopus`（自托管使用自身 `/api/v1` 入口）。参考 FASTA/FAI 将通过 `GET /tasks/:id/results/igv/reference/:asset?attempt=...` 读取；优先于静态 FASTA/FAI 地址。每次请求验证登录、租户、任务访问权限及当前 attempt。对象键由该执行的 hg19/hg38 身份选择，浏览器不能提交对象路径。FASTA 必须携带单个 Range，每次最多 8 MiB；FAI 最多 1 MiB。保留 `206`、`Content-Range`，拒绝对象存储忽略 Range 的响应，不下载整份 FASTA。浏览器与入口必须同源以携带现有登录 cookie。样本 BAM/VCF 仍使用短时 COS 授权。

同源入口复用 `CVM_REFERENCE_BUCKET` 及参考预检凭据选择：配置完整的 `CVM_REFERENCE_SECRET_ID/KEY` 时使用该对凭据，否则使用已有 COS/Tencent 凭据。无需为了配置同源入口复制密钥或开放参考桶的公开读权限。

## COS CORS 与 Range

为保存参考与任务归档的 COS bucket 配置仅允许 YiJian 站点 Origin 的 CORS 规则：

```text
AllowedOrigins: https://<yijian-origin>
AllowedMethods: GET, HEAD
AllowedHeaders: Range
ExposeHeaders: Accept-Ranges, Content-Length, Content-Range, Content-Type, ETag
```

对象存储必须对 Range 请求返回 `206 Partial Content` 与 `Content-Range`。发布前应使用一个脱敏 BAM/BAI、VCF/TBI 和目标 BED 在浏览器网络面板确认每个轨迹及索引均通过 HTTPS 的 Range 请求读取。

## 工作流归档契约

`single.wdl` 和 `trio.wdl` 的 `PipelineSummary` 现在输出 `vcf_raw_tbi`。`LeftAlignAndTrimVariants` 负责生成该索引。Trio 还输出 `members`，使 QC 与 BAM 轨迹能按先证者、父亲、母亲稳定排序。

历史任务缺少 VCF 索引、成员元数据或参考身份时，仍可使用已经导入的结果表；结果页会针对缺失证据显示原因，不会在请求期间临时生成大文件索引。
