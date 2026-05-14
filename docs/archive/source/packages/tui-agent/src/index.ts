#!/usr/bin/env bun

import { ProcessTerminal, TUI } from "@mariozechner/pi-tui";
import { App } from "./App";

async function main() {
  const terminal = new ProcessTerminal();
  const tui = new TUI(terminal);

  let shuttingDown = false;
  const shutdown = async (code = 0) => {
    if (shuttingDown) return;
    shuttingDown = true;
    try {
      tui.stop();
    } catch {}
    try {
      await terminal.drainInput(150, 25);
    } catch {}
    process.exit(code);
  };

  const app = new App((code) => void shutdown(code ?? 0));
  tui.addChild(app);
  tui.setFocus(app);

  process.once("SIGINT", () => void shutdown(0));
  process.once("SIGTERM", () => void shutdown(0));
  process.once("SIGQUIT", () => void shutdown(0));

  tui.start();
}

void main().catch((error) => {
  console.error("Failed to start tui-agent:", error);
  process.exit(1);
});
