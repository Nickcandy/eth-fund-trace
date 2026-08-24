import type { JobStatus, SyncJob } from "./types";

export interface JobProgress { status: JobStatus; currentDepth: number; visitedNodes: number; edgeCount: number }
export interface JobDescription { tone: "pending" | "running" | "success" | "warning" | "error"; label: string; detail: string }

export function describeTraceJob(job: JobProgress): JobDescription {
  const detail = job.status === "running"
    ? `第 ${job.currentDepth} 层 · ${job.visitedNodes} 节点 · ${job.edgeCount} 条边`
    : `已发现 ${job.visitedNodes} 个节点`;
  switch (job.status) {
    case "queued": return { tone: "pending", label: "任务排队中", detail };
    case "waiting_sync": return { tone: "pending", label: "等待数据同步", detail };
    case "running": return { tone: "running", label: "正在追踪", detail };
    case "succeeded": return { tone: "success", label: "分析完成", detail };
    case "partial": return { tone: "warning", label: "部分数据可用", detail };
    case "failed": return { tone: "error", label: "分析失败", detail };
  }
}

export function mergeSyncJobs(linked: SyncJob[], recovered?: SyncJob): SyncJob[] {
  if (!recovered) return linked;
  return [...linked.filter((job) => job.jobId !== recovered.jobId), recovered];
}
