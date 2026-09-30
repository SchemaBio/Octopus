# 参考资源与 IGV 读取修复验证

验证日期：2026-10-01（Asia/Shanghai）。本轮没有运行工作流或申请计算实例。

## 参考预检纠正

之前的诊断客户端使用了 COS 不支持的 path-style 寻址，返回 403。该错误被误判为 Octopus 参考资源凭据权限不足。以虚拟主机寻址和服务器当前 Tencent 凭据重新验证，以下对象均返回 HEAD 200，大小大于零：

| 参考 | FASTA 字节数 | FAI 字节数 | 默认 BED 字节数 |
| --- | ---: | ---: | ---: |
| hg19 | 3,153,507,220 | 2,743 | 4,296,971 |
| hg38 | 3,151,425,851 | 6,406 | 13,225,759 |

Octopus 原有凭据回退与虚拟主机寻址即可工作，保留资源预检。没有复制、替换或生成长期密钥，不要求补配 `CVM_REFERENCE_SECRET_ID/KEY`。

## 浏览器兼容入口

参考桶的带 Origin GET 返回 206，但没有 Access-Control-Allow-Origin；OPTIONS 返回 403。当前凭据不能读取或修改该桶 CORS 规则。用户归档桶的 BAM Range/CORS 已有效，因此仅参考 FASTA/FAI 改走同源鉴权入口。

已部署 `IGV_REFERENCE_PROXY_BASE_URL=https://yijian.schema-bio.com/api/v1/octopus`。参考访问使用浏览器现有登录 cookie；每次验证任务权限及当前 attempt，不签发公开参考桶权限，不接受任意对象键。FASTA 必须使用单个、不超过 8 MiB 的 Range；FAI 不超过 1 MiB。样本轨迹继续使用十分钟 COS 签名。

Octopus 运行镜像：`schemabio/octopus:git-b1f9ef5`，容器健康。YiJian 不需要重建，现有 IGV 组件消费新参考 URL。

## 线上验证

通过真实 HTTPS YiJian 入口，使用仅在内存保存、短时有效的认证令牌执行只读验证：

- hg19 与 hg38 的 FAI 返回 200，字节数与 COS 对象一致。
- 两个 FASTA 的 `bytes=0-63` 请求返回 206、64 字节及正确 Content-Range；未传输整份参考。
- 未登录请求返回 401；没有 Range 的 FASTA 请求返回 416；旧 attempt 请求返回 409。
- 任务 `73ac68fd-4f6e-437b-8244-d16b09bbc7e1` 的上下文为 ready，参考及 IGV available 均为 true，三个轨迹可用。
- BAM/BAI、VCF/TBI、BED 的签名读取均返回 206，Origin 被正确允许。签名地址与用户文件内容没有写入报告。
- hg38 失败任务 `0261450c-c59e-4c7d-8a13-065f103c10f7` 的参考也可读取，仍保持原任务终态，没有触发重试或计费。

## 自动验证与边界

Octopus service、handler、config、router 测试通过，Docker 构建通过。新增覆盖 Range 上限、多段请求拒绝、完整 FASTA 拒绝、后端忽略 Range、执行隔离及未知参考拒绝。

本轮验收包含实际 HTTPS/COS Range 链路，不包含浏览器 IGV reads 画面截图。没有改动历史积分、用户任务、桶公开权限或 CORS 规则。
