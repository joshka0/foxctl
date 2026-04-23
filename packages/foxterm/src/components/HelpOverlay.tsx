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
        height: 30,
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
      <text fg={theme.text}>: / Ctrl+p        Open command palette</text>
      <text fg={theme.text}>1 / 2 / 3         Runs / Rooms / Cards</text>
      <text fg={theme.text}>[ / ]             Cycle scopes</text>
      <text fg={theme.text}>b / w             Hide Scope / worklist</text>
      <text fg={theme.text}>, / .             Resize focused side pane</text>
      <text fg={theme.text}>- / =             Resize room agent panes</text>
      <text fg={theme.text}>Up/Down or j/k    Move selection</text>
      <text fg={theme.text}>Enter             Open selected run detail</text>
      <text fg={theme.text}>n                 Compose run for active context</text>
      <text fg={theme.text}>+ / Shift+n       Create room or card</text>
      <text fg={theme.text}>m                 Message selected room</text>
      <text fg={theme.text}>s                 Spawn agent into room</text>
      <text fg={theme.text}>Shift+s           Spawn ATCP CLI into room</text>
      <text fg={theme.text}>v                 Cycle ATCP screen detail</text>
      <text fg={theme.text}>p                 Prompt focused ATCP session</text>
      <text fg={theme.text}>c                 Continue selected run</text>
      <text fg={theme.text}>Cards Enter       Open linked run</text>
      <text fg={theme.text}>Cards d/u/t       Done / release / retry</text>
      <text fg={theme.text}>x                 Kill run or stop ATCP session</text>
      <text fg={theme.text}>/                 Filter active worklist</text>
      <text fg={theme.text}>a                 Cycle activity scope</text>
      <text fg={theme.text}>r                 Refresh active worklist</text>
      <text fg={theme.text}>?                 Toggle this help</text>
      <text fg={theme.text}>Esc               Close overlay or clear filter</text>
      <text fg={theme.text}>q or Ctrl+C       Quit and restore terminal</text>
    </box>
  );
}
