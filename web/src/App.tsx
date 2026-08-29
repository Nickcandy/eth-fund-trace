import "@xyflow/react/dist/style.css";
import {
  QueryClient,
  QueryClientProvider,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  Activity,
  Database,
  GitFork,
  KeyRound,
  PanelRightClose,
  PanelRightOpen,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { api, ApiError, setBearerToken } from "./api/client";
import {
  activeTraceJob,
  collapseSyncJobsByAddress,
  describeTraceJob,
  mergeSyncJobs,
  syncJobIDsToRefresh,
} from "./api/job";
import type { GraphEdgeModel } from "./graph/model";
import { buildGraphModel } from "./graph/model";
import {
  readTraceJobID,
  readTraceQuery,
  validateTraceQuery,
  writeTraceQuery,
} from "./lib/query";
import { BottomPanel } from "./components/BottomPanel";
import { DetailsPanel } from "./components/DetailsPanel";
import { GraphCanvas } from "./components/GraphCanvas";
import { QueryBar } from "./components/QueryBar";
import { TransactionView } from "./components/TransactionView";
import type { LabelInput, TraceQuery } from "./api/types";

const client = new QueryClient({
  defaultOptions: {
    queries: {
      retry: (count, error) =>
        error instanceof ApiError && error.retryable && count < 2,
      staleTime: 30_000,
    },
  },
});

export default function App() {
  return (
    <QueryClientProvider client={client}>
      <Console />
    </QueryClientProvider>
  );
}
function Console() {
  const extensionApplied = useRef<string | undefined>(undefined);
  const syncJobCache = useRef(
    new Map<string, import("./api/types").SyncJob>(),
  );
  const queryClient = useQueryClient();
  const requestGeneration = useRef(0);
  const initialQuery = useRef(readTraceQuery(location.search));
  const initialJobID = useRef(readTraceJobID(location.search));
  const [draft, setDraft] = useState<TraceQuery>(initialQuery.current);
  const [active, setActive] = useState<TraceQuery | undefined>(() =>
    initialJobID.current && !validateTraceQuery(initialQuery.current)
      ? initialQuery.current
      : undefined,
  );
  const [jobID, setJobID] = useState<string | undefined>(initialJobID.current);
  const [error, setError] = useState<string>();
  const [mode, setMode] = useState<"address" | "transaction">("address");
  const [txHash, setTxHash] = useState("");
  const [activeTxHash, setActiveTxHash] = useState<string>();
  const [selectedNode, setSelectedNode] = useState<string>();
  const [selectedEdge, setSelectedEdge] = useState<GraphEdgeModel>();
  const [detailsOpen, setDetailsOpen] = useState(true);
  const [layoutKey, setLayoutKey] = useState(0);
  const create = useMutation({
    mutationFn: ({ query }: { query: TraceQuery; generation: number }) =>
      api.createTrace(query),
    onSuccess: (data, input) => {
      if (input.generation !== requestGeneration.current) return;
      setActive(input.query);
      setJobID(data.traceJobId);
      history.replaceState(
        null,
        "",
        writeTraceQuery(input.query, data.traceJobId),
      );
      setError(undefined);
      setSelectedEdge(undefined);
      setSelectedNode(undefined);
    },
    onError: (e, input) => {
      if (input.generation === requestGeneration.current)
        setError(e instanceof Error ? e.message : "无法创建追踪任务");
    },
  });
  const [recoveredSyncID, setRecoveredSyncID] = useState<string>();
  const recoverSync = useMutation({
    mutationFn: (query: TraceQuery) =>
      api.createSync(query.chain, query.address),
    onSuccess: (data) => setRecoveredSyncID(data.jobId),
  });
  const recoveredSync = useQuery({
    queryKey: ["recovered-sync-job", recoveredSyncID],
    queryFn: ({ signal }) => api.syncJob(recoveredSyncID!, signal),
    enabled: !!recoveredSyncID,
    refetchInterval: (q) =>
      ["queued", "running"].includes(q.state.data?.status ?? "") ? 1000 : false,
  });
  const job = useQuery({
    queryKey: ["trace-job", jobID],
    queryFn: ({ signal }) => api.traceJob(jobID!, signal),
    enabled: !!jobID,
    refetchInterval: (q) =>
      ["succeeded", "partial", "failed"].includes(q.state.data?.status ?? "")
        ? false
        : 1000,
  });
  const extension = useQuery({
    queryKey: ["trace-extension", jobID],
    queryFn: ({ signal }) => api.latestTraceExtension(jobID!, signal),
    enabled:
      !!jobID && ["succeeded", "partial"].includes(job.data?.status ?? ""),
    retry: false,
    refetchInterval: (q) =>
      ["queued", "waiting_sync", "running"].includes(q.state.data?.status ?? "")
        ? 1000
        : false,
  });
  const extensionCreate = useMutation({
    mutationFn: (input: {
      address: string;
      direction: "in" | "out";
      depth: 1;
    }) => api.createTraceExtension(jobID!, input),
    onSuccess: async () => {
      setError(undefined);
      await extension.refetch();
    },
    onError: (e) => setError(e instanceof Error ? e.message : "无法继续追踪"),
  });
  const currentTraceJob = activeTraceJob(job.data, extension.data);
  useEffect(() => {
    const current = extension.data;
    if (
      !jobID ||
      !current ||
      !["succeeded", "partial"].includes(current.status) ||
      extensionApplied.current === current.id
    )
      return;
    extensionApplied.current = current.id;
    void job.refetch();
  }, [extension.data, jobID]);
  const recoverableTrace =
    mode === "address" &&
    !jobID &&
    requestGeneration.current === 0 &&
    !validateTraceQuery(draft)
      ? draft
      : undefined;
  const latestTrace = useQuery({
    queryKey: ["latest-trace-job", recoverableTrace],
    queryFn: ({ signal }) => api.latestTraceJob(recoverableTrace!, signal),
    enabled: !!recoverableTrace,
    retry: false,
  });
  useEffect(() => {
    if (!latestTrace.data || jobID || requestGeneration.current !== 0) return;
    setActive(draft);
    setJobID(latestTrace.data.id);
    history.replaceState(null, "", writeTraceQuery(draft, latestTrace.data.id));
  }, [latestTrace.data, jobID, draft]);
  const result =
    job.data?.result?.ruleVersion === "trace-v1" ? job.data.result : undefined;
  const model = useMemo(
    () =>
      result && active
        ? buildGraphModel(result, {
            chain: active.chain,
            address: active.address,
          })
        : undefined,
    [result, active],
  );
  const seedReady = !!active && !!result;
  const selectedNodeValue = model?.nodes.find((n) => n.id === selectedNode);
  const detailNode = selectedEdge
    ? undefined
    : (selectedNodeValue ?? model?.nodes.find((n) => n.seed));
  const detailChain = detailNode?.chain;
  const detailAddress = detailNode?.address;
  const address = useQuery({
    queryKey: ["address", detailChain, detailAddress],
    queryFn: ({ signal }) => api.address(detailChain!, detailAddress!, signal),
    enabled: seedReady && !!detailNode,
  });
  const balance = useQuery({
    queryKey: ["balance", detailChain, detailAddress],
    queryFn: ({ signal }) => api.balance(detailChain!, detailAddress!, signal),
    enabled: seedReady && !!detailNode,
    staleTime: 15_000,
  });
  const profile = useQuery({
    queryKey: ["profile", detailChain, detailAddress],
    queryFn: ({ signal }) => api.profile(detailChain!, detailAddress!, signal),
    enabled: seedReady && !!detailNode,
  });
  const labels = useQuery({
    queryKey: ["labels", detailChain, detailAddress],
    queryFn: ({ signal }) => api.labels(detailChain!, detailAddress!, signal),
    enabled: seedReady && !!detailNode,
  });
  const facts = useInfiniteQuery({
    queryKey: ["edges", active],
    queryFn: ({ signal, pageParam }) =>
      api.edges(
        active!.chain,
        active!.address,
        active!.direction,
        active!.asset,
        pageParam,
        signal,
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor,
    enabled: seedReady,
  });
  const syncIDs = [
    ...(job.data?.syncJobIds ?? []),
    ...(extension.data?.syncJobIds ?? []),
  ];
  const syncQueries = useQuery({
    queryKey: ["sync-jobs", jobID, syncIDs],
    queryFn: async ({ signal }) => {
      const refreshIDs = syncJobIDsToRefresh(
        syncIDs,
        syncJobCache.current.values(),
      );
      const refreshed = await api.syncJobs(refreshIDs, signal);
      for (const syncJob of refreshed)
        syncJobCache.current.set(syncJob.jobId, syncJob);
      return syncIDs.flatMap((id) => {
        const syncJob = syncJobCache.current.get(id);
        return syncJob ? [syncJob] : [];
      });
    },
    enabled: syncIDs.length > 0,
    placeholderData: (previousData) => previousData,
    refetchInterval:
      ["queued", "waiting_sync", "running"].includes(
        extension.data?.status ?? "",
      ) || job.data?.status === "waiting_sync"
        ? 1000
        : false,
  });
  const lookupQuery =
    mode === "address" && !validateTraceQuery(draft) ? draft : undefined;
  const latestSync = useQuery({
    queryKey: [
      "latest-sync-job",
      lookupQuery?.chain,
      lookupQuery?.address.toLowerCase(),
    ],
    queryFn: ({ signal }) =>
      api.latestSyncJob(lookupQuery!.chain, lookupQuery!.address, signal),
    enabled: !!lookupQuery && syncIDs.length === 0,
    retry: false,
    refetchInterval: (q) =>
      ["queued", "running"].includes(q.state.data?.status ?? "") ? 1000 : false,
  });
  const visibleSyncJobs = collapseSyncJobsByAddress(
    mergeSyncJobs(
      mergeSyncJobs(syncQueries.data ?? [], recoveredSync.data),
      latestSync.data,
    ),
  );
  const transaction = useQuery({
    queryKey: ["transaction", "ethereum", activeTxHash],
    queryFn: ({ signal }) => api.transaction("ethereum", activeTxHash!, signal),
    enabled: mode === "transaction" && !!activeTxHash,
    retry: false,
  });
  const startTrace = (query: TraceQuery) => {
    requestGeneration.current += 1;
    queryClient.cancelQueries();
    recoverSync.mutate(query);
    create.mutate({ query, generation: requestGeneration.current });
  };
  const submit = () => {
    if (mode === "transaction") {
      if (!/^0x[0-9a-fA-F]{64}$/.test(txHash)) {
        setError("请输入有效的 66 位交易哈希");
        return;
      }
      setError(undefined);
      const normalized = txHash.toLowerCase();
      if (activeTxHash === normalized) {
        void transaction.refetch();
      } else setActiveTxHash(normalized);
      return;
    }
    const issue = validateTraceQuery(draft);
    if (issue) {
      setError(issue);
      return;
    }
    startTrace(draft);
  };
  const focus = (chain: string, addressValue: string) => {
    const q = {
      ...draft,
      chain: chain as TraceQuery["chain"],
      address: addressValue,
    };
    setDraft(q);
    const issue = validateTraceQuery(q);
    if (!issue) startTrace(q);
  };
  const traceOutput = (addressValue: string) => {
    const q = { ...draft, chain: "ethereum" as const, address: addressValue };
    setMode("address");
    setDraft(q);
    setError(undefined);
    startTrace(q);
  };
  const status = currentTraceJob
    ? describeTraceJob(currentTraceJob)
    : undefined;
  const completedSyncCount = visibleSyncJobs.filter((sync) =>
    ["succeeded", "partial"].includes(sync.status),
  ).length;
  const activeSyncStep =
    visibleSyncJobs.findIndex((sync) =>
      ["queued", "running"].includes(sync.status),
    ) + 1;
  const statusDetail =
    currentTraceJob?.status === "waiting_sync"
      ? `${completedSyncCount} 个地址已同步${activeSyncStep > 0 ? ` · 正在处理步骤 ${activeSyncStep}` : ""}`
      : status?.detail;
  const writeLabel = async (input: LabelInput) => {
    await api.createLabel(input);
    await queryClient.invalidateQueries({ queryKey: ["labels"] });
  };
  const stopTrace = async () => {
    const targetID = currentTraceJob?.id;
    if (!targetID) return;
    await api.stopTraceJob(targetID);
    await queryClient.invalidateQueries({ queryKey: ["trace-job", jobID] });
    await queryClient.invalidateQueries({
      queryKey: ["trace-extension", jobID],
    });
    await queryClient.invalidateQueries({ queryKey: ["sync-jobs", jobID] });
  };
  const continueTrace = (addressValue: string, side: "left" | "right") => {
    if (!jobID) return;
    extensionCreate.mutate({
      address: addressValue,
      direction: side === "left" ? "in" : "out",
      depth: 1,
    });
  };
  const nodeRisk =
    detailNode && result && active && detailNode.chain === active.chain
      ? riskForAddress(result.risk, detailNode.address, detailNode.seed)
      : undefined;
  const transactionError =
    transaction.error instanceof Error ? transaction.error.message : undefined;
  return (
    <main
      className={`app-shell ${mode === "transaction" ? "transaction-mode" : ""}`}
    >
      <header className="app-header">
        <div className="brand">
          <GitFork size={24} />
          <div>
            <h1>资金链路分析台</h1>
            <span>ETH FUND TRACE · M11</span>
          </div>
        </div>
        <div className="system-state">
          <span>
            <i className="online" />
            API
          </span>
          <span>
            <Database size={14} /> Mongo
          </span>
          <button
            title="设置本次会话的 API Bearer Token"
            onClick={() => {
              const token = window.prompt(
                "输入 Bearer Token（仅保存在当前页面内存中）",
              );
              if (token !== null) setBearerToken(token);
            }}
          >
            <KeyRound size={14} /> API 凭证
          </button>
        </div>
      </header>
      <QueryBar
        value={draft}
        onChange={setDraft}
        onSubmit={submit}
        busy={
          mode === "transaction"
            ? transaction.isFetching
            : create.isPending || recoverSync.isPending
        }
        error={error ?? (mode === "transaction" ? transactionError : undefined)}
        mode={mode}
        onModeChange={(next) => {
          setMode(next);
          setError(undefined);
        }}
        txHash={txHash}
        onTxHashChange={setTxHash}
      />
      {mode === "transaction" ? (
        <>
          <section className="status-bar">
            <span
              className={`status-pill ${transaction.data?.quality.status === "complete" ? "success" : transaction.error ? "error" : transaction.data ? "warning" : "idle"}`}
            >
              <Activity size={14} />
              {transaction.isFetching
                ? "读取链上证据"
                : transaction.data
                  ? "交易分析完成"
                  : transaction.error
                    ? "交易分析失败"
                    : "等待交易哈希"}
            </span>
            <span>
              {transaction.data
                ? `${transaction.data.swaps.length} 段 Swap · ${transaction.data.transfers.length} 条 Transfer 事实`
                : "输入交易哈希以分析 Receipt"}
            </span>
          </section>
          <div className="transaction-workspace">
            {transaction.data ? (
              <TransactionView
                analysis={transaction.data}
                onTrace={traceOutput}
              />
            ) : (
              <EmptyState error={transactionError} />
            )}
          </div>
        </>
      ) : (
        <>
          <section className="status-bar">
            <span className={`status-pill ${status?.tone ?? "idle"}`}>
              <Activity size={14} />
              {status?.label ?? "等待分析"}
            </span>
            <span>{statusDetail ?? "输入地址以创建异步追踪任务"}</span>
            {result && (
              <>
                <span>数据区块 {result.dataThroughBlock.toLocaleString()}</span>
                <span>{result.ruleVersion}</span>
                <span>
                  {result.nodes.length} 节点 · {result.edges.length} 边
                </span>
              </>
            )}
            <button
              title={detailsOpen ? "收起详情" : "展开详情"}
              onClick={() => setDetailsOpen(!detailsOpen)}
            >
              {detailsOpen ? (
                <PanelRightClose size={16} />
              ) : (
                <PanelRightOpen size={16} />
              )}
            </button>
          </section>
          <div className={`workspace ${detailsOpen ? "with-details" : ""}`}>
            <section className="graph-stage">
              {model ? (
                <GraphCanvas
                  key={layoutKey}
                  model={model}
                  extension={
                    extension.data
                      ? {
                          address: extension.data.extensionAddress,
                          direction: extension.data.extensionDirection,
                          status: extension.data.status,
                        }
                      : undefined
                  }
                  onExpand={(action) => {
                    if (action.mode === "trace")
                      continueTrace(action.address, action.side);
                  }}
                  onSelectNode={(id) => {
                    setSelectedNode(id);
                    setSelectedEdge(undefined);
                    setDetailsOpen(true);
                  }}
                  onSelectEdge={(edge) => {
                    setSelectedEdge(edge);
                    setSelectedNode(undefined);
                    setDetailsOpen(true);
                  }}
                  onFocusAddress={focus}
                  onRelayout={() => setLayoutKey((v) => v + 1)}
                />
              ) : (
                <EmptyState
                  status={status?.label}
                  error={
                    job.data?.result &&
                    job.data.result.ruleVersion !== "trace-v1"
                      ? "旧版追踪结果，请重新运行"
                      : job.data?.error
                  }
                  onRetry={job.data?.retryable ? submit : undefined}
                />
              )}
            </section>
            {detailsOpen && (
              <DetailsPanel
                node={detailNode}
                edge={selectedEdge}
                address={address.data}
                balance={balance.data}
                balanceLoading={balance.isLoading}
                balanceError={
                  balance.error instanceof Error
                    ? balance.error.message
                    : undefined
                }
                profile={profile.data}
                labels={labels.data ?? []}
                labelsLoading={labels.isLoading}
                labelsError={
                  labels.error instanceof Error
                    ? labels.error.message
                    : undefined
                }
                risk={nodeRisk}
                onLabel={writeLabel}
                onClose={() => setDetailsOpen(false)}
                onFocus={focus}
              />
            )}
          </div>
          <BottomPanel
            facts={facts.data?.pages.flatMap((page) => page.items) ?? []}
            traceResult={result}
            hasMore={facts.hasNextPage}
            loadingMore={facts.isFetchingNextPage}
            onMore={() => facts.fetchNextPage()}
            traceJob={currentTraceJob}
            syncJobs={visibleSyncJobs}
            onStopTrace={stopTrace}
          />
        </>
      )}
    </main>
  );
}
function EmptyState({
  status,
  error,
  onRetry,
}: {
  status?: string;
  error?: string;
  onRetry?: () => void;
}) {
  return (
    <div className="empty-state">
      <div className="empty-axis">
        <span>上游</span>
        <i />
        <strong>查询中心</strong>
        <i />
        <span>下游</span>
      </div>
      <h2>{error ? "分析未完成" : (status ?? "尚未加载资金图")}</h2>
      <p>{error ?? "输入链上地址，系统会同步数据并按层展开事实资金边。"}</p>
      {onRetry && (
        <button className="retry-command" onClick={onRetry}>
          重试分析
        </button>
      )}
    </div>
  );
}
function riskForAddress(
  risk: import("./api/types").RiskResult,
  address: string,
  seed: boolean,
) {
  if (seed) return risk;
  const evidence =
    risk.evidence?.filter(
      (item) => item.address.toLowerCase() === address.toLowerCase(),
    ) ?? [];
  const score = Math.max(0, ...evidence.map((item) => item.score));
  return {
    ...risk,
    score,
    level:
      score >= 70 ? "known_high" : score >= 40 ? "suspected" : "no_conclusion",
    evidence,
  };
}
