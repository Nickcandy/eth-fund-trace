# Demo 架构与实施

## 1. 文档目的

本文描述当前代码实际实现的架构、运行路径、模块接口和部署方式。所有未落地能力统一放在 `03-追踪加速与下一阶段.md`，避免把设计目标误写成现状。

## 2. 系统边界

系统是只读的链上资金分析 Demo：输入 Ethereum 或 Base 地址，按需获取公开链上数据，保存标准化资金事实，再生成地址画像、资金图、标签传播和版本化风险结果。

系统不托管私钥、不广播交易、不扫描全链、不进行汇率换算，也不把“暂无风险证据”解释为“安全”。

## 3. 五层架构

```text
┌──────────────────────────────────────────────────────────┐
│ 1. 展示层                                                │
│ React 控制台、React Flow + ELK、任务进度、证据详情       │
├──────────────────────────────────────────────────────────┤
│ 2. HTTP 接入层                                           │
│ Echo 路由、参数校验、认证、限流、超时、错误契约          │
├──────────────────────────────────────────────────────────┤
│ 3. 应用编排层                                            │
│ syncer、tracer 异步任务与 profile/fundgraph/risk/bridge  │
├──────────────────────────────────────────────────────────┤
│ 4. 领域与适配层                                          │
│ 资金边模型、画像/传播规则、Etherscan 与 Mongo Adapter    │
├──────────────────────────────────────────────────────────┤
│ 5. 基础设施层                                            │
│ Etherscan V2、MongoDB、Docker、Go/Node 构建              │
└──────────────────────────────────────────────────────────┘
```

### 3.1 展示层

目录：`web/src`。

- `api`：严格 TypeScript 请求和响应类型；
- `graph`：事实边聚合、上下游分区和 ELK 输入；
- `components`：查询栏、资金图、详情、任务进度和证据写入；
- TanStack Query：异步任务轮询、缓存、取消和重试；
- URL：保存查询参数与 `traceJobId`，刷新后恢复同一持久化任务。

展示层不计算新的风险结论，不保存 Etherscan Key 或 Mongo 凭证。链上整数保持字符串，聚合时使用 `BigInt`。

### 3.2 HTTP 接入层

目录：`internal/httpapi`。

职责包括：

- 地址、链、方向、深度、Top-N 和资产参数校验；
- `202` 异步任务协议；
- `400/401/404/409/429/503/504` 错误映射；
- 请求 ID、Body 上限、请求超时、panic 恢复和访问日志；
- 可选 Bearer 鉴权和单进程 IP 令牌桶。

机器可读契约见 `openapi.yaml`。

### 3.3 应用编排层

| Module | 外部接口 | 隐藏的实现复杂度 |
| --- | --- | --- |
| `syncer` | `Enqueue`、`Job`、`Run` | 缓存、安全链头、分页续跑、区间拆分、批量 upsert、任务状态 |
| `tracer` | `Enqueue`、`Job`、`Trace` | 分层 BFS、未同步依赖、方向/资产裁剪、路径和终点剪枝 |
| `profile` | `Get` | 30 天窗口、生命周期统计、`hot-wallet-v1` 快照 |
| `fundgraph` | `Edges` | Mongo 方向/资产过滤、稳定游标和正金额事实边 |
| `risk` | `Analyze` | `propagation-v1` 标签衰减和 `risk-v1` 评分 |
| `bridge` | `Create`、`List` | 双链事实校验和跨链关联持久化 |

这些 module 的接口是主要测试 seam。HTTP 调用者不需要知道 Etherscan 分页或 Mongo 查询细节。

### 3.4 领域与适配层

`internal/etherscan` 将三种来源标准化为统一 `Transfer`：

| action | 语义 | 资金资产 |
| --- | --- | --- |
| `txlist` | 顶层交易 | 原生 ETH |
| `txlistinternal` | 执行轨迹中的 value 转移 | 原生 ETH |
| `tokentx` | ERC-20 `Transfer` 事件 | Token 合约资产 |

`internal/store` 是 Mongo Adapter。事实边唯一键为：

```text
(chain, txHash, source, traceId, logIndex, asset)
```

节点身份至少为 `(chain, address)`；同一 `0x` 地址在 Ethereum 和 Base 上不是同一图节点。

### 3.5 基础设施层

- Go 服务构建 HTTP、同步、图和风险模块；
- React/Vite 构建静态控制台；
- MongoDB 保存事实、任务、画像、标签和桥接关系；
- Dockerfile 使用 Node、Go、Alpine 三阶段构建；
- Docker Compose 默认启动 Mongo，可通过 `app` profile 启动完整应用。

## 4. 核心数据集合

| 集合 | 内容 | 关键约束 |
| --- | --- | --- |
| `transfers` | ETH、内部 ETH、ERC-20 事实边 | 幂等唯一键；金额为十进制字符串 |
| `addresses` | 同步覆盖范围、状态、终点属性 | `(chain,address)` 唯一 |
| `sync_jobs` | 同步请求、实时页数/条数、错误 | 重启后活动任务标记 `interrupted` |
| `trace_jobs` | 查询条件、同步依赖和图结果 | 结果持久化，URL 可恢复 |
| `address_profiles` | 规则版本化画像快照 | 地址、规则版本、数据区块唯一 |
| `labels` | 人工/公开确定性标签 | 来源参与唯一键 |
| `cross_chain_links` | 有双链事实的桥接关联 | 源/目标交易证据唯一 |

## 5. 当前地址追踪流程

```text
GET /api/v1/trace
  -> 创建 trace_job
  -> 确保种子地址已完整同步
  -> 从 Mongo 执行 BFS
  -> 遇到未同步邻居
  -> 创建一个 neighborLimit=0 的 sync_job
  -> 等待该邻居完成三类全历史同步
  -> 从头重跑 BFS
  -> 保存 TraceResult
```

这条路径保证已遍历地址的数据完整性，但会被高频邻居阻塞。它是当前实现，不是推荐的长期默认策略。

## 6. BFS 实施细节

- 默认深度 3，最大 5；
- 默认 Top-N 10，最大 20；
- frontier 每批最多 500 个地址，通过 Mongo `$in` 查询；
- 总访问节点最多 5000；
- 双向查询分别处理入边和出边；
- 已访问地址去重；
- 确定性终点标签保留到达边但停止扩展；
- `suspected_hot_wallet` 当前不自动成为终点；
- 查询每批最多从 Mongo 取 10,000 条候选边，再按稳定顺序选择每地址 Top-N。

注意：当前 Top-N 是按稳定边顺序裁剪，不是按跨资产金额排名。ETH 与不同 Token 没有共同可比金额。

## 7. 画像与风险

`hot-wallet-v1` 使用最新交易向前 30 天的事件统计：交易数、独立对手方、活跃天数、归集和批量转出。达到 70 分且入/出均至少 5 条才输出 `suspected_hot_wallet`。这是行为推断，不是确定性归属标签。

`risk-v1` 主要根据人工/公开标签及传播置信度评分，不计金额、不进行跨资产换算。传播低于 0.5 时不输出。

## 8. 运行和验收

```powershell
$env:HTTP_AUTH_DISABLED='true'
docker compose --profile app up -d --build
Invoke-RestMethod http://localhost:8080/healthz
```

控制台：`http://localhost:8080`。

```powershell
go test ./...
go vet ./...
golangci-lint run ./...
cd web
npm run typecheck
npm run test
npm run build
npm run test:e2e
```

## 9. Demo 实施顺序

1. 启动 Mongo 和应用，确认 `/healthz`；
2. 使用低频地址展示同步任务和三类事实边；
3. 展示异步追踪、上下游布局、节点/边详情；
4. 添加一条带证据的人工标签，展示传播路径和风险原因；
5. 展示 Base 节点与人工确认的桥接关系；
6. 使用高频地址说明当前全历史策略的瓶颈，再展示下一阶段的有界追踪方案。

Demo 中必须明确区分“链上事实”“行为推断”“确定性标签”和“推荐但尚未实现的优化”。

## 10. 外部额度说明

代码默认 `ETHERSCAN_REQUESTS_PER_SECOND=5`，表示进程内共享 limiter 的配置值，不代表 API Key 实际套餐。根据 2026-08-23 核验的官方费率页，Free 为 3 QPS，Lite 为 5 QPS；部署时必须按 Key 套餐下调配置。每日额度还需要独立预算控制，单靠 QPS limiter 无法保证。
