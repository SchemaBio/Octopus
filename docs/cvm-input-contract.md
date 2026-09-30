# CVM 输入预检协议

Octopus 对 Squid 的 CVM 输入契约使用 `germline-v2`。每个 `CVMInputDownload` 必须在发出前经过对象 HEAD 检查，并携带：

- `expected_size_bytes`：上传完成后记录的 `DataAsset.FileSize`，必须为正数，并与 HEAD 的对象大小相同。
- `validation_role`：`fastq_r1`、`fastq_r2` 或 `bed`。
- `pair_key`：FASTQ 的配对标识。单样本使用 `single:0`；Trio 和基线任务使用成员索引；BED 留空。

Octopus 在每次首次投递和 Squid 请求竞价重试签名 URL 时，都对本次请求的全部输入执行 HEAD；全部检查通过后才生成签名 URL。对象不存在或不可访问返回 `INPUT_OBJECT_UNAVAILABLE`，大小不一致返回 `INPUT_OBJECT_SIZE_MISMATCH`。这些都是平台故障，不修改资产状态、不申请新的实例并全额退款。

参考资源在申请节点前也会 HEAD 检查：所选 hg19/hg38 的 FASTA 和 FAI，以及该工作流实际使用的默认 BED。`CVM_REFERENCE_BUCKET` 指向独立参考桶。可配置 `CVM_REFERENCE_SECRET_ID` 与 `CVM_REFERENCE_SECRET_KEY` 作为参考桶专用只读密钥；其策略应允许对 `database/hg19/*`、`database/hg38/*` 执行对象读取/HEAD。未配置时复用常规 COS 存储凭据。参考资源缺失、空对象或无读取权限均按 `REFERENCE_DATABASE_FAILED` 平台故障处理，在申请节点前终止并全额退款。

部署时先更新 Squid 以兼容 v1/v2，再更新 Octopus 发送 v2。URL 刷新请求带回原 attempt 的契约版本，因此切换期间 v1 竞价重试仍会收到 v1 形状。只有在所有旧 v1 排队和活动 attempt 结束后，才移除 Squid 的 v1 兼容；回滚必须同时恢复两个服务。
