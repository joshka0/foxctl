import { Key, matchesKey, truncateToWidth, type Component } from "@mariozechner/pi-tui";

type SurfaceKey = "runtime" | "orchestration" | "rooms" | "activity";

interface SurfaceDef {
  key: SurfaceKey;
  shortcut: "1" | "2" | "3" | "4";
  label: string;
  summary: string;
  nextSteps: string[];
}

const SURFACES: SurfaceDef[] = [
  {
    key: "runtime",
    shortcut: "1",
    label: "Runtime",
    summary: "Active agents, states, and direct operator actions.",
    nextSteps: [
      "Add runtime inventory backed by foxctl read models.",
      "Add selected-agent detail and bounded follow-up actions.",
    ],
  },
  {
    key: "orchestration",
    shortcut: "2",
    label: "Orchestration",
    summary: "Issue flow, delegation, and board-backed coordination.",
    nextSteps: [
      "Project orchestration board rows into a terminal-native list/detail workflow.",
      "Add dispatch, retry, and release actions without duplicating backend policy.",
    ],
  },
  {
    key: "rooms",
    shortcut: "3",
    label: "Rooms",
    summary: "Shared coordination channels and room-scoped follow-up work.",
    nextSteps: [
      "Show room directory and room ownership signals.",
      "Add room dispatch and preview overlays.",
    ],
  },
  {
    key: "activity",
    shortcut: "4",
    label: "Activity",
    summary: "Summary-first event and evidence triage.",
    nextSteps: [
      "Project recent errors, slow ops, and active traces first.",
      "Keep raw payloads behind explicit detail views.",
    ],
  },
];

function cycleSurface(current: number, direction: 1 | -1): number {
  return (current + direction + SURFACES.length) % SURFACES.length;
}

function pad(line: string, width: number): string {
  const fitted = truncateToWidth(line, width);
  if (fitted.length >= width) return fitted;
  return fitted + " ".repeat(width - fitted.length);
}

export class App implements Component {
  private activeSurfaceIndex = 0;
  private showHelp = false;
  private readonly workspace = process.cwd();
  private readonly onQuit: (code?: number) => void;

  constructor(onQuit: (code?: number) => void) {
    this.onQuit = onQuit;
  }

  invalidate(): void {}

  handleInput(data: string): void {
    if (matchesKey(data, Key.ctrl("c")) || matchesKey(data, "q")) {
      this.onQuit(0);
      return;
    }

    if (matchesKey(data, "1")) {
      this.activeSurfaceIndex = 0;
      return;
    }
    if (matchesKey(data, "2")) {
      this.activeSurfaceIndex = 1;
      return;
    }
    if (matchesKey(data, "3")) {
      this.activeSurfaceIndex = 2;
      return;
    }
    if (matchesKey(data, "4")) {
      this.activeSurfaceIndex = 3;
      return;
    }

    if (matchesKey(data, Key.tab) || matchesKey(data, Key.right) || matchesKey(data, "l")) {
      this.activeSurfaceIndex = cycleSurface(this.activeSurfaceIndex, 1);
      return;
    }
    if (matchesKey(data, Key.left) || matchesKey(data, "h")) {
      this.activeSurfaceIndex = cycleSurface(this.activeSurfaceIndex, -1);
      return;
    }

    if (matchesKey(data, "?")) {
      this.showHelp = !this.showHelp;
    }
  }

  render(width: number): string[] {
    const current = SURFACES[this.activeSurfaceIndex] ?? SURFACES[0];
    const lines: string[] = [];

    lines.push(pad("foxctl tui-agent  |  pi-tui control-plane scaffold", width));
    lines.push(
      pad(
        SURFACES.map((surface, index) => {
          const active = index === this.activeSurfaceIndex ? ">" : " ";
          return `${active}[${surface.shortcut}] ${surface.label}`;
        }).join("  "),
        width,
      ),
    );
    lines.push(pad("", width));
    lines.push(pad(`Surface: ${current.label}`, width));
    lines.push(pad(current.summary, width));
    lines.push(pad(`Workspace: ${this.workspace}`, width));
    lines.push(pad("", width));

    if (this.showHelp) {
      lines.push(pad("Keys", width));
      lines.push(pad("1-4 switch surfaces", width));
      lines.push(pad("tab/right/l next surface", width));
      lines.push(pad("left/h previous surface", width));
      lines.push(pad("? toggle help", width));
      lines.push(pad("q or ctrl+c quit", width));
    } else {
      lines.push(pad("Phase 0", width));
      lines.push(pad("This shell starts the new terminal control plane on top of pi-tui.", width));
      lines.push(pad("Next steps", width));
      for (const step of current.nextSteps) {
        lines.push(pad(`- ${step}`, width));
      }
    }

    lines.push(pad("", width));
    lines.push(pad(`status: ${current.key} | ${this.showHelp ? "help open" : "plan view"} | q quit`, width));
    return lines;
  }
}
