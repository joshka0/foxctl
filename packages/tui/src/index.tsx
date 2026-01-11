#!/usr/bin/env bun
/**
 * agentctl-tui - Terminal UI for agentctl
 *
 * Built with OpenTUI + React
 */

import { createCliRenderer } from "@opentui/core";
import { createRoot } from "@opentui/react";
import { App } from "./App";

async function main() {
  console.log("Starting agentctl-tui...");

  try {
    // Create the OpenTUI renderer
    const renderer = await createCliRenderer({
      exitOnCtrlC: false,
      exitSignals: [],
      targetFps: 30,
      useMouse: true,
      useAlternateScreen: true,
    });

    // Create React root and render the app
    const root = createRoot(renderer);
    root.render(<App />);

    console.log("agentctl-tui started. Press Ctrl+C to exit.");

    let shuttingDown = false;
    const shutdown = (code = 0) => {
      if (shuttingDown) return;
      shuttingDown = true;
      try {
        root.unmount();
      } catch (err) {
        console.error("Failed to unmount TUI:", err);
      }
      try {
        renderer.destroy();
      } catch (err) {
        console.error("Failed to destroy renderer:", err);
      }
      process.exit(code);
    };

    (globalThis as { __agentctl_tui_shutdown?: (code?: number) => void }).__agentctl_tui_shutdown = shutdown;

    process.once("SIGINT", () => shutdown(0));
    process.once("SIGTERM", () => shutdown(0));
    process.once("SIGQUIT", () => shutdown(0));
  } catch (error) {
    console.error("Failed to start agentctl-tui:", error);
    process.exit(1);
  }
}

main();
