# ETH 资金链路追踪 Demo

只读的 Ethereum/Base 资金分析系统：按需采集普通 ETH、内部 ETH 和 ERC-20 事实边，持久化到 MongoDB，再执行地址画像、分层资金图、标签传播、风险解释和跨链证据展示。

## 当前能力

- Etherscan V2 三类地址历史适配、共享限流、重试、分页续跑和幂等写入；
- 异步同步任务及实时页数、读取/写入条数和区块范围；
- Mongo 事实边方向/资产过滤和稳定游标；
- 异步 BFS、Top-N、终点剪枝和未同步依赖；
- `hot-wallet-v1`、`propagation-v1`、`risk-v1`；
- 人工/公开标签和有双链证据的 Ethereum/Base 桥接关系；
- React Flow + ELK 中文分析控制台，刷新后从 URL 恢复 TraceJob。

## 重要边界

当前交互式追踪仍会为待展开邻居执行三类全历史同步。真实运行中，一个高频地址产生约 87 万条事实边并长期阻塞任务。该问题、热钱包定义、内部交易、ERC-20、L2 和推荐的有界追踪方案已记录在文档中，不能在 Demo 中宣称已经解决。

代码默认 5 QPS 是本地 limiter 配置，不代表 Etherscan Free 套餐。2026-08-23 核验的官方费率为 Free 3 QPS、Lite 5 QPS，运行时应按 Key 套餐配置。

## 文档导航

| 文档 | 用途 |
| --- | --- |
| [Demo 架构与实施](docs/01-Demo架构与实施.md) | 五层架构、module 接口、数据流、实施和 Demo 顺序 |
| [运行难点与工程结论](docs/02-运行难点与工程结论.md) | 热钱包、高频节点、内部交易、ERC-20、L2 和其他复杂问题 |
| [追踪加速与下一阶段](docs/03-追踪加速与下一阶段.md) | 有界地址追踪、txHash 锚点、热钱包 v2 和实施阶段 |
| [Demo 运行手册](docs/04-Demo运行手册.md) | 启动、演示脚本、状态解释、排障和测试 |
| [架构决策记录](docs/05-架构决策记录.md) | 已采用和下一阶段的关键取舍 |
| [前端架构决策](docs/06-M10前端架构决策.md) | 图布局、金额、鉴权和部署选择 |
| [外部数据源与链上语义研究](docs/07-外部数据源与链上语义研究.md) | 21 个官方来源核验与工程推论 |
| [DEX 交易解析设计](docs/08-DEX交易解析设计.md) | Uniswap V3、Universal Router、WETH 和交易哈希分析边界 |
| [M12 官方桥识别与状态同步设计](docs/09-M12官方桥识别与状态同步设计.md) | Ethereum↔Base 官方桥 ETH/ERC-20 自动识别、生命周期和有界状态同步 |
| [项目难点与方案权衡](docs/10-项目难点与方案权衡.md) | 数据规模、查询、缓存、图追踪、协议语义和打标的工程权衡 |
| [生产级跨链追踪展望](docs/11-生产级跨链追踪展望.md) | 当前跨链能力边界、未来生产方案和分阶段范围 |
| [OpenAPI](docs/openapi.yaml) | 当前 HTTP 契约 |

## 启动

```powershell
$env:HTTP_AUTH_DISABLED='true'
docker compose --profile app up -d --build
Invoke-RestMethod http://localhost:8080/healthz
```

访问 `http://localhost:8080`。详细流程见 [Demo 运行手册](docs/04-Demo运行手册.md)。

## 验收

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
