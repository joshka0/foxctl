export type FocusRegion = "nav" | "worklist" | "detail";
export type Mode = "normal" | "help" | "filter";
export type LoadState = "idle" | "loading" | "ready" | "error";
export type ActivityScope = "focused" | "important" | "all" | "debug";

export interface StatusMessage {
  tone: "success" | "warning" | "danger" | "muted" | "focus";
  text: string;
}
