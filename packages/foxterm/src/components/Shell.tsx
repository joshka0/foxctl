import type { ReactNode } from "react";

import type { ActivityScope, FocusRegion, Mode, StatusMessage } from "../types";
import { theme, toneColor } from "../theme";

export function AppFrame({
  children,
  mode,
  focus,
  compact,
  status,
  streamStatus,
  filterText,
  activityScope,
}: {
  children: ReactNode;
  mode: Mode;
  focus: FocusRegion;
  compact: boolean;
  status: StatusMessage;
  streamStatus: StatusMessage;
  filterText: string;
  activityScope: ActivityScope;
}) {
  return (
    <box
      style={{
        width: "100%",
        height: "100%",
        flexDirection: "column",
        backgroundColor: theme.bg,
      }}
    >
      <Header compact={compact} status={status} streamStatus={streamStatus} />
      {children}
      <Footer
        focus={focus}
        mode={mode}
        compact={compact}
        filterText={filterText}
        activityScope={activityScope}
      />
    </box>
  );
}

export function MainRegion({
  children,
  compact,
}: {
  children: ReactNode;
  compact: boolean;
}) {
  return (
    <box
      style={{
        flexGrow: 1,
        flexDirection: compact ? "column" : "row",
        gap: 1,
        padding: 1,
      }}
    >
      {children}
    </box>
  );
}

export function SmallTerminalNotice({
  width,
  height,
}: {
  width: number;
  height: number;
}) {
  return (
    <box
      style={{
        flexGrow: 1,
        border: true,
        borderStyle: "single",
        borderColor: theme.warning,
        flexDirection: "column",
        justifyContent: "center",
        padding: 1,
        gap: 1,
      }}
    >
      <text fg={theme.warning}>Terminal too small</text>
      <text fg={theme.text}>
        Current {width}x{height}; use at least 60x20.
      </text>
      <text fg={theme.muted}>q or Ctrl+C quits cleanly.</text>
    </box>
  );
}

function Header({
  compact,
  status,
  streamStatus,
}: {
  compact: boolean;
  status: StatusMessage;
  streamStatus: StatusMessage;
}) {
  return (
    <box
      style={{
        height: 3,
        border: true,
        borderStyle: "single",
        borderColor: theme.border,
        flexDirection: "row",
        justifyContent: "space-between",
        paddingLeft: 1,
        paddingRight: 1,
      }}
    >
      <text fg={theme.text}>{compact ? "foxterm" : "foxterm - agent terminal"}</text>
      <text fg={toneColor(status.tone)}>{truncate(status.text, compact ? 28 : 44)}</text>
      {!compact && (
        <text fg={toneColor(streamStatus.tone)}>
          stream {truncate(streamStatus.text, 28)}
        </text>
      )}
    </box>
  );
}

function Footer({
  focus,
  mode,
  compact,
  filterText,
  activityScope,
}: {
  focus: FocusRegion;
  mode: Mode;
  compact: boolean;
  filterText: string;
  activityScope: ActivityScope;
}) {
  const left =
    mode === "filter"
      ? `filter /${filterText}`
      : mode === "compose"
        ? "compose  Enter submit  Esc cancel"
        : mode === "createRoom"
          ? "new room  Enter create  Esc cancel"
          : mode === "roomMessage"
            ? "room message  Enter send  Esc cancel"
            : mode === "spawnAgent"
              ? "spawn agent  role[:prompt]  Enter"
              : mode === "spawnCLI"
                ? "spawn CLI  agent@adapter: cmd"
                : mode === "atcpPrompt"
                  ? "ATCP prompt  Enter send  Esc cancel"
        : mode === "confirmKill"
          ? "confirm kill  Enter confirm  Esc cancel"
          : mode === "confirmATCPStop"
            ? "confirm ATCP stop  Enter confirm  Esc cancel"
      : `${mode === "help" ? "help" : focus}  activity ${activityScope}`;
  return (
    <box
      style={{
        height: 1,
        flexDirection: "row",
        justifyContent: "space-between",
        backgroundColor: theme.panel,
        paddingLeft: 1,
        paddingRight: 1,
      }}
    >
      <text fg={theme.focus}>{truncate(left, compact ? 28 : 44)}</text>
      <text fg={theme.muted}>
        {compact
          ? "1/2/3 scope  h/l pane  j/k Enter  n + m s S v p c x / r ? q"
          : "Tab focus  1/2/3 scope  j/k move  Enter open  n context run  + room  m message  s agent  S cli  v atcp  p prompt  c continue  x kill/stop  / filter  a scope  r refresh  ? help  q quit"}
      </text>
    </box>
  );
}

function truncate(value: string, max: number): string {
  if (value.length <= max) return value;
  if (max <= 1) return value.slice(0, max);
  return `${value.slice(0, max - 1)}…`;
}
