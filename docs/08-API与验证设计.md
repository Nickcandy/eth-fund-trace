# 08 · API 与验证设计

## 第一阶段 API

### `POST /api/v1/sync`

按地址补齐 Etherscan 数据并入库。只负责同步，不执行完整风险判断。

始终返回 `202` 和 `jobId`；同地址已有活动任务时返回同一 ID。`neighborLimit` 取值 `0..10`，默认 10。

### `GET /api/v1/sync-jobs/:id`

返回 `queued -> running -> succeeded | partial | failed` 状态及同步计数、邻居结果和可重试错误。

### `GET /api/v1/edges`

查询一个或多个已同步地址的正金额事实边：

```text
chain, address, direction=in|out|both, asset=all|ETH|erc20|<token contract>
fromBlock, toBlock, limit, cursor
```

`address` 可重复传入；`limit` 默认 100、最大 500。结果按 `blockNumber, txHash, source, traceId, logIndex, asset` 降序排列，`cursor` 为不透明 Base64URL 值。不推断同哈希跨资产换币关系，不做汇率换算。

### `GET /api/v1/trace`

执行上游、下游或双向追踪。

```text
chain, address, direction, depth, topN, asset
```

返回节点、边、路径、跳数和数据不足提示。

### `GET /api/v1/addresses/:address`

返回地址元数据、合约信息、同步状态和已有标签。

### `GET /api/v1/addresses/:address/profile`

根据已同步数据计算地址画像，例如交易频率、独立对手方、归集行为和疑似热钱包分数。画像结果不自动等同于人工标签。

当前规则版本为 `hot-wallet-v1`，快照唯一键为 `(chain, address, ruleVersion, dataThroughBlock)`。少于 10 条记录输出 `insufficient_data`；画像只表达推断信号，不写入确定性标签。

### `POST /api/v1/labels`

添加或更新人工标签，记录来源、备注、证据和风险等级。

### `GET /api/v1/risk`

根据当前数据和固定规则计算风险分数、等级和解释。也可以作为 `/trace` 的可选结果字段，但内部职责独立。

## 错误和限制

统一处理：

- 地址格式错误：`400`；
- 参数超过深度/节点/边上限：`400`；
- 外部数据源限流：`503` 或可重试错误；
- 同步失败：返回任务状态和错误原因；
- 画像或资金边查询的地址从未成功同步：`409 address_not_synced`，不能解释为无风险；
- 未找到资金边：返回空页，仅表示当前过滤条件下没有已同步的正金额事实边。

每个请求需要有超时、日志、请求 ID 和最大资源限制。

## 测试层次

### 单元测试

- Etherscan 响应解析和标准化；
- 金额字符串和 Token 精度处理；
- 普通/内部/ERC-20 去重键；
- 上游、下游和双向 BFS；
- `$in` 分批逻辑；
- 终点剪枝和 Top-N；
- 标签传播和固定风险评分。

### 集成测试

- Docker MongoDB 连接和索引；
- upsert 幂等性；
- 按地址同步流程；
- API 参数、状态码和错误响应。

### 外部 API 测试

只做少量显式集成测试，使用环境变量提供 API Key；不能让普通单元测试依赖 Etherscan 网络。

## 完成标准

第一阶段完成的最低标准是：

1. 输入一个 Ethereum Mainnet 地址可以按需同步三类资金记录；
2. 再次输入同一地址不会重复拉取或重复入库；
3. 追踪使用分层 `$in` 查询和内存 BFS；
4. 结果能区分 ETH、内部 ETH 和 ERC-20；
5. 人工标签、推断标签和风险原因可追溯；
6. 固定图测试和 Mongo 集成测试通过。
