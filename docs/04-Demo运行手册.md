# Demo 运行手册

## 1. 启动

前置条件：Docker Desktop、可用 Etherscan V2 Key。

```powershell
$env:HTTP_AUTH_DISABLED='true'
docker compose --profile app up -d --build
```

检查：

```powershell
docker compose --profile app ps
Invoke-RestMethod http://localhost:8080/healthz
```

打开 `http://localhost:8080`。

停止：

```powershell
docker compose --profile app down
```

Mongo 数据卷默认保留；`down` 不会清空已同步事实。

## 2. 推荐演示流程

### 场景 A：低频地址闭环

1. 输入低频地址；
2. 选择 `depth=1` 或 `2`、`topN=3`；
3. 创建分析；
4. 在“任务进度”查看当前 action、页数、API 已读、Mongo 已写和区块范围；
5. 完成后展示上游、种子和下游节点；
6. 点击汇总边查看累计金额、准确笔数和合约转换扫描状态。

### 场景 B：标签和风险解释

1. 在节点详情中为固定地址添加人工/公开风险标签和证据；
2. 从该节点启动独立风险传播任务；
3. 展示任务状态、传播方向、跳数、置信度、路径和截断信息；
4. 强调关联结果是版本化推断，不是归属定论。

### 场景 C：多链与桥接

1. 展示 Ethereum/Base 节点身份隔离；
2. 分析官方 OP Stack Bridge 交易，或导入包含双链交易事实的人工桥接关系；
3. 展示桥接虚线边；
4. 说明系统不根据相同地址或相近金额自动连接。

### 场景 D：工程瓶颈

用已记录的高频地址运行数据说明：当前全历史策略会被单节点拖慢。不要在现场重新等待几十分钟；直接展示进度截图或数据库统计，并结合 [项目难点与解决方案](02-项目难点与解决方案.md) 说明有界追踪方案。

## 3. 页面状态解释

| 状态 | 含义 |
| --- | --- |
| `queued` | 等待单 worker |
| `waiting_sync` | TraceJob 正等待一个地址同步 |
| `running` | 正在同步或执行 BFS |
| `succeeded` | 当前规则下完成 |
| `partial` | 有部分邻居或数据失败 |
| `failed` | 任务失败，可根据 retryable 决定重试 |
| `interrupted` | 服务重启中断，任务不会自动恢复 |

刷新页面不会丢失当前 TraceJob：任务 ID 保存在 URL，页面会从 Mongo 恢复任务和同步依赖。Bearer Token 仍只保存在页面内存，启用鉴权时刷新后需要重新输入。

## 4. 常用排障

```powershell
docker compose --profile app logs --since 10m --timestamps app
docker stats --no-stream eth-fund-trace-app-1 eth-fund-trace-mongo-1
```

检查最近任务时不要输出 `.env` 或 API Key。Mongo 中重点查看：

```text
sync_jobs.status/progress/errorCode
trace_jobs.status/syncJobIds/errorCode
addresses.syncStatus/latestSyncedBlock
```

常见判断：

- 页面持续轮询且 `progress.pagesFetched` 增长：任务未死锁，仍在拉取；
- `recordsRead > recordsWritten`：可能有失败交易过滤或边界重读，不等于数据丢失；
- Mongo 无活动操作但网络持续增长：可能正在读取 Etherscan 响应；
- `interrupted`：服务重启导致，重新提交任务；
- `page_limit` 且范围为单一区块：当前数据源无法完整分页该区块。

## 5. 测试

```powershell
go test ./...
go vet ./...
golangci-lint run

cd web
npm run typecheck
npm run test
npm run build
npm run test:e2e
```

真实 Etherscan 测试显式执行，会消耗额度：

```powershell
$env:ETHERSCAN_LIVE_TEST='1'
go test -tags=integration ./internal/etherscan -run TestLive -v -count=1
```

## 6. Demo 声明边界

- 当前地址追踪仍可能触发邻居全历史同步；
- 高交易量只能证明高频，不能单独证明热钱包；
- ERC-20 和内部 ETH 是两种不同事实；
- L2 链内交易与跨链桥接必须分开建模；
- Top-N 不做跨资产价值比较；
- 完整度不足必须展示 `partial` 或数据质量说明；
- 当前是单进程 Demo，不具备分布式任务恢复和集中配额控制。
