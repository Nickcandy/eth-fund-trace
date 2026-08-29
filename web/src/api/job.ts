import type { JobStatus, SyncJob, TraceJob } from "./types";

export interface JobProgress {
  status: JobStatus;
  currentDepth: number;
  visitedNodes: number;
  edgeCount: number;
}
export interface JobDescription {
  tone: "pending" | "running" | "success" | "warning" | "error";
  label: string;
  detail: string;
}

const activeStatuses: JobStatus[] = ["queued", "waiting_sync", "running"];

export function activeTraceJob(
  root?: TraceJob,
  extension?: TraceJob,
): TraceJob | undefined {
  return extension && activeStatuses.includes(extension.status)
    ? extension
    : root;
}

export function describeTraceJob(job: JobProgress): JobDescription {
  const detail =
    job.status === "running"
      ? `第 ${job.currentDepth} 层 · ${job.visitedNodes} 节点 · ${job.edgeCount} 条边`
      : `已发现 ${job.visitedNodes} 个节点`;
  switch (job.status) {
    case "queued":
      return { tone: "pending", label: "任务排队中", detail };
    case "waiting_sync":
      return { tone: "pending", label: "等待数据同步", detail };
    case "running":
      return { tone: "running", label: "正在追踪", detail };
    case "succeeded":
      return { tone: "success", label: "分析完成", detail };
    case "partial":
      return { tone: "warning", label: "部分数据可用", detail };
    case "failed":
      return { tone: "error", label: "分析失败", detail };
    case "stopped":
      return { tone: "warning", label: "已停止", detail: "任务已由用户停止" };
  }
}

export function mergeSyncJobs(
  linked: SyncJob[],
  recovered?: SyncJob,
): SyncJob[] {
  if (!recovered) return linked;
  const index = linked.findIndex((job) => job.jobId === recovered.jobId);
  if (index < 0) return [...linked, recovered];
  return linked.map((job, jobIndex) => (jobIndex === index ? recovered : job));
}

export function collapseSyncJobsByAddress(jobs: SyncJob[]): SyncJob[] {
  const collapsed: SyncJob[] = [];
  const indexes = new Map<string, number>();

  for (const job of jobs) {
    const address = (job.progress?.currentAddress || job.address).toLowerCase();
    const existingIndex = indexes.get(address);
    if (existingIndex === undefined) {
      indexes.set(address, collapsed.length);
      collapsed.push(job);
    } else {
      collapsed[existingIndex] = job;
    }
  }

  return collapsed;
}
