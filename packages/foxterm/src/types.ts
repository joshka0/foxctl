export type FocusRegion = "nav" | "worklist" | "detail" | "composer";
export type Mode =
  | "normal"
  | "help"
  | "filter"
  | "compose"
  | "createRoom"
  | "roomMessage"
  | "spawnAgent"
  | "spawnCLI"
  | "atcpPrompt"
  | "atcpScreen"
  | "confirmKill"
  | "confirmATCPStop";
export type LoadState = "idle" | "loading" | "ready" | "error";
export type ActivityScope = "focused" | "important" | "all" | "debug";

export interface StatusMessage {
  tone: "success" | "warning" | "danger" | "muted" | "focus";
  text: string;
}
