# DEX 交易解析设计

## 1. 目标与范围

当前范围分析 Ethereum Mainnet 上的 Uniswap V3、Universal Router 和 WETH。输入一个已确认的交易哈希，输出发起地址、入口合约、经过验证的成交池、逐段资产输入输出、包装/解包事件和证据质量，并允许从明确的输出地址继续现有地址追踪。

当前不支持 Uniswap V2/V4、1inch、Curve、Balancer、CowSwap 或自动跨链识别，也不把时间相关路径描述成逐币归因证明。

## 2. 事实与解释

```text
Transaction                  Receipt
from/to/value/input          status/logs
       |                        |
       +------ 链上事实 --------+
                    |
          Transfer / WETH / Swap logs
                    |
          Uniswap V3 Pool 验证
                    |
            TransactionAnalysis
```

- `Transfer` 保持链上资金事实，不因识别到 Swap 而改变。
- `SwapEvent` 是对同一交易日志的协议解释，不写成新的资金边。
- 交易 `to` 可能是 Universal Router，但实际成交协议和 Pool 必须由日志 emitter 与官方 Factory 验证。
- 同一交易可以有多个 Pool 和资产段；不能唯一确定总体路线时保留逐段事实并标记 `ambiguousRoute`。

## 3. 数据来源

复用 Etherscan V2 Proxy 和现有共享 limiter：

```text
eth_getTransactionByHash -> from/to/value/input/blockNumber
eth_getTransactionReceipt -> status/logs
eth_call(pool) -> token0/token1/fee/factory
```

交易不存在、Receipt 尚未生成、上游限流和上游不可用必须区分。API Key 只从环境变量读取，不进入日志或错误文本。

## 4. Uniswap V3 与 WETH

Ethereum Mainnet 只读注册表：

| 类型 | 地址 |
|---|---|
| V3 Factory | `0x1F98431c8aD98523631AE4a59f267346ea31F984` |
| V3 SwapRouter | `0xE592427A0AEce92De3Edee1F18E0157C05861564` |
| SwapRouter02 | `0x68b3465833fb72A70ecDF485E0e4C7bD8665Fc45` |
| Universal Router | `0xEf1c6E67703c7BD7107eed8303Fbe6EC2554BF6B` |
| Universal Router | `0x66a9893cC07D91D95644AEDD05D03f95e1dBA8Af` |
| WETH9 | `0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2` |

V3 合约地址来自 [Uniswap Ethereum deployments](https://docs.uniswap.org/contracts/v3/reference/deployments/ethereum-deployments)，Universal Router 地址来自 [Universal Router deployments](https://docs.uniswap.org/contracts/universal-router/deploy-addresses)。注册表匹配不替代 Pool 验证。

- V3 `Swap` 的 `amount0/amount1` 是 Pool 余额变化：正值表示 Pool 收到，负值表示 Pool 发出。
- 解析器使用二进制补码读取 `int256`，所有数量以十进制字符串返回。
- Pool 需要读取 `token0()`、`token1()`、`fee()`、`factory()`；只有 Factory 等于官方 Ethereum Uniswap V3 Factory 才设置 `protocol=uniswap`、`version=v3`。
- WETH `Deposit` 和 `Withdrawal` 单独返回为包装事件，不伪装成 Swap。
- ERC-20 `Transfer` 日志用于补充发出方、接收方和资产流证据；Token 元数据缺失不应丢弃已确认事件。

## 5. 存储

新增集合：

```text
transaction_analyses: unique(chain, txHash)
pool_metadata:         unique(chain, pool)
```

分析结果采用 read-through cache。Pool 元数据成功验证后复用；失败或不完整的元数据保留质量状态，不把未知合约认定为 Uniswap。

## 6. HTTP 契约

```http
GET /api/v1/transactions/:txHash?chain=ethereum
```

成功返回单个 `TransactionAnalysis`。错误覆盖：无效参数、交易不存在、Receipt 未确认、上游限流、上游不可用和内部错误。接口保持现有嵌套错误 envelope。

## 7. 前端闭环

查询栏支持“地址 / 交易哈希”模式。交易视图展示入口 Router、实际 Pool、逐段输入输出、WETH 事件和证据质量。只有输出地址明确时才显示“继续追踪”，该操作创建现有地址级 TraceJob，不宣称是严格的交易锚定追踪。

## 8. 完成标准

固定 Uniswap V3、Universal Router、WETH、两跳 Swap、未知协议和失败交易 fixtures 全部通过；普通测试不访问外部网络。Mongo 缓存、HTTP 状态、前端交互和桌面/移动端 E2E 均通过。
