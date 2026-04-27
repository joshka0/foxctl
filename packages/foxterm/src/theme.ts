export const theme = {
  bg: "transparent",
  panel: "#1f2335",
  panelAlt: "#24283b",
  border: "#414868",
  borderMuted: "#303443",
  text: "#c0caf5",
  muted: "#7f849c",
  subtle: "#565f89",
  focus: "#7aa2f7",
  success: "#9ece6a",
  warning: "#e0af68",
  danger: "#f7768e",
  accent: "#bb9af7",
};

export type Tone = "success" | "warning" | "danger" | "muted" | "focus";

export function toneColor(tone: Tone): string {
  switch (tone) {
    case "success":
      return theme.success;
    case "warning":
      return theme.warning;
    case "danger":
      return theme.danger;
    case "focus":
      return theme.focus;
    case "muted":
    default:
      return theme.muted;
  }
}
