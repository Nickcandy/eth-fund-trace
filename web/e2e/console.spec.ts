import { expect, test, type Page } from "@playwright/test";

const seed = "0x0000000000000000000000000000000000000001";
const upstream = "0x0000000000000000000000000000000000000002";
const downstream = "0x0000000000000000000000000000000000000003";
const baseAddress = "0x0000000000000000000000000000000000000004";
const token = "0x0000000000000000000000000000000000000010";
const traceJobID = "6a8a8c307fcbef52929d0d09";
let jobPolls = 0;

const transfer = (from: string, to: string, hash: string, source = "txlist", asset = "ETH", amount = "1000000000000000000") => ({ chain: "ethereum", chainId: 1, txHash: hash, blockNumber: 19876543, blockTime: "2026-08-22T10:00:00Z", from, to, assetType: source === "tokentx" ? "erc20" : "native", asset, symbol: source === "tokentx" ? "USDC" : "ETH", decimals: source === "tokentx" ? 6 : 18, amount, tokenMetadataComplete: true, source, traceId: "", logIndex: source === "tokentx" ? 1 : 0, transferKind: "transfer" });
const facts = [transfer(upstream, seed, "0x" + "a".repeat(64)), transfer(seed, downstream, "0x" + "b".repeat(64), "tokentx", token, "2500000")];
const aggregate = (fact: ReturnType<typeof transfer>, depth: number, path: string[]) => ({ chain: fact.chain, from: fact.from, to: fact.to, assetType: fact.assetType, asset: fact.asset, symbol: fact.symbol, decimals: fact.decimals, tokenMetadataComplete: true, totalAmount: fact.amount, transferCount: 1, kind: "transfer", depth, path });
const result = {
  nodes: [
    { chain: "ethereum", address: seed, depth: 0, terminal: false }, { chain: "ethereum", address: upstream, depth: 1, terminal: true },
    { chain: "ethereum", address: downstream, depth: 1, terminal: false }, { chain: "base", address: baseAddress, depth: 2, terminal: false },
  ],
  edges: [aggregate(facts[0], 1, [seed, upstream]), aggregate(facts[1], 1, [seed, downstream])],
  bridgeEdges: [{ depth: 2, path: [{ chain: "ethereum", address: downstream }, { chain: "base", address: baseAddress }], link: { sourceChain: "ethereum", sourceChainId: 1, sourceTxHash: "0x" + "c".repeat(64), sourceLogIndex: 2, sourceAddress: downstream, targetChain: "base", targetChainId: 8453, targetTxHash: "0x" + "d".repeat(64), targetLogIndex: 3, targetAddress: baseAddress, bridgeAddress: "0x0000000000000000000000000000000000000099", sourceAsset: token, sourceAmount: "2500000", targetAsset: token, targetAmount: "2490000", status: "confirmed", evidence: ["case-42"] } }],
  paths: [[seed, upstream], [seed, downstream]], crossChainPaths: [[{chain:"ethereum",address:seed},{chain:"base",address:baseAddress}]], dataThroughBlock: 19876543, dataThroughBlocks: { ethereum: 19876543, base: 18765432 }, dataStatus: "synced", labels: [{ address: upstream, type: "exchange", source: "propagation", confidence: .8, direction: "upstream", distance: 1, path: [seed, upstream], txHashes: [facts[0].txHash] }], risk: { score: 70, level: "known_high", inferredLabels: [], evidence: [{ address: seed, labelType: "phishing", baseScore: 70, score: 70, confidence: 1, distance: 0, direction: "direct", path: [seed], txHashes: [facts[0].txHash], rule: "risk-v1" }], ruleVersion: "risk-v1", propagationVersion: "propagation-v1" }, ruleVersion: "trace-v5",
};
const transactionAnalysis = {
  chain: "ethereum", chainId: 1, txHash: "0x" + "e".repeat(64), blockNumber: 19876543, from: seed,
  to: "0x68b3465833fb72a70ecdf485e0e4c7bd8665fc45", value: "0", input: "0x1234", succeeded: true,
  entryContract: "0x68b3465833fb72a70ecdf485e0e4c7bd8665fc45", entryContractName: "Uniswap SwapRouter02",
  transfers: [{ token, from: "0x0000000000000000000000000000000000000020", to: downstream, amount: "2490000", logIndex: 11 }],
  swaps: [
    { pool: "0x0000000000000000000000000000000000000020", protocol: "uniswap", version: "v3", verified: true, sender: seed, recipient: downstream, tokenIn: "0x0000000000000000000000000000000000000030", tokenOut: token, amountIn: "1000000000000000000", amountOut: "2500000", fee: 3000, logIndex: 10, outputAddress: downstream, evidence: ["receipt Swap log", "pool factory() matches official Uniswap V3 Factory"] },
  ],
  wraps: [{ type: "deposit", account: seed, amount: "1000000000000000000", logIndex: 4, evidence: "WETH contract receipt log" }],
  finalOutputAddress: downstream, quality: { status: "complete", ambiguousRoute: false, evidence: ["transaction", "receipt", "verified Uniswap V3 pool logs"] }, analyzedAt: "2026-08-24T00:00:00Z",
};

async function mockAPI(page: Page) {
  jobPolls = 0;
  const labels = [{chain:"ethereum",address:seed,type:"phishing",source:"manual" as const,riskLevel:"high" as const,confidence:1,evidence:["case-42"]}];
  await page.route("**/api/v1/**", async route => {
    const url = new URL(route.request().url()); const path = url.pathname;
    const json = (body: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
    if (path.startsWith("/api/v1/transactions/")) return json(transactionAnalysis);
    if (path === "/api/v1/propagation-jobs") return json({ id: "propagation-1", chain: "ethereum", targetAddress: seed, asset: "ETH", direction: "both", status: "queued", maxHops: 3, maxNodes: 10000, maxEdges: 50000, perNodeCandidateCap: 50, maxPathsPerTarget: 3, currentHop: 0, visitedNodes: 0, edgeCount: 0, dataThroughBlock: 19876543, ruleVersion: "risk-association-v2", propagationVersion: "propagation-v3", truncated: false, retryCount: 0, retryable: false }, 202);
    if (path === "/api/v1/propagation-jobs/propagation-1") return json({ id: "propagation-1", chain: "ethereum", targetAddress: seed, asset: "ETH", direction: "both", status: "succeeded", maxHops: 3, maxNodes: 10000, maxEdges: 50000, perNodeCandidateCap: 50, maxPathsPerTarget: 3, currentHop: 2, visitedNodes: 4, edgeCount: 3, dataThroughBlock: 19876543, ruleVersion: "risk-association-v2", propagationVersion: "propagation-v3", truncated: false, retryCount: 1, retryable: false, result: { status: "complete", score: 56, level: "suspected", directRisk: { present: false, score: 0, labels: [] }, nodes: [], associations: [{ sourceLabelId: "6a8a8c307fcbef52929d0d10", sourceAddress: upstream, sourceType: "phishing", targetChain: "ethereum", targetAddress: seed, direction: "in", asset: "ETH", confidence: 0.56, score: 56, distance: 1, level: "medium", path: { nodes: [`ethereum:${upstream}`, `ethereum:${seed}`], transactions: [], factors: { sourceBase: 100, labelConfidence: 0.8, hopFactor: 0.7, timeFactor: 1, amountFactor: 1, protocolFactor: 1 }, score: 56 }, paths: [[`ethereum:${upstream}`, `ethereum:${seed}`]], txHashes: [[facts[0].txHash]] }], coverage: [], missingAddresses: [], candidateCoverage: 1, ruleVersion: "risk-association-v2", propagationVersion: "propagation-v3", dataThroughBlock: 19876543, visitedNodes: 4, edgeCount: 3, truncated: false } });
    if (path === "/api/v1/trace") { const address = url.searchParams.get("address"); return json({ traceJobId: address?.endsWith("9") ? "job-fail" : address?.endsWith("8") ? "job-partial" : traceJobID, status: "queued" }, 202); }
    if (path === "/api/v1/trace-jobs/job-fail") return json({ id: "job-fail", chain: "ethereum", seedAddress: seed, direction: "both", depth: 3, topN: 10, asset: "all", status: "failed", createdAt: "2026-08-22T00:00:00Z", currentDepth: 1, visitedNodes: 2, edgeCount: 1, dataThroughBlock: 0, ruleVersion: "trace-v2", errorCode: "sync_failed", error: "上游同步失败", retryable: true });
    if (path === "/api/v1/trace-jobs/job-partial") return json({ id: "job-partial", chain: "ethereum", seedAddress: seed, direction: "both", depth: 3, topN: 10, asset: "all", status: "partial", createdAt: "2026-08-22T00:00:00Z", currentDepth: 2, visitedNodes: 4, edgeCount: 3, result, dataThroughBlock: 19876543, ruleVersion: "trace-v2", errorCode: "neighbor_sync_failed", error: "一个邻居同步失败", retryable: true });
    if (path === `/api/v1/trace-jobs/${traceJobID}`) { jobPolls++; return json({ id: traceJobID, chain: "ethereum", seedAddress: seed, direction: "both", depth: 3, topN: 10, asset: "all", status: jobPolls < 2 ? "running" : "succeeded", createdAt: "2026-08-22T00:00:00Z", currentDepth: jobPolls < 2 ? 1 : 3, visitedNodes: jobPolls < 2 ? 2 : 4, edgeCount: jobPolls < 2 ? 1 : 3, result: jobPolls < 2 ? undefined : result, dataThroughBlock: 19876543, ruleVersion: "trace-v2", retryable: false }); }
    if (path.includes("/profile")) return json({ chain:"ethereum",chainId:1,address:seed,ruleVersion:"hot-wallet-v1",dataThroughBlock:19876543,features:{lifetimeTransfers:320,lifetimeIncoming:160,lifetimeOutgoing:160,windowTransfers:120,incoming:60,outgoing:60,uniqueCounterparties:80,uniqueSenders:40,uniqueRecipients:40,activeDays:22,ethTransfers:80,erc20Transfers:40},score:72,classification:"suspected_hot_wallet",suspectedHotWallet:true,computedAt:"2026-08-22T00:00:00Z" });
    if (path === "/api/v1/edges") return json({ items: facts, dataThroughBlock: 19876543, dataStatus: "synced" });
    if (path === "/api/v1/bridge-links" && route.request().method() === "GET") return json({ items: [result.bridgeEdges[0].link] });
    if (path === "/api/v1/labels" && route.request().method() === "GET") return json(labels.filter(label => label.address === url.searchParams.get("address")));
    if (path === "/api/v1/labels") {
      const label = route.request().postDataJSON();
      labels.push(label);
      return json({ id: "6a8a8c307fcbef52929d0d11", ...label }, 201);
    }
    if (path === "/api/v1/bridge-links") return json({}, 201);
    if (path.includes("/addresses/")) return json({ address: { chain:"ethereum",chainId:1,address:seed,isContract:false,isTerminal:false,earliestSyncedBlock:0,historySyncedToBlock:19876543,latestSyncedBlock:19876543,lastSyncedAt:"2026-08-22T00:00:00Z",syncStatus:"synced" }, labels: [] });
    return json({ error: { code: "not_found", message: "not found", retryable: false } }, 404);
  });
}

test.beforeEach(async ({ page }) => { await mockAPI(page); await page.goto("/"); });

test("creates an async trace and renders upstream, downstream, and bridge evidence", async ({ page }) => {
  await page.getByLabel("分析地址").fill(seed); await page.getByRole("button", { name: "开始分析" }).click();
  await expect(page.getByText("分析完成")).toBeVisible({ timeout: 10_000 });
  await expect(page.locator(".fund-node")).toHaveCount(4); await expect(page.getByText("ETHEREUM", { exact: true })).toBeVisible(); await expect(page.getByText("BASE", { exact: true })).toBeVisible();
  const box = await page.getByTestId("graph-canvas").boundingBox(); expect(box?.width).toBeGreaterThan(250); expect(box?.height).toBeGreaterThan(250);
  await page.locator(".edge-label").first().click(); await expect(page.getByRole("heading", { name: "资金边详情" })).toBeVisible(); await expect(page.locator(".edge-summary strong")).toContainText("1 笔 ETH");
  await expect(page.locator(".react-flow__edge").last().locator(".react-flow__edge-path")).toHaveCSS("stroke-dasharray", "8px, 6px");
  await page.getByRole("button", { name: /桥接关系/ }).click(); await expect(page.getByText("Ethereum → Base")).toBeVisible();
  await page.screenshot({ path: test.info().outputPath("analysis-console.png"), fullPage: true });
});

test("automatically assesses target risk after a trace completes", async ({ page }) => {
  const request = page.waitForRequest(value => value.url().endsWith("/api/v1/propagation-jobs") && value.method() === "POST");
  await page.getByLabel("分析地址").fill(seed); await page.getByRole("button", { name: "开始分析" }).click();
  await expect(page.getByText("分析完成")).toBeVisible({ timeout: 10_000 });
  expect((await request).postDataJSON()).toEqual({ chain: "ethereum", targetAddress: seed, direction: "both", asset: "ETH" });
  await expect(page.getByText("风险传播 succeeded · 4 节点")).toBeVisible();
  await expect(page.getByText("完整检查 · 1 条路径证据")).toBeVisible();
});

test("saves a deterministic label for the selected graph node", async ({ page }) => {
  await page.getByLabel("分析地址").fill(seed); await page.getByRole("button", { name: "开始分析" }).click();
  await expect(page.getByText("分析完成")).toBeVisible({ timeout: 10_000 });
  await page.locator(`.node-address[title="${downstream}"]`).click();
  await page.getByRole("tab", { name: "标签" }).click();
  await expect(page.getByPlaceholder("标签类型，如 exchange")).toBeInViewport();
  await page.getByPlaceholder("标签类型，如 exchange").fill("sanctions");
  await page.getByLabel("来源").selectOption("public-list");
  await page.getByLabel("风险等级").selectOption("high");
  await page.getByLabel("置信度").fill("0.85");
  await page.getByPlaceholder("证据，每行一条").fill("public-list:2026-08-26\ncase-84");
  await page.getByPlaceholder("备注").fill("verified source");
  const request = page.waitForRequest(value => value.url().endsWith("/api/v1/labels") && value.method() === "POST");
  await page.getByRole("button", { name: "保存标签" }).click();
  expect((await request).postDataJSON()).toEqual({ chain: "ethereum", address: downstream, type: "sanctions", source: "public-list", riskLevel: "high", confidence: 0.85, note: "verified source", evidence: ["public-list:2026-08-26", "case-84"] });
  await expect(page.getByText("标签已保存，重新追踪后传播生效")).toBeVisible();
  await expect(page.getByText("sanctions")).toBeVisible();
  await expect(page.getByText("85%")).toBeVisible();
});

test("restores the active trace job after a page refresh", async ({ page }) => {
  await page.getByLabel("分析地址").fill(seed);
  await page.getByRole("button", { name: "开始分析" }).click();
  await expect(page.getByText("分析完成")).toBeVisible({ timeout: 10_000 });
  await expect(page).toHaveURL(new RegExp(`traceJobId=${traceJobID}`));
  await page.reload();
  await expect(page.getByText("分析完成")).toBeVisible({ timeout: 10_000 });
  await expect(page.locator(".fund-node")).toHaveCount(4);
});

test("keeps the analysis controls usable on every viewport", async ({ page }) => {
  await expect(page.getByRole("button", { name: "开始分析" })).toBeVisible();
  const viewport = page.viewportSize()!; const button = await page.getByRole("button", { name: "开始分析" }).boundingBox();
  expect(button!.x + button!.width).toBeLessThanOrEqual(viewport.width + 1);
});

test("shows a retry action for a failed trace job", async ({ page }) => {
  await page.getByLabel("分析地址").fill("0x0000000000000000000000000000000000000009");
  await page.getByRole("button", { name: "开始分析" }).click();
  await expect(page.getByText("分析失败")).toBeVisible();
  await expect(page.getByText("上游同步失败")).toBeVisible();
  await expect(page.getByRole("button", { name: "重试分析" })).toBeVisible();
});

test("keeps partial trace results visible with a warning", async ({ page }) => {
  await page.getByLabel("分析地址").fill("0x0000000000000000000000000000000000000008");
  await page.getByRole("button", { name: "开始分析" }).click();
  await expect(page.getByText("部分数据可用")).toBeVisible();
  await expect(page.locator(".fund-node")).toHaveCount(4);
});

test("analyzes a V3 transaction and continues with the output address", async ({ page }) => {
  await page.getByRole("button", { name: "交易哈希" }).click();
  await page.getByLabel("交易哈希").fill(transactionAnalysis.txHash);
  await page.getByRole("button", { name: "开始分析" }).click();
  await expect(page.getByRole("heading", { name: "Uniswap SwapRouter02" })).toBeVisible();
  await expect(page.getByText("Uniswap V3", { exact: true })).toBeVisible();
  await expect(page.getByText("包装 ETH")).toBeVisible();
  const view = page.locator(".transaction-view");
  const box = await view.boundingBox(); const viewport = page.viewportSize()!;
  expect(box!.x).toBeGreaterThanOrEqual(0); expect(box!.x + box!.width).toBeLessThanOrEqual(viewport.width + 1);
  await page.getByRole("button", { name: /继续追踪/ }).click();
  await expect(page.getByLabel("分析地址")).toHaveValue(downstream);
  await expect(page.getByText("分析完成")).toBeVisible({ timeout: 10_000 });
});
