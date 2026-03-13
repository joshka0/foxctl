import type {
  ConsoleMessage,
  PersistedSession,
  SessionMessage,
} from "@/api/client";

export function getSessionMessageContent(msg: SessionMessage): string {
  if (msg.summary) return msg.summary;
  if (msg.error) return `Error: ${msg.error}`;
  if (msg.message?.content) {
    if (typeof msg.message.content === "string") return msg.message.content;
    if (Array.isArray(msg.message.content)) {
      return msg.message.content
        .map((block: unknown) => {
          if (typeof block === "string") return block;
          if (typeof block === "object" && block !== null) {
            const candidate = block as Record<string, unknown>;
            if (candidate.type === "text" && typeof candidate.text === "string") {
              return candidate.text;
            }
            if (candidate.type === "tool_use") {
              return `[Tool: ${String(candidate.name || "unknown")}]`;
            }
            if (candidate.type === "tool_result") return "[Tool Result]";
          }
          return "";
        })
        .filter(Boolean)
        .join("\n");
    }
  }
  if (msg.tool_calls && msg.tool_calls.length > 0) {
    const toolNames = msg.tool_calls
      .map((toolCall) => {
        if (typeof toolCall === "string") return toolCall;
        if (typeof toolCall === "object" && toolCall !== null) {
          const candidate = toolCall as Record<string, unknown>;
          if (typeof candidate.name === "string") return candidate.name;
          if (
            typeof candidate.function === "object" &&
            candidate.function !== null &&
            typeof (candidate.function as Record<string, unknown>).name ===
              "string"
          ) {
            return String(
              (candidate.function as Record<string, unknown>).name,
            );
          }
        }
        return "unknown";
      })
      .join(", ");
    return `[Used ${msg.tool_calls.length} tool${msg.tool_calls.length > 1 ? "s" : ""}: ${toolNames}]`;
  }
  return "[No content]";
}

export function buildHistoricalFollowUpPrompt(
  persisted: PersistedSession,
  sessionMessages: ConsoleMessage[],
): string {
  const workspace = persisted.workspace_path || "/";
  const project =
    persisted.project_name ||
    workspace.split("/").pop() ||
    "Historical Session";
  const summary = (persisted.summary || "").trim();
  const recentTranscript = sessionMessages
    .slice(-8)
    .map((message) => `${message.role}: ${message.content}`)
    .join("\n\n");

  return [
    "You are continuing work from a historical agentctl session.",
    `Project: ${project}`,
    `Workspace: ${workspace}`,
    summary ? `Session summary:\n${summary}` : "",
    recentTranscript ? `Recent transcript excerpt:\n${recentTranscript}` : "",
    "Use this as background context for the follow-up conversation, but treat it as historical context rather than authoritative live runtime state.",
  ]
    .filter(Boolean)
    .join("\n\n");
}
