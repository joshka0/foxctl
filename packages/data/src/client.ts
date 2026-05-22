// API client contracts shared by active foxctl UIs.

import type {
  ApiEnvelope,
  OrchestrationBoardCardRuntimeResult,
  OrchestrationCardAction,
  OrchestrationCardActionResult,
  OrchestrationLaneID,
  OrchestrationRefreshResult,
} from "./types";
import {
  parseOrchestrationBoardPayload,
  type OrchestrationBoardPayload,
} from "./orchestration";

interface ViteImportMeta {
  env?: {
    VITE_API_URL?: string;
  };
}

interface ProcessGlobal {
  env?: {
    FOXCTL_API_URL?: string;
  };
}

function getApiBase(): string {
  const meta = import.meta as ViteImportMeta;
  if (typeof meta !== "undefined" && meta.env?.VITE_API_URL) {
    return meta.env.VITE_API_URL;
  }

  const runtime = globalThis as typeof globalThis & {
    process?: ProcessGlobal;
  };
  return runtime.process?.env?.FOXCTL_API_URL ?? "";
}

const API_BASE = getApiBase();

class APIError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "APIError";
    this.status = status;
  }
}

async function request<T>(
  endpoint: string,
  options: RequestInit = {},
): Promise<T> {
  const response = await fetch(`${API_BASE}${endpoint}`, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...options.headers,
    },
    credentials: "include",
  });

  const text = await response.text();
  if (!response.ok) {
    throw new APIError(response.status, text || `HTTP ${response.status}`);
  }
  if (!text) {
    throw new APIError(response.status, "Empty response body");
  }

  try {
    return JSON.parse(text) as T;
  } catch {
    throw new APIError(response.status, "Invalid JSON response");
  }
}

function unwrapEnvelope<T>(env: ApiEnvelope<T>): T {
  if (env.status !== "ok") {
    throw new APIError(500, env.error?.message || "Request failed");
  }
  return env.data;
}

export interface OrchestrationBoardGetParams {
  request_id?: string;
  workspace_id?: string;
  limit?: number;
  cursor?: string;
  lane?: OrchestrationLaneID;
  archived_only?: boolean;
}

export async function getOrchestrationBoard(
  params?: OrchestrationBoardGetParams,
): Promise<OrchestrationBoardPayload> {
  const query = new URLSearchParams();
  if (params?.request_id) query.set("request_id", params.request_id);
  if (params?.workspace_id) query.set("workspace_id", params.workspace_id);
  if (typeof params?.limit === "number" && Number.isFinite(params.limit)) {
    query.set("limit", String(params.limit));
  }
  if (params?.cursor) query.set("cursor", params.cursor);
  if (params?.lane) query.set("lane", params.lane);
  if (params?.archived_only) query.set("archived_only", "true");

  const suffix = query.size > 0 ? `?${query.toString()}` : "";
  const env = await request<ApiEnvelope<unknown>>(
    `/api/orchestration/board-get${suffix}`,
  );
  return parseOrchestrationBoardPayload(unwrapEnvelope(env));
}

export async function getOrchestrationBoardCardRuntime(params: {
  request_id?: string;
  workspace_id?: string;
  issue_id: string;
  depth?: number;
}): Promise<OrchestrationBoardCardRuntimeResult> {
  const query = new URLSearchParams();
  if (params.request_id) query.set("request_id", params.request_id);
  if (params.workspace_id) query.set("workspace_id", params.workspace_id);
  query.set("issue_id", params.issue_id);
  if (typeof params.depth === "number" && Number.isFinite(params.depth)) {
    query.set("depth", String(params.depth));
  }

  const env = await request<ApiEnvelope<OrchestrationBoardCardRuntimeResult>>(
    `/api/orchestration/board-card-runtime-get?${query.toString()}`,
  );
  return unwrapEnvelope(env);
}

export async function applyOrchestrationCardAction(params: {
  request_id: string;
  workspace_id?: string;
  issue_id: string;
  action: OrchestrationCardAction;
}): Promise<OrchestrationCardActionResult> {
  const env = await request<ApiEnvelope<OrchestrationCardActionResult>>(
    "/api/orchestration/card-action",
    {
      method: "POST",
      body: JSON.stringify(params),
    },
  );
  return unwrapEnvelope(env);
}

export async function refreshOrchestration(params: {
  request_id: string;
  workspace_id?: string;
}): Promise<OrchestrationRefreshResult> {
  const env = await request<ApiEnvelope<OrchestrationRefreshResult>>(
    "/api/orchestration/refresh",
    {
      method: "POST",
      body: JSON.stringify(params),
    },
  );
  return unwrapEnvelope(env);
}
