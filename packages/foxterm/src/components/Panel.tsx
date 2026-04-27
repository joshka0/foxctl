import type { ReactNode } from "react";

import { theme, toneColor, type Tone } from "../theme";

type PanelSize = number | "auto" | `${number}%`;

export function Panel({
  title,
  subtitle,
  focused,
  children,
  width,
  minWidth,
  height,
  flexGrow,
  footer,
}: {
  title: string;
  subtitle?: string;
  focused: boolean;
  children: ReactNode;
  width?: PanelSize;
  minWidth?: number;
  height?: PanelSize;
  flexGrow?: number;
  footer?: ReactNode;
}) {
  return (
    <box
      style={{
        width,
        minWidth,
        height,
        flexGrow,
        border: true,
        borderStyle: "single",
        borderColor: focused ? theme.focus : theme.border,
        flexDirection: "column",
        padding: 1,
      }}
    >
      <box
        style={{
          flexDirection: "row",
          justifyContent: "space-between",
          height: 1,
        }}
      >
        <text fg={focused ? theme.focus : theme.text}>{title}</text>
        {subtitle && <text fg={theme.muted}>{subtitle}</text>}
      </box>
      {children}
      {footer && (
        <box style={{ height: 1, marginTop: 1 }}>
          {footer}
        </box>
      )}
    </box>
  );
}

export function PanelState({
  tone,
  title,
  detail,
}: {
  tone: Tone;
  title: string;
  detail?: string;
}) {
  return (
    <box
      style={{
        flexGrow: 1,
        justifyContent: "center",
        flexDirection: "column",
        gap: 1,
      }}
    >
      <text fg={toneColor(tone)}>{title}</text>
      {detail && <text fg={theme.muted}>{detail}</text>}
    </box>
  );
}
