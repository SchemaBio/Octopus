# 浏览器 Parquet 查询与判读同步

实施日期：2026-10-01。

## 数据与计算边界

- COS Parquet 为不可变原始结果；浏览器通过任务权限校验后的短时授权读取文件，以 SHA-256 校验内容。
- DuckDB-Wasm 1.32.0 在自托管 Worker 中完成筛选、排序、分页、统计、有效结果 CSV 导出。普通表格查询不调用服务端 `/query`。
- 服务端保留首次数据集验证、自动评估基线准备、授权、调整持久化及个人方案存储。既有 DuckDB 查询服务仍用于准备数据和用户明确选择的兼容模式。
- 当前读取采用完整 GET，单文件上限 128 MiB，Worker 内存配置 512 MiB；超限或设备无法运行时提供明确的服务端兼容入口，不静默切换。
- COS 的 GET 签名对 HEAD 返回 403，Range GET 为 206；本次使用完整 GET 避免 DuckDB 的 HEAD 探测。实际 Origin 测试确认允许 YiJian 域名。浏览器缓存仅在内存，不写 IndexedDB、OPFS 或签名 URL 持久缓存。

## 筛选和调整的同步

个人筛选与协作判读分开保存：

1. 筛选即时在浏览器执行；用户显式保存命名方案，服务端以租户、任务、用户及表隔离。方案跨设备可读取，通过“应用方案”恢复；不会自动覆盖另一位用户的筛选。
2. 复核、回报、ACMG、CNV 评估保存为数据库调整快照和追加审计。保存带 attempt、数据版本、原始行序号、预期调整版本及 mutation UUID。
3. 行身份取数据集 ID、对象 SHA-256 与原始行序号的 SHA-256，不能用筛选后的页内序号。
4. 服务端事务锁定当前 task 和 dataset；重复 mutation 重放不重复记录，内容不同或版本冲突返回 409。节点重跑/数据集替换后拒绝旧版本修改。
5. 浏览器首次拉取调整快照，随后约每 10 秒按 revision 拉取增量，先应用调整再筛选和排序。每次有效调整清除计数与分页缓存，导出前再次同步。
6. CNV 表直接读取查询中的评估覆盖层，翻页不逐行请求服务端。少量审计历史操作和服务端保存校验仍保留。

接口：

- `GET /v1/tasks/:id/results/tables/:table/browser`
- `GET /v1/tasks/:id/results/tables/:table/adjustments?attemptId=…&datasetVersion=…&since=…`
- `GET/PUT /v1/tasks/:id/results/tables/:table/views`
- 既有行调整写入与历史接口保留兼容性。

迁移由现有 AutoMigrate 增加个人方案表、数据集 revision、调整 revision 和 mutation 审计字段；无原始结果删除。JSON 方案使用字段白名单，不接收 SQL。权限沿用现有租户、任务读取/判读授权。

## 验收证据

实际数据来自任务 `73ac68fd-4f6e-437b-8244-d16b09bbc7e1`，attempt `5298563b-0d4a-4a28-93c9-e10f86d3b2fe`。

- SNP/InDel 原始记录 55,393 条，文件 SHA-256：`e6c18f1181d4c3c55f1eed21eba464faff72d405cd75d5b16032c0d8c27cf7b5`。
- 使用相同源码 Worker、真实 Parquet 和自动评估基线的本地浏览器夹具成功查询、筛选和导出；表格 `/query` 请求为 0。
- 四并发、共 16 次重复查询，缓存就绪后 P95 约 692 ms。它是同一环境的重复查询缓存指标，不代表任意冷查询、低端设备或公网总延迟；优化前同类测试约 2,798 ms。
- 本地模拟首行 `reviewed=true` 增量后，“未复核”数量从 55,393 变为 55,392，旧缓存失效。模拟仅发生于本地夹具，没有修改生产判读记录。
- 当前任务的严格自动证据基线不产生可计分证据，未伪造致病分类。
- Octopus `go test ./...` 全部通过；YiJian 29 项测试和 TypeScript 检查通过；生产容器构建通过。
- 覆盖 SQL 字段限制、原始行身份、旧 attempt 拒绝、revision 锁及游标、个人方案反复恢复、保存冲突、浏览器导出和 CNV 无逐行请求。

验证边界：浏览器使用授权下载的本地文件副本验证真实 Worker，并独立验证 COS 实际 Origin；未取得用户登录会话，因此未声称完成已登录生产页面的跨设备协作点击验收。后续应以两个正常用户会话验证修改同步、409 编辑冲突、个人方案隔离及设备内存边界。

## 发布与回滚

远端：`/home/ubuntu/schema/Octopus`、`/home/ubuntu/schema/YiJian`。

部署镜像为 `schemabio/octopus:browser-results-3` 和 `schemabio/yijian:browser-workspace-3`；查询服务保留 `schemabio/parquet-query:parquet-results-2`。最终镜像摘要及健康核验见本文件后续记录。

保留 `pre-browser-20261001` 镜像和私有配置备份。回滚前端可以恢复旧查询方式，新增调整和个人方案数据不删除。发布不申请实例、不重跑工作流、不修改积分。

临时诊断授权文件、下载副本和临时 HTTP 服务器在验收后清理；正式自动评估基线及归档结果保留。

最终发布核验：两个服务均为 healthy；容器内 WASM 请求返回 `200 application/wasm`。

- Octopus：`sha256:3bfd8ff6363d903e08eea27800b30eebc6d4933c3b139d2826139c0518132e3a`
- YiJian：`sha256:bb375aac0c5bad82b3a64c757281f72fade4c3028dafabcb2b4b0aab714adae9`

本地夹具服务器已停止，两个临时夹具目录及远端诊断授权文件已删除。
