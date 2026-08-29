# Demo 运行手册

## 启动

复制 `.env.example` 为本地环境文件并配置 Ethereum 的 Etherscan API Key、Ethereum RPC 和 MongoDB。随后执行：

```powershell
docker compose --profile app up -d --build
```

服务默认监听 `http://localhost:8080`，健康检查为 `GET /healthz`。

生产配置使用 `ETHEREUM_SYNC_START_BLOCK=21525891` 和 `ETHEREUM_SYNC_END_BLOCK=25860787`，对应北京时间 2025-01-01 至 2026-08-29。修改截止日期时应先把日期换算为 Ethereum 区块并更新 `ETHEREUM_SYNC_END_BLOCK`；已同步地址只补新增加的区间。

## 演示流程

1. 输入 Ethereum 地址并选择方向、资产和初始深度。
2. 创建 `trace-v1`，观察同步、等待、运行、完成或部分完成状态。
3. 查看节点类型、协议、终点原因、直接标签风险和当前余额。
4. 点击节点的“展开上游”或“展开下游”，按需加载下一段路径。
5. 在运行中的 Trace 或扩展任务上点击停止，确认任务进入 `stopped`。
6. 打开交易详情，检查 Transfer、Swap 或 THORChain 语义。

前端不会按 5 跳隐藏手动扩展结果。若扩展没有新边，页面必须显示终点原因、数据不足或错误，不能只重排画布。

## 高频地址

任一 Etherscan action 达到 50,000 条时，同步任务返回部分结果并标记 `high_frequency`。这不是失败，也不是风险标签。Trace 保留到达该地址的边，但不会继续拉取它的全部历史。

## 当前不演示

- Base；
- 桥接目标链、桥消息状态或跨链边；
- 多跳风险传播和标签传染；
- 旧 Trace 结果升级。

旧的非 `trace-v1` 任务应删除后重新执行。

## 验收

```powershell
go test ./...
cd web
npm test -- --run
npm run build
npm run test:e2e
```

验收同时检查 `/healthz`、Ethereum-only 网络选择、手动扩展超过 5 跳、任务停止、桥终点和直接标签风险。
