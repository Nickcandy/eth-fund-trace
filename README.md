# ETH 资金链路追踪

这是一个面向 Ethereum 和 Base 的只读资金分析 Demo。系统按需采集普通 ETH、内部 ETH 和 ERC-20 事实，持久化到 MongoDB，再提供地址画像、分层资金图、交易协议解释、确定性标签和有界风险关联。

项目重点不是生成一张尽可能大的图，而是在外部数据源有配额、单地址可能拥有百万级记录、链上语义存在歧义的条件下，输出有边界、可解释、可回查的结果。

## 后端概览

```text
Etherscan V2 / Ethereum RPC / Base RPC
                    |
                    v
       syncer / transactionanalysis / bridge
                    |
                    v
              MongoDB 事实与任务
                    |
          +---------+----------+
          |                    |
	  tracer trace-v6   propagation-v3
          |                    |
          +---------+----------+
                    v
             Echo HTTP API / Web
```

后端以 `cmd/server` 为组合入口，核心模块位于 `internal`：

- `syncer`：分页采集三类地址历史，保存检查点和同步进度；
- `store`：MongoDB Adapter、事实幂等写入、索引和有界聚合查询；
- `tracer`：`trace-v6` 异步分层追踪，先识别合约身份，再按资产和方向选择累计金额 TopN 对手方；
- `transactionanalysis`：基于交易、Receipt、单交易 Internal Transaction 和合约校验解释 Uniswap V3、KyberSwap RFQ 与 WETH 证据；
- `bridge`：识别 Ethereum/Base 官方 OP Stack Bridge 并维护生命周期；
- `profile`：生成 `hot-wallet-v1` 地址画像快照；
- `propagation`：`propagation-v3` 以查询目标为中心的双向风险评估任务，不依赖展示图的 TopN；
- `httpapi`：Echo 路由、参数校验、鉴权、限流、超时和统一错误响应。

详细职责、数据集合和运行链路见 [后端项目架构](docs/01-后端项目架构.md)。

## 已实现能力

- Ethereum/Base 地址历史同步、共享限流、重试、区间拆分、分页续跑和批量幂等写入；
- 普通 ETH、内部 ETH、ERC-20 的统一事实模型和稳定游标分页；
- `trace-v6` 持久化 TraceJob，上下游分别展开，TopN 按同资产的对手方累计金额排序；
- 根节点展示 ETH 及官方白名单 Token：Ethereum 的 DAI、USDC、USDT、WETH，Base 的 USDC、WETH；
- 合约关系最多分析金额最大的 20 笔交易，只依据完整且无歧义的 Swap/Wrap 证据切换资产；
- 人工/公开确定性标签、`hot-wallet-v1` 行为画像和风险解释；
- `propagation-v3` 持久化任务：目标中心双向最多 3 跳、多源饱和聚合和结构化路径证据；分数是调查优先级，不是违法概率。
- Ethereum/Base 官方 OP Stack Bridge 的有限解析、双链证据关联和状态同步；
- React 控制台中的图、任务进度、节点打标、风险关联和交易事实分页。

## Demo 边界

当前实现验证了从数据采集、事实存储、图查询到风险解释的闭环，但不是全链生产索引器：

- Trace 遇到未同步 EOA 邻居时仍可能触发全历史同步，高频地址会拖慢交互任务；合约关系使用最多 20 笔关系交易分析，不同步合约全历史；
- 同步和 Trace 调度以单进程队列为主，不具备完整的多实例领取与恢复能力；
- 没有链重组回滚、持续缺块校验、多数据源容灾和全局 API 配额治理；
- 地址余额、`profile_job`、高扇出专项分析、交易锚点追踪和通用多协议/多链适配尚未完成；
- 风险传播输出的是“与已知风险源的关联”，不是地址身份或恶意定论；
- 账户制链上的路径表示资金关联，不代表 UTXO 式逐币归因。

完整清单及生产建设方式见 [Demo 边界与生产化](docs/03-Demo边界与生产化.md)。

## 核心文档

| 文档 | 内容 |
| --- | --- |
| [后端项目架构](docs/01-后端项目架构.md) | 分层、模块职责、Mongo 集合与索引、核心数据流和任务模型 |
| [项目难点与解决方案](docs/02-项目难点与解决方案.md) | 数据完整性、百万级聚合、图扇出、协议证据、风险传播和可靠性 |
| [Demo 边界与生产化](docs/03-Demo边界与生产化.md) | 已完成、未完成、主动删减及生产级建设方案 |
| [Demo 运行手册](docs/04-Demo运行手册.md) | 启动、状态解释、排障和测试 |
| [架构决策记录](docs/05-架构决策记录.md) | 当前关键技术取舍 |
| [OpenAPI](docs/openapi.yaml) | HTTP 接口契约 |

专项资料包括 [外部数据源与链上语义研究](docs/07-外部数据源与链上语义研究.md)、[DEX 交易解析](docs/08-DEX交易解析设计.md) 和 [官方桥识别与状态同步](docs/09-官方桥识别与状态同步.md)。

## 启动

```powershell
$env:HTTP_AUTH_DISABLED='true'
docker compose --profile app up -d --build
Invoke-RestMethod http://localhost:8080/healthz
```

访问 `http://localhost:8080`。环境变量和排障方式见 [Demo 运行手册](docs/04-Demo运行手册.md)。

## 验证

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
