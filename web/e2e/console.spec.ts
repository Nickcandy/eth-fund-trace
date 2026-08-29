import { expect, test, type Page } from "@playwright/test";

const seed = "0x0000000000000000000000000000000000000001";
const downstream = "0x0000000000000000000000000000000000000002";
const traceJobID = "6a8a8c307fcbef52929d0d09";

async function mockAPI(page: Page) {
  const result = {
    nodes: [
      { chain: "ethereum", address: seed, depth: 0, terminal: false },
      { chain: "ethereum", address: downstream, depth: 6, terminal: false },
    ],
    edges: [
      {
        chain: "ethereum",
        txHash: `0x${"a".repeat(64)}`,
        from: seed,
        to: downstream,
        assetType: "eth",
        asset: "ETH",
        totalAmount: "1000000000000000000",
        transferCount: 1,
        kind: "transfer",
        depth: 6,
        path: [seed, downstream],
      },
    ],
    paths: [[seed, downstream]],
    dataThroughBlock: 20_000_000,
    dataStatus: "synced",
    labels: [
      {
        chain: "ethereum",
        address: seed,
        type: "hacker",
        source: "manual",
        riskLevel: "high",
        confidence: 1,
      },
    ],
    risk: {
      score: 70,
      level: "known_high",
      evidence: [],
      ruleVersion: "direct-label-v1",
    },
    ruleVersion: "trace-v1",
  };
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const json = (body: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    if (url.pathname === "/api/v1/trace")
      return json({ traceJobId: traceJobID, status: "queued" }, 202);
    if (url.pathname === `/api/v1/trace-jobs/${traceJobID}`)
      return json({
        id: traceJobID,
        chain: "ethereum",
        seedAddress: seed,
        direction: "both",
        depth: 0,
        asset: "ETH",
        status: "succeeded",
        createdAt: "2026-08-29T00:00:00Z",
        currentDepth: 6,
        visitedNodes: 2,
        edgeCount: 1,
        result,
        dataThroughBlock: result.dataThroughBlock,
        ruleVersion: "trace-v1",
        retryable: false,
      });
    if (url.pathname.endsWith("/extensions/latest"))
      return json(
        {
          error: { code: "not_found", message: "not found", retryable: false },
        },
        404,
      );
    if (url.pathname === "/api/v1/edges")
      return json({
        items: [],
        dataThroughBlock: result.dataThroughBlock,
        dataStatus: "synced",
      });
    if (url.pathname === "/api/v1/labels") return json(result.labels);
    if (url.pathname.includes("/profile"))
      return json({
        chain: "ethereum",
        chainId: 1,
        address: seed,
        ruleVersion: "hot-wallet-v1",
        dataThroughBlock: result.dataThroughBlock,
        features: {},
        score: 0,
        classification: "normal",
        suspectedHotWallet: false,
        computedAt: "2026-08-29T00:00:00Z",
      });
    if (url.pathname.includes("/balance"))
      return json({
        chain: "ethereum",
        chainId: 1,
        address: seed,
        asset: "ETH",
        amount: "0",
        decimals: 18,
        blockNumber: result.dataThroughBlock,
        fetchedAt: "2026-08-29T00:00:00Z",
      });
    if (url.pathname.includes("/addresses/"))
      return json({
        address: {
          chain: "ethereum",
          chainId: 1,
          address: seed,
          syncStatus: "synced",
        },
        labels: result.labels,
      });
    return json(
      { error: { code: "not_found", message: "not found", retryable: false } },
      404,
    );
  });
}

test.beforeEach(async ({ page }) => {
  await mockAPI(page);
  await page.goto("/");
});

test("runs Ethereum-only trace-v1 without bridge or propagation UI", async ({
  page,
}) => {
  await page.getByLabel("分析地址").fill(seed);
  await page.getByRole("button", { name: "开始分析" }).click();
  await expect(page.getByText("分析完成")).toBeVisible();
  await expect(page.getByLabel("网络")).toHaveValue("ethereum");
  await expect(page.getByLabel("网络")).toBeDisabled();
  await expect(page.getByText("BASE", { exact: true })).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: /桥接关系|写入证据/ }),
  ).toHaveCount(0);
  await expect(page.getByText(/风险传播/)).toHaveCount(0);
});

test("manual expansion can reveal a node beyond hop five", async ({ page }) => {
  await page.getByLabel("分析地址").fill(seed);
  await page.getByRole("button", { name: "开始分析" }).click();
  await expect(page.locator(".fund-node")).toHaveCount(1);
  await page.getByRole("button", { name: "展开下游" }).click();
  await expect(page.locator(".fund-node")).toHaveCount(2);
});
