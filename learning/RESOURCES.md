# Web3 / Ethereum Interview Resources

## Knowledge

- [Ethereum: Transactions](https://ethereum.org/en/developers/docs/transactions/)
  交易字段、签名、calldata、gas、typed transaction 和生命周期。用于建立项目中 transaction 与执行结果的边界。
- [Ethereum: JSON-RPC API](https://ethereum.org/en/developers/docs/apis/json-rpc/)
  节点读取接口、十六进制编码、block tag、`eth_call`、transaction 和 receipt。用于理解 `internal/chainrpc`。
- [ERC-20: Token Standard](https://eips.ethereum.org/EIPS/eip-20)
  Token 方法与 `Transfer`/`Approval` 事件的规范来源。用于理解 ERC-20 事实边和 metadata 不完整问题。
- [Ethereum: Accounts](https://ethereum.org/en/developers/docs/accounts/)
  EOA、合约账户和账户状态。用于解释地址、nonce、code 与合约识别。
- [Ethereum: Gas and Fees](https://ethereum.org/en/developers/docs/gas/)
  gas unit、base fee、priority fee 与执行资源计价。用于回答 EIP-1559 和失败交易费用问题。
- [Solidity: Contract ABI Specification](https://docs.soliditylang.org/en/latest/abi-spec.html)
  函数 selector、32-byte word、静态/动态参数、event topics/data 与 custom error 的编码规则。用于核对项目中的日志和 `eth_call` 解码。
- [Solidity: Error Handling](https://docs.soliditylang.org/en/latest/control-structures.html#reverting)
  `require`、`assert`、`revert`、错误数据和状态回滚语义。用于区分调用失败、交易失败与可捕获的子调用失败。
- [EIP-7702: Set Code for EOAs](https://eips.ethereum.org/EIPS/eip-7702)
  现代 EOA 可设置代码委托指示器，因此不能再把“有 code”机械等同于传统合约账户。用于回答地址类型判断的现代边界。
- [OpenZeppelin: IERC20.sol](https://github.com/OpenZeppelin/openzeppelin-contracts/blob/master/contracts/token/ERC20/IERC20.sol)
  ERC-20 接口的主流开源实现，适合先看函数和事件声明。
- [OpenZeppelin: ERC20.sol](https://github.com/OpenZeppelin/openzeppelin-contracts/blob/master/contracts/token/ERC20/ERC20.sol)
  ERC-20 的完整基础实现，适合对照 `transfer`、`approve`、`transferFrom` 和内部余额更新。
- [Uniswap V3 Core](https://github.com/Uniswap/v3-core)
  V3 Pool 与事件接口的官方源码。用于对照 Pool 的 Swap 实现和事件语义。
- [Geth: EVM Tracing](https://geth.ethereum.org/docs/developers/evm-tracing)
  调用帧和执行级 trace。用于解释为什么普通交易和 receipt 无法完整恢复内部 ETH 调用。
- [Etherscan V2 API](https://docs.etherscan.io/etherscan-v2)
  多链账户历史和 API 模型。用于理解当前同步层是索引服务适配器，不是 Ethereum 协议本身。
- [Base: Connecting to Base](https://docs.base.org/base-chain/quickstart/connecting-to-base)
  Base 的 EVM 兼容性、chain ID 8453、RPC 和公开端点限制。用于解释多链隔离。
- [Base: Bridging and Withdrawals](https://docs.base.org/base-chain/network-information/bridging-and-withdrawals)
  deposit 与标准 withdrawal 的协议方向、证明和挑战期。用于理解桥状态机。
- [Uniswap V3: Pool Events](https://docs.uniswap.org/contracts/v3/reference/core/interfaces/pool/IUniswapV3PoolEvents)
  V3 Pool 的 `Swap` 事件语义。用于理解交易分析器对 amount0/amount1 的解析。

## Wisdom (Communities)

- [Ethereum Stack Exchange](https://ethereum.stackexchange.com/)
  高质量的协议与开发问题库。用于验证具体 EVM、RPC、ABI 和 Solidity 边界问题。
- [Ethereum Research](https://ethresear.ch/)
  协议研究社区。用于深入 rollup、最终性和扩容机制，不作为日常 API 文档替代品。

## Gaps

- 后续课程需按实际面试岗位补充：偏 Go 后端、链上数据工程、协议分析或 Web3 全栈的权重不同。
