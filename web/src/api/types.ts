export type Chain = "ethereum" | "base";
export type Direction = "in" | "out" | "both";
export type JobStatus = "queued" | "waiting_sync" | "running" | "succeeded" | "partial" | "failed";

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

export interface TraceEdge { transfer: Transfer; depth: number; path: string[] }
export interface TraceNode { chain: Chain; address: string; depth: number; terminal: boolean }
export interface NodeRef { chain: Chain; address: string }

export interface CrossChainLink {
  id?: string;
  sourceChain: Chain;
  sourceChainId?: number;
  sourceTxHash: string;
  sourceLogIndex: number;
  sourceAddress: string;
  targetChain: Chain;
  targetChainId?: number;
  targetTxHash: string;
  targetLogIndex: number;
  targetAddress: string;
  bridgeAddress: string;
  sourceAsset: string;
  sourceAmount: string;
  targetAsset: string;
  targetAmount: string;
  status?: string;
  evidence: string[];
  observedAt?: string;
}

export interface BridgeEdge { link: CrossChainLink; depth: number; path: NodeRef[] }

export interface InferredLabel {
  address: string; type: string; source: "propagation"; confidence: number;
  direction: string; distance: number; path: string[]; paths?: string[][]; txHashes: string[]; evidence?: string[];
}

export interface RiskEvidence {
  address: string; labelType: string; baseScore: number; score: number; confidence: number;
  distance: number; direction: string; path: string[]; txHashes: string[]; evidence?: string[]; rule: string;
}

export interface RiskResult {
  score: number;
  level: "known_high" | "suspected" | "no_conclusion" | string;
  inferredLabels?: InferredLabel[];
  evidence?: RiskEvidence[];
  ruleVersion: string;
  propagationVersion: string;
}

export interface TraceResult {
  nodes: TraceNode[];
  edges: TraceEdge[];
  bridgeEdges?: BridgeEdge[];
  crossChainPaths?: NodeRef[][];
  paths?: string[][];
  dataThroughBlock: number;
  dataThroughBlocks?: Record<string, number>;
  dataStatus: string;
  labels?: InferredLabel[];
  risk: RiskResult;
  ruleVersion: string;
}

export interface TraceJob {
  id: string;
  chain: Chain;
  seedAddress: string;
  direction: Direction;
  depth: number;
  topN: number;
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
}

export interface TraceAccepted { traceJobId: string; status: JobStatus }

export interface SyncJob {
  jobId: string; chain: Chain; address: string; status: string; createdAt: string; startedAt?: string; finishedAt?: string;
  safeHead: number; totalAddresses: number; completedAddresses: number; cachedAddresses: number; fetched: number;
  actionCounts?: Record<string, number>; successfulNeighbors?: string[]; failedNeighbors?: Array<{ address: string; code: string; message: string; retryable: boolean }>;
  error?: string; errorCode?: string; retryable: boolean;
}

export interface AddressMetadata {
  chain: Chain; chainId: number; address: string; isContract: boolean; isTerminal: boolean;
  earliestSyncedBlock: number; historySyncedToBlock: number; latestSyncedBlock: number; lastSyncedAt?: string;
  syncStatus: string; syncError?: string;
}

export interface Label {
  chain: Chain; chainId?: number; address: string; type: string; riskLevel?: "low" | "medium" | "high";
  confidence: number; source: "manual" | "public-list"; note?: string; evidence?: string[]; observedAt?: string;
}

export interface AddressResponse { address: AddressMetadata; labels: Label[] }

export interface ProfileFeatures {
  lifetimeTransfers: number; lifetimeIncoming: number; lifetimeOutgoing: number; windowTransfers: number;
  incoming: number; outgoing: number; uniqueCounterparties: number; uniqueSenders: number; uniqueRecipients: number;
  activeDays: number; ethTransfers: number; erc20Transfers: number;
}

export interface AddressProfile {
  chain: Chain; chainId: number; address: string; ruleVersion: string; dataThroughBlock: number;
  windowStart?: string; windowEnd?: string; features: ProfileFeatures; score: number; classification: string;
  suspectedHotWallet: boolean; signals?: string[]; computedAt: string;
}

export interface EdgePage { items: Transfer[]; nextCursor?: string; dataThroughBlock: number; dataStatus: string }
export interface BridgePage { items: CrossChainLink[] }

export interface TraceQuery {
  chain: Chain; address: string; direction: Direction; depth: number; topN: number; asset: string;
}

export interface LabelInput {
  chain: Chain; address: string; type: string; riskLevel?: "low" | "medium" | "high"; confidence: number;
  source: "manual" | "public-list"; note?: string; evidence: string[];
}

export type BridgeInput = Omit<CrossChainLink, "id" | "sourceChainId" | "targetChainId" | "status" | "observedAt">;
