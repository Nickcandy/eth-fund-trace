# Etherscan V2 Account API 分页与套餐限制

核对日期：2026-08-25。本文只引用 Etherscan 官方资料。

## 结论

1. `txlist`、`txlistinternal`、`tokentx` 都支持 `page` 和 `offset`。三个官方 OpenAPI 页面把 `offset=100` 写作示例，但没有声明 `100` 是最大值，也没有给出 `offset` 的 `maximum` 约束。因此，不能根据官方文档断言 Etherscan 只能每次返回 100 条。
2. 同样地，官方文档没有承诺这些接口支持某个大于 100 的固定 `offset` 上限。2026-08-25 对 Ethereum 高密度区间实测显示：`offset=1000` 返回 1000 条，`offset=2000/5000` 仍只返回 1000 条；同时服务端要求 `page × offset <= 50000`。这是当时实测行为，不是官方长期契约。生产实现仍需分页、按区块范围切分、重试和断点续传。
3. 购买套餐明确改善调用速率和每日额度；官方资料没有说明付费套餐会扩大 `txlist`、`txlistinternal`、`tokentx` 的单页记录数。
4. 当前项目已使用每页 1000 条、每个区块窗口最多 50 页、3 calls/s。升级 Standard（10 calls/s）可把纯请求阶段的理论吞吐提高到约 3.33 倍，Professional/Pro Plus（30 calls/s）约 10 倍。实际加速会受 Etherscan 响应时间、超时重试、区块切分、应用解析和 MongoDB 写入限制。

## 分页参数

三个端点对 `offset` 的官方描述均为“每页返回的记录数，后续记录使用 `page` 参数”，schema 仅标注 `integer`，示例为 `100`：

- [`txlist`](https://docs.etherscan.io/api-reference/endpoint/txlist)：普通交易。
- [`txlistinternal`](https://docs.etherscan.io/api-reference/endpoint/txlistinternal)：内部交易。
- [`tokentx`](https://docs.etherscan.io/api-reference/endpoint/tokentx)：ERC-20 Transfer 记录。

Etherscan 的[最佳实践](https://docs.etherscan.io/resources/best-practices)也只说明 `offset` 决定一次请求返回多少条，并建议用 `page`/`offset` 和 `startblock`/`endblock` 缩小查询；没有给出上述三个端点的最大 `offset`。其[常见错误说明](https://docs.etherscan.io/common-error-messages)指出，大数据集可能触发查询超时，官方建议缩小日期或区块范围。

因此，`offset=100` 应视为当前项目的保守分页选择，而不是已经由官方证明的硬限制。若要提高它，应先针对三个 action 分别做小规模兼容性测试，并保留服务端截断、超时或行为变化时的回退逻辑。

## 套餐速率与每日额度

根据 Etherscan 官方[限流页面](https://docs.etherscan.io/rate-limits)：

| 套餐 | 调用速率 | 每日额度 | PRO endpoints |
| --- | ---: | ---: | --- |
| Free | 3 calls/s | 100,000/day | 不可用 |
| Lite | 5 calls/s | 100,000/day | 不可用 |
| Standard | 10 calls/s | 200,000/day | 可用 |
| Advanced | 20 calls/s | 500,000/day | 可用 |
| Professional | 30 calls/s | 1,000,000/day | 可用 |
| Pro Plus | 30 calls/s | 1,500,000/day | 可用 |
| Dedicated/Custom | 联系 Etherscan | 联系 Etherscan | 可用 |

官方还说明 Free 仅覆盖部分链；当前项目只使用 Ethereum Mainnet。

## 历史数据与分页能力差异

官方当前资料没有声明 Free 与付费套餐在 `txlist`、`txlistinternal`、`tokentx` 的 Ethereum 历史区块深度或分页上限上存在差异。这三个端点也未在各自页面标为 PRO endpoint。

能够由官方资料确认的套餐差异是：

- calls/s；
- daily quota；
- Free 套餐的链覆盖范围；
- PRO endpoints 是否可用。

所以，对 Ethereum 大地址全历史同步，付费套餐主要通过更高 calls/s 缩短请求耗时，并通过更高 daily quota 降低当天耗尽配额的风险；它不会从官方契约层面消除逐页拉取、区块切分、超时和本地落库成本。

## 工程建议

- 保持分页和按区块范围递归切分，不能假设购买套餐后可一次拉完整历史。
- 若升级套餐，应把客户端限流值同步调整到套餐上限，并保留少量余量，避免多个任务共享同一 API key 时触发限流。
- 可独立压测 `offset=100`、`500`、`1000`；只有接口实际返回完整页且在大地址、高密度区块下稳定时才提高默认值。该测试只能证明当时的实际行为，不等于官方长期保证。
- 使用官方 [`getapilimit`](https://docs.etherscan.io/api-reference/endpoint/getapilimit) 端点监控当前周期的额度消耗。
