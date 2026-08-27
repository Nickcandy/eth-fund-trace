export type Chain = "ethereum" | "base";
export type Direction = "in" | "out" | "both";
export type JobStatus = "queued" | "waiting_sync" | "running" | "succeeded" | "partial" | "failed" | "stopped";

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
  chain: Chain; from: string; to: string; assetType: string; asset: string; symbol?: string; decimals?: number;
  tokenMetadataComplete?: boolean; totalAmount: string; transferCount: number; kind: string; depth: number; path: string[];
  firstBlock?: number; firstTime?: string; latestBlock?: number; latestTime?: string;
  conversionStatus?: "complete" | "partial"; conversionScanned?: number;
	conversionEvidence?: ConversionEvidence[];
}
export interface ConversionEvidence {
	txHash: string; protocol: string; version: string; status: "complete" | "partial";
	initiator?: string; router?: string; executor?: string; liquidityProvider?: string; recipient?: string;
	tokenIn?: string; amountIn?: string; tokenOut?: string; amountOut?: string; evidence: string[];
}
export interface TraceNode { chain: Chain; address: string; depth: number; terminal: boolean; stopReason?: string; addressType?: "unknown" | "eoa" | "contract"; protocol?: "unknown" | string; roles?: string[] }
export interface MoneyState { chain: Chain; address: string; direction: Direction; assetType: string; asset: string; amount: string; remainingAmount: string; entryTxHash?: string; entryBlock?: number; path: string[]; evidence?: string; inferred?: boolean; stopReason?: string }
export interface MoneyTransfer { chain: Chain; from: string; to: string; asset: string; amount: string; txHash: string; kind: string; blockNumber: number; evidence?: string; inferred?: boolean; stopReason?: string }
export interface AssetLedger { address: string; asset: string; openingAmount: string; incomingAmount: string; outgoingAmount: string; explainedAmount: string; unexplainedAmount: string; status: string }
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
  targetTxHash?: string;
  targetLogIndex: number;
  targetAddress: string;
  bridgeAddress: string;
  sourceAsset: string;
  sourceAmount: string;
  targetAsset: string;
  targetAmount: string;
  status?: string;
  identityKey?: string;
  protocol?: string;
  direction?: "deposit" | "withdrawal";
  messageHash?: string;
  nonce?: string;
  sourceBlock?: number;
  targetBlock?: number;
  evidenceLevel?: "confirmed" | "strong" | "partial";
  lastCheckedAt?: string;
  nextCheckAt?: string;
  adapterVersion?: string;
  retryCount?: number;
  lastErrorCode?: string;
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
  moneyStates?: MoneyState[]; moneyTransfers?: MoneyTransfer[]; ledgers?: AssetLedger[]; reconciliation?: string;
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
}

export interface TraceAccepted { traceJobId: string; status: JobStatus }

export interface PropagationCoverage {
  chain: Chain; address: string; direction: "in" | "out"; asset: string;
  selectedCounterparties: number; totalCounterparties: number; selectedAmount: string; totalAmount: string;
  amountCoverage: string; truncated: boolean; truncationReason?: string;
  latestTime?: string; latestTxHash?: string;
}

export interface ScoreFactors { sourceBase: number; labelConfidence: number; hopFactor: number; timeFactor: number; amountFactor: number; protocolFactor: number }
export interface PathEvidence { nodes: string[]; transactions: Array<{ chain: Chain; txHash: string; blockNumber: number; blockTime?: string; from: string; to: string; amount: string; protocol?: string }>; factors: ScoreFactors; score: number }
export interface NodeRiskAssessment { chain: Chain; address: string; status: "complete" | "partial" | "unknown"; score: number; level: string; directRisk: { present: boolean; score: number; labels: Label[] }; associations: RiskAssociation[] }

export interface RiskAssociation {
  sourceLabelId: string; sourceAddress: string; sourceType: string; targetChain: Chain; targetAddress: string;
  direction: "in" | "out"; asset: string; confidence: number; score: number; distance: number;
  level: "strong" | "medium" | "weak"; path: PathEvidence; paths: string[][]; txHashes: string[][]; cycleDetected?: boolean;
}

export interface PropagationResult {
  status: "complete" | "partial" | "unknown"; score: number; level: string; directRisk: { present: boolean; score: number; labels: Label[] };
  nodes: NodeRiskAssessment[]; associations: RiskAssociation[]; coverage: PropagationCoverage[]; missingAddresses: string[]; candidateCoverage: number;
  ruleVersion: "risk-association-v2"; propagationVersion: "propagation-v4"; dataThroughBlock: number; visitedNodes: number; edgeCount: number;
  truncated: boolean; truncationReason?: string;
}

export interface PropagationJob {
  id: string; chain: Chain; targetAddress: string; asset: string; direction: Direction;
  status: Exclude<JobStatus, "waiting_sync">; maxHops: number; maxNodes: number; maxEdges: number; maxAssetChannels: number;
  perNodeCandidateCap: number; maxPathsPerTarget: number; currentHop: number; visitedNodes: number; edgeCount: number;
  dataThroughBlock: number; ruleVersion: string; propagationVersion: string; truncated: boolean; truncationReason?: string;
  retryCount: number; createdAt: string; startedAt?: string; finishedAt?: string; result?: PropagationResult;
  errorCode?: string; error?: string; retryable: boolean;
}

export interface PropagationRequest { chain: Chain; targetAddress: string; direction: Direction; asset: string }

export interface SyncJob {
  jobId: string; chain: Chain; address: string; status: string; createdAt: string; startedAt?: string; finishedAt?: string;
  safeHead: number; totalAddresses: number; completedAddresses: number; processedAddresses: number; cachedAddresses: number; fetched: number;
  actionCounts?: Record<string, number>; successfulNeighbors?: string[]; failedNeighbors?: Array<{ address: string; code: string; message: string; retryable: boolean }>;
	maxRecordsPerAction?: number; truncatedActions?: string[];
  progress?: {
    currentAddress?: string; currentAction?: string; rangeStart?: number; rangeEnd?: number; currentPage?: number;
    pagesFetched: number; recordsRead: number; recordsWritten: number; splitCount: number; updatedAt?: string;
  };
  error?: string; errorCode?: string; retryable: boolean;
}

export interface AddressMetadata {
  chain: Chain; chainId: number; address: string; isContract: boolean; isTerminal: boolean;
	addressType: "unknown" | "eoa" | "contract"; protocol?: string; roles?: string[];
  earliestSyncedBlock: number; historySyncedToBlock: number; latestSyncedBlock: number; lastSyncedAt?: string;
	  syncStatus: string; syncError?: string; syncMaxRecordsPerAction?: number;
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
	chain: Chain; address: string; direction: Direction; depth: number; asset: string;
}

export interface ReceiptTransfer {
  token: string; from: string; to: string; amount: string; logIndex: number;
}

export interface SwapEvent {
  pool: string; protocol?: string; version?: string; verified: boolean; sender: string; recipient: string;
  tokenIn?: string; tokenOut?: string; amountIn?: string; amountOut?: string; fee?: number; logIndex: number;
  outputAddress?: string; evidence: string[];
}

export interface WrapEvent {
  type: "deposit" | "withdrawal"; account: string; amount: string; logIndex: number; evidence: string;
}

export interface TransactionAnalysis {
	analysisVersion: string;
  chain: "ethereum"; chainId: 1; txHash: string; blockNumber: number; from: string; to: string; value: string;
  input: string; succeeded: boolean; entryContract?: string; entryContractName?: string; transfers: ReceiptTransfer[];
	swaps: SwapEvent[]; wraps: WrapEvent[]; internalCalls: Array<{ from: string; to: string; value: string; type: string; traceId: string; isError: boolean }>;
	conversions: Array<{ protocol: string; version: string; status: "complete" | "partial"; initiator?: string; router?: string; executor?: string; liquidityProvider?: string; recipient?: string; tokenIn?: string; amountIn?: string; tokenOut?: string; amountOut?: string; evidence: string[]; issues?: string[] }>;
	finalOutputAddress?: string;
	protocolAction?: "vault_migration" | "protocol_outbound" | "cross_chain_swap" | "refund" | "protocol_internal";
	protocolMemo?: string; protocolDestination?: string;
  quality: { status: "complete" | "partial"; ambiguousRoute: boolean; evidence: string[]; issues?: string[] };
  analyzedAt: string;
}

export interface LabelInput {
  chain: Chain; address: string; type: string; riskLevel?: "low" | "medium" | "high"; confidence: number;
  source: "manual" | "public-list"; note?: string; evidence: string[];
}

export type BridgeInput = Pick<CrossChainLink, "sourceChain"|"sourceTxHash"|"sourceLogIndex"|"sourceAddress"|"targetChain"|"targetTxHash"|"targetLogIndex"|"targetAddress"|"bridgeAddress"|"sourceAsset"|"sourceAmount"|"targetAsset"|"targetAmount"|"evidence">;
export interface BridgeAnalysisInput { chain: Chain; txHash: string }
export interface BridgeSyncAccepted { status: "queued"; linkId: string }
