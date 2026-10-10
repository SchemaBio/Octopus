# 浏览器 Parquet 查询与判读同步

实施日期：2026-10-01。

## 数据与计算边界

- COS Parquet 为不可变原始结果；浏览器通过任务权限校验后的短时授权读取文件，以 SHA-256 校验内容。
- DuckDB-Wasm 1.32.0 在自托管 Worker 中完成筛选、排序、分页、统计、有效结果 CSV 导出。普通表格查询不调用服务端 `/query`。
- 服务端保留首次数据集验证、自动评估基线准备、授权、调整持久化及个人方案存储。2026-10-10 起由 Octopus 内部纯 Go 模块负责准备和服务端兼容查询，不再依赖 Python/DuckDB 容器。浏览器 DuckDB-Wasm 方式不变。
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

## 2026-10-01 后续：唯一浏览器计算与加载优化

本节覆盖前文“用户选择兼容模式”的设计：YiJian 已移除模式选择、服务端表格查询和导出分支。服务器查询服务仅保留受控数据准备及既有接口兼容，页面没有切换或静默回退。

- 任务/样本信息固定于顶部，结果区独立滚动。窄屏限定顶部区高度，长样本信息可在固定区域内滚动。
- SNP、CNV、CNV 评估和 MT 详情改为底部展开；表格留出下方面板空间，仍可切换候选变异。
- 加载原先串行经过 Worker 启动、Parquet 下载、18.22MB 自动评估 JSONL 下载、结构推断、初始化。改为独立下载与 Worker 启动并行，自动基线发布 gzip，使用明确 JSON 读取结构。
- 实际基线 18,224,297 bytes 压缩至 2,058,296 bytes，减少约 89%；原始 Parquet 8,544,780 bytes 保持不变。基线以独立版本对象保存，不改写原始报告。
- 实际 COS 下载测试：Parquet 约 646ms、压缩基线约 426ms。这是本次所在网络的单次测量。
- 相同源码、本地真实文件副本的浏览器冷启动首屏 3,853ms：Worker 1,802ms，Parquet 展开 1,030ms，基线解析 910ms，调整同步 16ms。该数值不包含真实公网及登录接口耗时，不能声称所有用户生产页面都在此时间内完成。
- 新增脱敏阶段计时事件（授权、下载、Worker、解析、同步及总时间），不包含签名 URL、注释内容或凭据。
- 前端 30 项测试、类型检查和后端完整测试通过。首次进入仍有 WASM 启动及数据展开成本；已加载表继续复用浏览器内存缓存。

后续发布镜像：Octopus `sha256:ab72e0128fa474be1f1a43fda454cea9c3038826404dfbe21ae9dd2b664a5cf7`；YiJian `sha256:098dcccbc5385eb4d19f3b593bfd3d8334287a78f2ea96163b633534094acb1b`。临时授权、下载副本、HTTP 服务和验证页面已清理。
