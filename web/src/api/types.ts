export type Chain = "ethereum" | "bitcoin";
export type Direction = "in" | "out" | "both";
export type JobStatus =
  | "queued"
  | "waiting_sync"
  | "running"
  | "succeeded"
  | "partial"
  | "failed"
  | "stopped";

export interface ApiErrorBody {
  error: { code: string; message: string; retryable: boolean };
}

export interface Transfer {
  chain: Chain;
  chainId: number;
  txHash: string;
  blockNumber: number;
  blockTime?: string;
  from: string;
  to: string;
  assetType: string;
  asset: string;
  symbol?: string;
  decimals?: number;
  amount?: string;
  tokenValue?: string;
  tokenName?: string;
  transferKind?: "transfer" | "mint" | "burn" | string;
  transactionGroup?: string;
  tokenMetadataComplete?: boolean;
  source: string;
  traceId: string;
  logIndex: number;
}

export interface TraceEdge {
  chain: Chain;
  sourceChain?: Chain;
  targetChain?: Chain;
  from: string;
  to: string;
  assetType: string;
  asset: string;
  symbol?: string;
  decimals?: number;
  tokenMetadataComplete?: boolean;
  totalAmount: string;
  transferCount: number;
  kind: string;
  depth: number;
  path: string[];
  txHash?: string;
  sourceTxHash?: string;
  sourceAmount?: string;
  sourceAsset?: string;
  firstBlock?: number;
  firstTime?: string;
  latestBlock?: number;
  latestTime?: string;
  conversionStatus?: "complete" | "partial";
  conversionScanned?: number;
  conversionEvidence?: ConversionEvidence[];
  protocol?: string;
  protocolAction?: string;
  protocolMemo?: string;
}
export interface ConversionEvidence {
  txHash: string;
  protocol: string;
  version: string;
  status: "complete" | "partial";
  initiator?: string;
  router?: string;
  executor?: string;
  liquidityProvider?: string;
  recipient?: string;
  tokenIn?: string;
  amountIn?: string;
  tokenOut?: string;
  amountOut?: string;
  evidence: string[];
}
export interface TraceNode {
  chain: Chain;
  address: string;
  depth: number;
  terminal: boolean;
  stopReason?: string;
  addressType?: "unknown" | "eoa" | "contract";
  protocol?: "unknown" | string;
  roles?: string[];
}
export interface MoneyState {
  chain: Chain;
  address: string;
  direction: Direction;
  assetType: string;
  asset: string;
  amount: string;
  remainingAmount: string;
  entryTxHash?: string;
  entryBlock?: number;
  path: string[];
  evidence?: string;
  inferred?: boolean;
  stopReason?: string;
}
export interface MoneyTransfer {
  chain: Chain;
  from: string;
  to: string;
  asset: string;
  amount: string;
  txHash: string;
  kind: string;
  blockNumber: number;
  evidence?: string;
  inferred?: boolean;
  stopReason?: string;
}
export interface AssetLedger {
  address: string;
  asset: string;
  openingAmount: string;
  incomingAmount: string;
  outgoingAmount: string;
  explainedAmount: string;
  unexplainedAmount: string;
  status: string;
}
export interface NodeRef {
  chain: Chain;
  address: string;
}

export interface RiskEvidence {
  address: string;
  labelType: string;
  baseScore: number;
  score: number;
  confidence: number;
  distance: number;
  direction: string;
  path: string[];
  txHashes: string[];
  evidence?: string[];
  rule: string;
}

export interface RiskResult {
  score: number;
  level: "known_high" | "suspected" | "no_conclusion" | string;
  evidence?: RiskEvidence[];
  ruleVersion: string;
}

export interface TraceResult {
  nodes: TraceNode[];
  edges: TraceEdge[];
  paths?: string[][];
  dataThroughBlock: number;
  dataThroughBlocks?: Record<string, number>;
  dataStatus: string;
  labels?: Label[];
  risk: RiskResult;
  ruleVersion: string;
  moneyStates?: MoneyState[];
  moneyTransfers?: MoneyTransfer[];
  ledgers?: AssetLedger[];
  reconciliation?: string;
}

export interface TraceJob {
  id: string;
  chain: Chain;
  seedAddress: string;
  direction: Direction;
  depth: number;
  asset: string;
  status: JobStatus;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  currentDepth: number;
  visitedNodes: number;
  edgeCount: number;
  syncJobIds?: string[];
  result?: TraceResult;
  dataThroughBlock: number;
  ruleVersion: string;
  errorCode?: string;
  error?: string;
  retryable: boolean;
  rootTraceJobId?: string;
  extensionAddress?: string;
  extensionDirection?: "in" | "out";
}

export interface TraceAccepted {
  traceJobId: string;
  status: JobStatus;
}
export interface TraceExtensionRequest {
  address: string;
  direction: "in" | "out";
  depth: 1;
}

export interface SyncJob {
  jobId: string;
  chain: Chain;
  address: string;
  status: string;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  safeHead: number;
  totalAddresses: number;
  completedAddresses: number;
  processedAddresses: number;
  cachedAddresses: number;
  fetched: number;
  actionCounts?: Record<string, number>;
  successfulNeighbors?: string[];
  failedNeighbors?: Array<{
    address: string;
    code: string;
    message: string;
    retryable: boolean;
  }>;
  maxRecordsPerAction?: number;
  truncatedActions?: string[];
  progress?: {
    currentAddress?: string;
    currentAction?: string;
    rangeStart?: number;
    rangeEnd?: number;
    currentPage?: number;
    pagesFetched: number;
    recordsRead: number;
    recordsWritten: number;
    splitCount: number;
    updatedAt?: string;
  };
  error?: string;
  errorCode?: string;
  retryable: boolean;
}

export interface AddressMetadata {
  chain: Chain;
  chainId: number;
  address: string;
  isContract: boolean;
  isTerminal: boolean;
  addressType: "unknown" | "eoa" | "contract";
  protocol?: string;
  roles?: string[];
  normalSyncedFrom?: number;
  normalSyncedTo?: number;
  internalSyncedFrom?: number;
  internalSyncedTo?: number;
  tokenSyncedFrom?: number;
  tokenSyncedTo?: number;
  lastSyncedAt?: string;
  syncStatus: string;
  syncError?: string;
  syncMaxRecordsPerAction?: number;
}

export interface Label {
  chain: Chain;
  chainId?: number;
  address: string;
  type: string;
  riskLevel?: "low" | "medium" | "high";
  confidence: number;
  source: "manual" | "public-list";
  note?: string;
  evidence?: string[];
  observedAt?: string;
}

export interface AddressResponse {
  address: AddressMetadata;
  labels: Label[];
}

export interface NativeBalance {
  chain: Chain;
  chainId: number;
  address: string;
  asset: "ETH";
  amount: string;
  decimals: 18;
  blockNumber: number;
  fetchedAt: string;
}

export interface ProfileFeatures {
  lifetimeTransfers: number;
  lifetimeIncoming: number;
  lifetimeOutgoing: number;
  windowTransfers: number;
  incoming: number;
  outgoing: number;
  uniqueCounterparties: number;
  uniqueSenders: number;
  uniqueRecipients: number;
  activeDays: number;
  ethTransfers: number;
  erc20Transfers: number;
}

export interface AddressProfile {
  chain: Chain;
  chainId: number;
  address: string;
  ruleVersion: string;
  dataThroughBlock: number;
  windowStart?: string;
  windowEnd?: string;
  features: ProfileFeatures;
  score: number;
  classification: string;
  suspectedHotWallet: boolean;
  signals?: string[];
  computedAt: string;
}

export interface EdgePage {
  items: Transfer[];
  nextCursor?: string;
  dataThroughBlock: number;
  dataStatus: string;
}

export interface TraceQuery {
  chain: Chain;
  address: string;
  direction: Direction;
  depth: number;
  asset: string;
}

export interface ReceiptTransfer {
  token: string;
  from: string;
  to: string;
  amount: string;
  logIndex: number;
}

export interface SwapEvent {
  pool: string;
  protocol?: string;
  version?: string;
  verified: boolean;
  sender: string;
  recipient: string;
  tokenIn?: string;
  tokenOut?: string;
  amountIn?: string;
  amountOut?: string;
  fee?: number;
  logIndex: number;
  outputAddress?: string;
  evidence: string[];
}

export interface WrapEvent {
  type: "deposit" | "withdrawal";
  account: string;
  amount: string;
  logIndex: number;
  evidence: string;
}

export interface TransactionAnalysis {
  analysisVersion: string;
  chain: "ethereum";
  chainId: 1;
  txHash: string;
  blockNumber: number;
  from: string;
  to: string;
  value: string;
  input: string;
  succeeded: boolean;
  entryContract?: string;
  entryContractName?: string;
  transfers: ReceiptTransfer[];
  swaps: SwapEvent[];
  wraps: WrapEvent[];
  internalCalls: Array<{
    from: string;
    to: string;
    value: string;
    type: string;
    traceId: string;
    isError: boolean;
  }>;
  conversions: Array<{
    protocol: string;
    version: string;
    status: "complete" | "partial";
    initiator?: string;
    router?: string;
    executor?: string;
    liquidityProvider?: string;
    recipient?: string;
    tokenIn?: string;
    amountIn?: string;
    tokenOut?: string;
    amountOut?: string;
    evidence: string[];
    issues?: string[];
  }>;
  finalOutputAddress?: string;
  protocolAction?:
    | "router_inbound"
    | "vault_migration"
    | "protocol_outbound"
    | "cross_chain_swap"
    | "relay_cross_chain_transfer"
    | "bittorrent_bridge_inbound"
    | "refund"
    | "protocol_internal";
  protocolMemo?: string;
  protocolDestination?: string;
  protocolAsset?: string;
  protocolAmount?: string;
  crossChain?: {
    protocol: string;
    status: "complete";
    requestId: string;
    sourceChain: string;
    sourceChainId: number;
    targetChain: string;
    targetChainId: number;
    sourceTxHash: string;
    targetTxHash: string;
    from: string;
    to: string;
    sourceAsset: string;
    sourceAmount: string;
    targetAsset: string;
    targetAmount: string;
    feeAmount: string;
  };
  quality: {
    status: "complete" | "partial";
    ambiguousRoute: boolean;
    evidence: string[];
    issues?: string[];
  };
  analyzedAt: string;
}

export interface LabelInput {
  chain: Chain;
  address: string;
  type: string;
  riskLevel?: "low" | "medium" | "high";
  confidence: number;
  source: "manual" | "public-list";
  note?: string;
  evidence: string[];
}
