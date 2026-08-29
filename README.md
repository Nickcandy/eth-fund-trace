# ETH 资金链路追踪

这是一个面向 Ethereum 的只读资金分析 Demo。系统按需采集普通 ETH、内部 ETH 和 ERC-20 事实，持久化到 MongoDB，再提供地址画像、分层资金图、交易协议解释和确定性标签。

项目重点不是生成一张尽可能大的图，而是在外部数据源有配额、单地址可能拥有百万级记录、链上语义存在歧义的条件下，输出有边界、可解释、可回查的结果。

## 后端概览

```text
Etherscan V2 / Ethereum RPC
                    |
                    v
          syncer / transactionanalysis
                    |
                    v
              MongoDB 事实与任务
                    |
               tracer trace-v1
                    v
             Echo HTTP API / Web
```

后端以 `cmd/server` 为组合入口，核心模块位于 `internal`：

- `syncer`：分页采集三类地址历史，保存检查点和同步进度；
- `store`：MongoDB Adapter、事实幂等写入、索引和有界聚合查询；
- `tracer`：`trace-v1` 资金状态追踪，按具体交易、资产、金额预算和时间方向解释资金来源与去向；
- `transactionanalysis`：基于交易、Receipt、单交易 Internal Transaction 和合约校验解释 Uniswap V3、KyberSwap RFQ 与 WETH 证据；
- `profile`：生成 `hot-wallet-v1` 地址画像快照；
- `httpapi`：Echo 路由、参数校验、鉴权、限流、超时和统一错误响应。

详细职责、数据集合和运行链路见 [后端项目架构](docs/01-后端项目架构.md)。

主资金追踪设计见 [资金链路追踪设计](docs/10-资金链路追踪设计.md)。中心地址全量账本、链路地址时间窗口、合约单笔交易解析和高频终止分别定义。

## 已实现能力

- Ethereum 地址历史同步、共享限流、重试、区间拆分、分页续跑和批量幂等写入；
- 普通 ETH、内部 ETH、ERC-20 的统一事实模型和稳定游标分页；
- `trace-v1` 持久化 TraceJob，上下游按具体资金事实、时间边界和剩余金额展开；
- 根节点展示 ETH 及官方白名单 Token：DAI、USDC、USDT、WETH；
- 合约只分析资金进入交易，只依据完整且无歧义的 Swap/Wrap 证据切换资产；
- 人工/公开确定性标签、`hot-wallet-v1` 行为画像和仅基于直接标签的风险解释；
- 已知跨链桥合约仅作为追踪终点，不解析或继续跨链；
- React 控制台中的手动展开图、任务进度、节点打标和交易事实分页。

## Demo 边界

当前实现验证了从数据采集、事实存储、图查询到风险解释的闭环，但不是全链生产索引器：

- Trace 遇到未同步 EOA 邻居时仍可能触发全历史同步，高频地址会拖慢交互任务；合约关系使用最多 20 笔关系交易分析，不同步合约全历史；
- 同步和 Trace 调度以单进程队列为主，不具备完整的多实例领取与恢复能力；
- 没有链重组回滚、持续缺块校验、多数据源容灾和全局 API 配额治理；
- `profile_job`、高扇出专项分析、交易锚点追踪和通用多协议适配尚未完成；
- 不做标签传染或相邻地址风险推断；
- 账户制链上的路径表示资金关联，不代表 UTXO 式逐币归因。

完整清单及生产建设方式见 [Demo 边界与生产化](docs/03-Demo边界与生产化.md)。

## 核心文档

| 文档 | 内容 |
| --- | --- |
| [后端项目架构](docs/01-后端项目架构.md) | 分层、模块职责、Mongo 集合与索引、核心数据流和任务模型 |
| [项目难点与解决方案](docs/02-项目难点与解决方案.md) | 数据完整性、手动扩展、协议证据、直接标签风险和可靠性 |
| [Demo 边界与生产化](docs/03-Demo边界与生产化.md) | 已完成、未完成、主动删减及生产级建设方案 |
| [Demo 运行手册](docs/04-Demo运行手册.md) | 启动、状态解释、排障和测试 |
| [架构决策记录](docs/05-架构决策记录.md) | 当前关键技术取舍 |
| [资金链路追踪设计](docs/10-资金链路追踪设计.md) | 时间顺序、地址覆盖、合约解析、资金归因和终止规则 |
| [OpenAPI](docs/openapi.yaml) | HTTP 接口契约 |

专项资料包括 [外部数据源与链上语义研究](docs/07-外部数据源与链上语义研究.md) 和 [DEX 交易解析](docs/08-DEX交易解析设计.md)。跨链分析不在当前版本范围内。

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
