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
      exitOnCtrlC: true,
      targetFps: 30,
      useMouse: true,
      useAlternateScreen: true,
    });

    // Create React root and render the app
    const root = createRoot(renderer);
    root.render(<App />);

    console.log("agentctl-tui started. Press Ctrl+C to exit.");

    // Start the render loop (switches to alternate screen)
    renderer.start();
  } catch (error) {
    console.error("Failed to start agentctl-tui:", error);
    process.exit(1);
  }
}

main();
