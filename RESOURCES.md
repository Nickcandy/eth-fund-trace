# ETH 资金追踪项目面试资源

## Knowledge

- [项目源码：`internal/tracer/tracer.go`](internal/tracer/tracer.go)
  当前追踪状态、分层扩展、金额预算、时间锚点、终点与资源保护的最终事实来源。
- [项目源码：`internal/transactionanalysis/service.go`](internal/transactionanalysis/service.go)
  Transaction/Receipt、ERC-20、WETH、Uniswap V3、KyberSwap、THORChain、MAYAChain 和 BitTorrent Bridge 的解析规则。
- [Etherscan API: Get Normal Transactions By Address](https://docs.etherscan.io/api-reference/endpoint/txlist)
  说明地址交易历史接口的区块范围、分页、排序，以及 `status=0` 既可能是错误也可能是空结果。
- [Ethereum JSON-RPC API](https://ethereum.org/developers/docs/apis/json-rpc/)
  说明交易、receipt、logs、历史数据和 `safe`/`finalized` 区块标签。用于解释为什么地址历史依赖索引器，而单笔交易语义使用 RPC 事实。
- [Uniswap V3: `IUniswapV3PoolEvents.sol`](https://github.com/Uniswap/v3-core/blob/main/contracts/interfaces/pool/IUniswapV3PoolEvents.sol)
  官方 `Swap` 事件定义；`amount0/amount1` 是 Pool 的 token0/token1 余额变化，输出接收者来自 `recipient`。
- [THORChain Transaction Memos](https://dev.thorchain.org/concepts/memos)
  官方 memo 格式、Swap 目标资产和目标地址语义。用于解释为什么只解 calldata 不足以证明跨链出站已经发生。

## Wisdom (Communities)

- 面试现场的代码追问
  最有价值的反馈来自面试官对“为什么这样做、哪里可能错、生产化怎么改”的连续追问；回答必须区分当前实现、设计目标和未来改进。

## Gaps

- KyberSwap RFQ、MAYAChain 与 BitTorrent Bridge 当前实现依据主要沉淀在代码和 fixtures 中，尚未为每条验证规则整理对应的官方合约版本链接。
- 当前设计文档与代码在小额过滤、协议范围和同步保护阈值的表述存在漂移；面试以代码事实为准。
