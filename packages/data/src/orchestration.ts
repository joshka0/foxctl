import type {
  OrchestrationBoard,
  OrchestrationBoardArtifactRef,
  OrchestrationLane,
} from "./types";

export type OrchestrationBoardPayload =
  | { kind: "board"; board: OrchestrationBoard }
  | { kind: "artifact"; artifact: OrchestrationBoardArtifactRef };

export function parseOrchestrationBoardPayload(data: unknown): OrchestrationBoardPayload {
  if (isOrchestrationBoard(data)) {
    return { kind: "board", board: data };
  }
  if (isOrchestrationBoardArtifactRef(data)) {
    return { kind: "artifact", artifact: data };
  }
  throw new Error("orchestration board response missing board or artifact payload");
}

function isOrchestrationBoard(data: unknown): data is OrchestrationBoard {
  const record = asRecord(data);
  if (!record) return false;
  if (typeof record.generated_at !== "string") return false;
  if (!asRecord(record.counts)) return false;
  if (!Array.isArray(record.lanes)) return false;
  return record.lanes.every(isOrchestrationLane);
}

function isOrchestrationBoardArtifactRef(data: unknown): data is OrchestrationBoardArtifactRef {
  const record = asRecord(data);
  if (!record) return false;
  return typeof record.artifact === "string" && typeof record.summary === "string";
}

function isOrchestrationLane(data: unknown): data is OrchestrationLane {
  const record = asRecord(data);
  if (!record) return false;
  return typeof record.id === "string" && typeof record.title === "string" && Array.isArray(record.cards);
}

function asRecord(data: unknown): Record<string, unknown> | null {
  return data && typeof data === "object" && !Array.isArray(data)
    ? (data as Record<string, unknown>)
    : null;
}
