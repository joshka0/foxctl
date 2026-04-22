#!/usr/bin/env bun

import { createCliRenderer } from "@opentui/core";
import { createRoot } from "@opentui/react";
import { App } from "./App";

const renderer = await createCliRenderer({
  screenMode: "alternate-screen",
  exitOnCtrlC: false,
  targetFps: 30,
});

let shuttingDown = false;

const shutdown = (code = 0) => {
  if (shuttingDown) {
    process.exit(code);
  }
  shuttingDown = true;
  renderer.destroy();
  process.exit(code);
};

process.on("uncaughtException", (error) => {
  renderer.destroy();
  console.error(error);
  process.exit(1);
});

process.on("unhandledRejection", (reason) => {
  renderer.destroy();
  console.error(reason);
  process.exit(1);
});

process.once("SIGINT", () => shutdown(0));
process.once("SIGTERM", () => shutdown(0));
process.once("beforeExit", () => {
  if (!shuttingDown) {
    renderer.destroy();
  }
});

createRoot(renderer).render(<App onExit={() => shutdown(0)} />);
