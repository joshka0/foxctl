import { theme } from "../theme";

export function HelpOverlay({
  compact,
  width,
}: {
  compact: boolean;
  width: number;
}) {
  const overlayWidth = compact ? Math.max(44, Math.min(width - 4, 62)) : 62;
  const left = compact ? 2 : 8;
  return (
    <box
      style={{
        position: "absolute",
        top: 4,
        left,
        width: overlayWidth,
        height: 18,
        border: true,
        borderStyle: "rounded",
        borderColor: theme.focus,
        backgroundColor: theme.panel,
        flexDirection: "column",
        padding: 1,
        gap: 1,
      }}
    >
      <text fg={theme.focus}>Help</text>
      <text fg={theme.text}>Tab / Shift+Tab   Move focus between regions</text>
      <text fg={theme.text}>Up/Down or j/k    Move selection</text>
      <text fg={theme.text}>Enter             Open selected run detail</text>
      <text fg={theme.text}>n                 Compose a new v2 run</text>
      <text fg={theme.text}>c                 Continue selected run</text>
      <text fg={theme.text}>x                 Kill selected active run</text>
      <text fg={theme.text}>/                 Filter active worklist</text>
      <text fg={theme.text}>a                 Cycle activity scope</text>
      <text fg={theme.text}>r                 Refresh active worklist</text>
      <text fg={theme.text}>?                 Toggle this help</text>
      <text fg={theme.text}>Esc               Close overlay or clear filter</text>
      <text fg={theme.text}>q or Ctrl+C       Quit and restore terminal</text>
    </box>
  );
}
