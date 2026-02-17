import type { LucideIcon } from "lucide-react";
import {
  Bot,
  Brain,
  Search,
  Zap,
  Hash,
  FileText,
  Eye,
  Cpu,
  Activity,
  Bug,
  Users,
} from "lucide-react";
import type { Agent } from "@/api/types";

const ZERO_TIME_PREFIX = "0001-01-01T";

const roleIcons: Record<string, LucideIcon> = {
  researcher: Brain,
  coder: Cpu,
  reviewer: Eye,
  planner: Activity,
  semantic_scout: Search,
  dag_scout: Zap,
  symbol_scout: Hash,
  annotation_scout: FileText,
  overseer: Users,
  fixer: Bug,
};

const WORKER_ROLES = new Set([
  "semantic_scout",
  "dag_scout",
  "symbol_scout",
  "annotation_scout",
]);

const truncate = (value: string, maxLen: number): string => {
  const rs = [...value];
  if (maxLen <= 0 || rs.length <= maxLen) return value;
  return `${rs.slice(0, maxLen).join("")}...`;
};

export function getRoleIcon(role?: string): LucideIcon {
  return roleIcons[role || ""] || Bot;
}

export function getAgentDisplayName(agent: Agent): string {
  return agent.role || agent.name || agent.slug || "Agent";
}

export function getAgentSubtitle(agent: Agent): string {
  return (
    agent.name || agent.slug || (agent.id ? agent.id.slice(0, 8) : "agent")
  );
}

export function getPromptSummaryOrSubtitle(agent: Agent, maxLen = 120): string {
  const summary = (agent.prompt_summary || "").trim();
  if (summary) return truncate(summary, maxLen);
  return truncate(getAgentSubtitle(agent), maxLen);
}

export function getAgentActivityTimestamp(agent: Agent): number {
  const candidates = [agent.heartbeat_at, agent.created_at];
  for (const ts of candidates) {
    if (!ts) continue;
    if (ts.startsWith(ZERO_TIME_PREFIX)) continue;
    const parsed = Date.parse(ts);
    if (Number.isFinite(parsed) && parsed > 0) return parsed;
  }
  return 0;
}

export function isWorkerAgent(agent: Agent): boolean {
  const role = agent.role || "";
  if (WORKER_ROLES.has(role)) return true;
  return role.endsWith("_scout") && agent.exec_mode === "autonomous";
}
