import type {
  AddressProfile, AddressResponse, BridgeAnalysisInput, BridgeInput, BridgePage, BridgeSyncAccepted, CrossChainLink, EdgePage, Label, LabelInput,
  SyncJob, TraceAccepted, TraceJob, TraceQuery, TransactionAnalysis,
} from "./types";

export class ApiError extends Error {
  constructor(public readonly status: number, public readonly code: string, message: string, public readonly retryable: boolean) {
    super(message);
  }
}

let bearerToken = "";
export function setBearerToken(value: string) { bearerToken = value.trim(); }

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(path, { ...init, headers: { "Content-Type": "application/json", ...(bearerToken ? { Authorization: `Bearer ${bearerToken}` } : {}), ...init.headers } });
  if (!response.ok) {
    const body = await response.json().catch(() => undefined) as { error?: { code?: string; message?: string; retryable?: boolean } } | undefined;
    throw new ApiError(response.status, body?.error?.code ?? "http_error", body?.error?.message ?? `HTTP ${response.status}`, body?.error?.retryable ?? response.status >= 500);
  }
  return response.json() as Promise<T>;
}

function queryString(values: object): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(values as Record<string, unknown>)) if (value !== undefined && value !== "") params.set(key, String(value));
  return params.toString();
}

async function requestSyncJobs(ids: string[], signal?: AbortSignal): Promise<SyncJob[]> {
  const jobs: SyncJob[] = [];
  const batchSize = 5;
  for (let offset = 0; offset < ids.length; offset += batchSize) {
    const batch = ids.slice(offset, offset + batchSize);
    const results = await Promise.allSettled(batch.map((id) => request<SyncJob>(`/api/v1/sync-jobs/${encodeURIComponent(id)}`, { signal })));
    for (const result of results) if (result.status === "fulfilled") jobs.push(result.value);
    if (offset + batchSize < ids.length) await abortableDelay(400, signal);
  }
  return jobs;
}

function abortableDelay(milliseconds: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) return reject(signal.reason);
    const timer = window.setTimeout(resolve, milliseconds);
    signal?.addEventListener("abort", () => { window.clearTimeout(timer); reject(signal.reason); }, { once: true });
  });
}

export const api = {
  createTrace: (query: TraceQuery, signal?: AbortSignal) => request<TraceAccepted>(`/api/v1/trace?${queryString(query)}`, { signal }),
  traceJob: (id: string, signal?: AbortSignal) => request<TraceJob>(`/api/v1/trace-jobs/${encodeURIComponent(id)}`, { signal }),
  stopTraceJob: (id: string) => request<TraceJob>(`/api/v1/trace-jobs/${encodeURIComponent(id)}/stop`, { method: "POST" }),
  latestTraceJob: (query: TraceQuery, signal?: AbortSignal) => request<TraceJob>(`/api/v1/trace-jobs/latest?${queryString(query)}`, { signal }),
  createSync: (chain: string, address: string) => request<SyncJob>("/api/v1/sync", { method: "POST", body: JSON.stringify({ chain, address, neighborLimit: 0 }) }),
  syncJob: (id: string, signal?: AbortSignal) => request<SyncJob>(`/api/v1/sync-jobs/${encodeURIComponent(id)}`, { signal }),
  syncJobs: requestSyncJobs,
  latestSyncJob: (chain: string, address: string, signal?: AbortSignal) => request<SyncJob>(`/api/v1/sync-jobs/latest?${queryString({ chain, address })}`, { signal }),
  address: (chain: string, address: string, signal?: AbortSignal) => request<AddressResponse>(`/api/v1/addresses/${address}?${queryString({ chain })}`, { signal }),
  profile: (chain: string, address: string, signal?: AbortSignal) => request<AddressProfile>(`/api/v1/addresses/${address}/profile?${queryString({ chain })}`, { signal }),
  labels: (chain: string, address: string, signal?: AbortSignal) => request<Label[]>(`/api/v1/labels?${queryString({ chain, address })}`, { signal }),
  edges: (chain: string, address: string, direction: string, asset: string, cursor?: string, signal?: AbortSignal) => request<EdgePage>(`/api/v1/edges?${queryString({ chain, address, direction, asset, limit: 100, cursor })}`, { signal }),
  bridges: (chain: string, address: string, signal?: AbortSignal) => request<BridgePage>(`/api/v1/bridge-links?${queryString({ chain, address })}`, { signal }),
  createLabel: (input: LabelInput) => request<Label>("/api/v1/labels", { method: "POST", body: JSON.stringify(input) }),
  createBridge: (input: BridgeInput) => request<CrossChainLink>("/api/v1/bridge-links", { method: "POST", body: JSON.stringify(input) }),
  analyzeBridge: (input: BridgeAnalysisInput) => request<BridgePage>("/api/v1/bridge-analysis", { method: "POST", body: JSON.stringify(input) }),
  syncBridge: (linkId: string) => request<BridgeSyncAccepted>("/api/v1/bridge-sync", { method: "POST", body: JSON.stringify({ linkId }) }),
  transaction: (chain: string, txHash: string, signal?: AbortSignal) => request<TransactionAnalysis>(`/api/v1/transactions/${encodeURIComponent(txHash)}?${queryString({ chain })}`, { signal }),
};
