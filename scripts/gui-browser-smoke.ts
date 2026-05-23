#!/usr/bin/env bun

import { mkdtempSync, rmSync } from "node:fs";
import { existsSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { AddressInfo } from "node:net";
import { createServer } from "node:net";

type Args = {
  routes: string[];
  chrome: string;
  previewPort: number;
  timeoutMS: number;
  server: "dev" | "preview";
  backendPort: number;
};

type CDPError = {
  route: string;
  source: string;
  text: string;
};

type CDPResponse<T> = {
  id: number;
  result?: T;
  error?: { message?: string; data?: string };
};

type RuntimeEvaluateResult = {
  result?: {
    value?: {
      title: string;
      heading: string;
      hash: string;
      rootChildren: number;
      bodyTextLength: number;
      bodyTextPreview: string;
    };
  };
};

const repoRoot = process.cwd();

function usage(): never {
  console.error(`Usage:
  bun scripts/gui-browser-smoke.ts [--routes /,/#rooms,/#orchestration] [--server dev|preview] [--chrome /path/to/chrome] [--preview-port 41743]

Starts packages/gui-agent through Vite and opens each route in headless Chrome.
Fails when the route renders a blank root, the document response is not 2xx, or
Chrome reports console/runtime errors.
`);
  process.exit(1);
}

function parseArgs(argv: string[]): Args {
  const args: Args = {
    routes: ["/", "/#rooms", "/#orchestration"],
    chrome: "",
    previewPort: 0,
    timeoutMS: 20_000,
    server: "dev",
    backendPort: 0,
  };

  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (
      arg === "--routes" ||
      arg === "--chrome" ||
      arg === "--preview-port" ||
      arg === "--timeout-ms" ||
      arg === "--server" ||
      arg === "--backend-port"
    ) {
      const value = argv[i + 1];
      if (!value || value.startsWith("--")) usage();
      if (arg === "--routes") args.routes = value.split(",").map((route) => route.trim()).filter(Boolean);
      if (arg === "--chrome") args.chrome = value;
      if (arg === "--preview-port") args.previewPort = Number(value);
      if (arg === "--timeout-ms") args.timeoutMS = Number(value);
      if (arg === "--backend-port") args.backendPort = Number(value);
      if (arg === "--server") {
        if (value !== "dev" && value !== "preview") usage();
        args.server = value;
      }
      i += 1;
      continue;
    }
    usage();
  }

  if (args.routes.length === 0 || !Number.isFinite(args.timeoutMS) || args.timeoutMS <= 0) usage();
  return args;
}

async function getFreePort(): Promise<number> {
  return await new Promise((resolve, reject) => {
    const server = createServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address() as AddressInfo;
      server.close(() => resolve(address.port));
    });
  });
}

async function resolveChrome(explicit: string): Promise<string> {
  if (explicit) return explicit;
  for (const candidate of [
    "google-chrome-stable",
    "google-chrome",
    "chromium-browser",
    "chromium",
    "/snap/bin/chromium",
  ]) {
    const proc = Bun.spawn(["bash", "-lc", `command -v ${candidate}`], {
      stdout: "pipe",
      stderr: "ignore",
    });
    const text = await new Response(proc.stdout).text();
    if ((await proc.exited) === 0 && text.trim()) return text.trim();
  }
  throw new Error("no Chrome/Chromium executable found");
}

async function waitForHTTP(url: string, timeoutMS: number): Promise<void> {
  const deadline = Date.now() + timeoutMS;
  let lastError = "";
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok) return;
      lastError = `status ${response.status}`;
    } catch (error) {
      lastError = error instanceof Error ? error.message : String(error);
    }
    await Bun.sleep(250);
  }
  throw new Error(`timed out waiting for ${url}: ${lastError}`);
}

function spawnLogged(name: string, command: string[], env?: Record<string, string>): Bun.Subprocess<"ignore", "pipe", "pipe"> {
  const proc = Bun.spawn(command, {
    cwd: repoRoot,
    stdout: "pipe",
    stderr: "pipe",
    env: { ...process.env, ...env },
  });
  void streamLogs(name, proc.stdout);
  void streamLogs(name, proc.stderr);
  return proc;
}

async function streamLogs(name: string, stream: ReadableStream<Uint8Array> | null): Promise<void> {
  if (!stream) return;
  const reader = stream.pipeThrough(new TextDecoderStream()).getReader();
  for (;;) {
    const { value, done } = await reader.read();
    if (done) return;
    for (const line of value.split("\n")) {
      const trimmed = line.trim();
      if (trimmed) console.error(`[${name}] ${trimmed}`);
    }
  }
}

class CDPClient {
  private nextID = 1;
  private pending = new Map<number, { resolve: (value: unknown) => void; reject: (error: Error) => void }>();
  readonly errors: CDPError[] = [];

  constructor(private readonly socket: WebSocket, private route: string) {
    socket.addEventListener("message", (event) => this.handleMessage(String(event.data)));
  }

  static async connect(url: string, route: string): Promise<CDPClient> {
    const socket = new WebSocket(url);
    await new Promise<void>((resolve, reject) => {
      socket.addEventListener("open", () => resolve(), { once: true });
      socket.addEventListener("error", () => reject(new Error(`failed to connect CDP socket: ${url}`)), { once: true });
    });
    return new CDPClient(socket, route);
  }

  setRoute(route: string): void {
    this.route = route;
  }

  clearErrors(): void {
    this.errors.length = 0;
  }

  send<T>(method: string, params: Record<string, unknown> = {}): Promise<T> {
    const id = this.nextID;
    this.nextID += 1;
    const message = JSON.stringify({ id, method, params });
    return new Promise<T>((resolve, reject) => {
      this.pending.set(id, { resolve: resolve as (value: unknown) => void, reject });
      this.socket.send(message);
    });
  }

  close(): void {
    this.socket.close();
  }

  private handleMessage(raw: string): void {
    const msg = JSON.parse(raw) as CDPResponse<unknown> & { method?: string; params?: Record<string, unknown> };
    if (msg.id) {
      const pending = this.pending.get(msg.id);
      if (!pending) return;
      this.pending.delete(msg.id);
      if (msg.error) {
        pending.reject(new Error(`${msg.error.message || "CDP error"} ${msg.error.data || ""}`.trim()));
      } else {
        pending.resolve(msg.result);
      }
      return;
    }
    this.recordEvent(msg.method || "", msg.params || {});
  }

  private recordEvent(method: string, params: Record<string, unknown>): void {
    if (method === "Runtime.exceptionThrown") {
      this.errors.push({ route: this.route, source: "runtime", text: JSON.stringify(params) });
      return;
    }
    if (method === "Runtime.consoleAPICalled" && (params.type === "error" || params.type === "assert")) {
      this.errors.push({ route: this.route, source: "console", text: JSON.stringify(params.args || []) });
      return;
    }
    if (method === "Log.entryAdded") {
      const entry = params.entry as { level?: string; text?: string } | undefined;
      if (entry?.level === "error") this.errors.push({ route: this.route, source: "log", text: entry.text || "" });
      return;
    }
    if (method === "Network.responseReceived") {
      const response = params.response as { status?: number; url?: string } | undefined;
      if (response?.url?.includes("/assets/") && response.status && response.status >= 400) {
        this.errors.push({ route: this.route, source: "network", text: `${response.status} ${response.url}` });
      }
    }
  }
}

async function openPage(debugPort: number, url: string): Promise<string> {
  const response = await fetch(`http://127.0.0.1:${debugPort}/json/new?${encodeURIComponent(url)}`, { method: "PUT" });
  if (!response.ok) throw new Error(`Chrome target create failed: HTTP ${response.status}`);
  const target = (await response.json()) as { webSocketDebuggerUrl?: string };
  if (!target.webSocketDebuggerUrl) throw new Error("Chrome target response did not include webSocketDebuggerUrl");
  return target.webSocketDebuggerUrl;
}

async function enablePage(client: CDPClient): Promise<void> {
  await client.send("Page.enable");
  await client.send("Runtime.enable");
  await client.send("Log.enable");
  await client.send("Network.enable");
}

async function readRendered(client: CDPClient): Promise<RuntimeEvaluateResult["result"]["value"]> {
  const expression = `(() => {
    const root = document.getElementById("root");
    const text = (document.body.innerText || "").trim();
    return {
      title: document.title,
      heading: (document.querySelector("h1")?.textContent || "").trim(),
      hash: window.location.hash,
      rootChildren: root ? root.children.length : 0,
      bodyTextLength: text.length,
      bodyTextPreview: text.slice(0, 240)
    };
  })()`;
  const result = await client.send<RuntimeEvaluateResult>("Runtime.evaluate", {
    expression,
    returnByValue: true,
  });
  return result.result?.value;
}

async function assertRendered(client: CDPClient, route: string, timeoutMS: number): Promise<void> {
  const deadline = Date.now() + timeoutMS;
  const expectedHeading = expectedHeadingForRoute(route);
  let value: RuntimeEvaluateResult["result"]["value"] | undefined;
  while (Date.now() < deadline) {
    value = await readRendered(client);
    if (
      value &&
      value.rootChildren > 0 &&
      value.bodyTextLength > 0 &&
      (!expectedHeading || value.heading === expectedHeading)
    ) {
      break;
    }
    await Bun.sleep(250);
  }
  if (!value) throw new Error(`${route}: unable to evaluate rendered DOM`);
  if (value.rootChildren <= 0) throw new Error(`${route}: #root rendered no child elements`);
  if (value.bodyTextLength <= 0) throw new Error(`${route}: document body rendered no visible text`);
  if (/checking access|session is not active|could not verify your sign-in/i.test(value.bodyTextPreview)) {
    throw new Error(`${route}: rendered auth gate instead of the application shell`);
  }
  if (expectedHeading && value.heading !== expectedHeading) {
    throw new Error(`${route}: heading=${JSON.stringify(value.heading)} want ${JSON.stringify(expectedHeading)}`);
  }
  console.log(`${route} ok: ${value.bodyTextLength} chars rendered, heading=${value.heading || "(none)"}`);
}

function expectedHeadingForRoute(route: string): string {
  const hash = route.includes("#") ? route.slice(route.indexOf("#") + 1).split("?")[0] : "";
  switch (hash) {
    case "rooms":
      return "Rooms";
    case "orchestration":
      return "Orchestration";
    case "":
      return "Runtime";
    default:
      return "";
  }
}

async function main(): Promise<void> {
  const args = parseArgs(Bun.argv.slice(2));
  const chrome = await resolveChrome(args.chrome);
  const previewPort = args.previewPort || (await getFreePort());
  const backendPort = args.backendPort || (await getFreePort());
  const debugPort = await getFreePort();
  const scratchDir = mkdtempSync(path.join(tmpdir(), "foxctl-gui-browser-smoke-"));
  const userDataDir = path.join(scratchDir, "chrome");
  const foxctlBin = path.join(repoRoot, "bin", "foxctl");
  if (!existsSync(foxctlBin)) {
    throw new Error("missing ./bin/foxctl; run make build before browser smoke");
  }

  const backend = spawnLogged(
    "foxctl-web",
    [foxctlBin, "web", "serve", "--port", String(backendPort), "--dev-cors"],
    {
      FOXCTL_HOME: path.join(scratchDir, "home"),
      FOXCTL_STORAGE_ROOT: path.join(scratchDir, "storage"),
      FOXCTL_PATHS_CAS: path.join(scratchDir, "cas"),
      FOXCTL_OBS_DIR: path.join(scratchDir, "observability"),
      FOXCTL_LOGGING_OUTPUT: path.join(scratchDir, "foxctl-web.log"),
    },
  );
  const preview = spawnLogged("vite-server", [
    "bun",
    "run",
    "--cwd",
    "packages/gui-agent",
    args.server,
    "--host",
    "127.0.0.1",
    "--port",
    String(previewPort),
  ], { FOXCTL_GUI_API_TARGET: `http://127.0.0.1:${backendPort}` });
  const chromeProc = spawnLogged("chrome", [
    chrome,
    "--headless=new",
    "--disable-gpu",
    "--no-sandbox",
    "--disable-dev-shm-usage",
    `--remote-debugging-port=${debugPort}`,
    `--user-data-dir=${userDataDir}`,
    "about:blank",
  ]);

  try {
    await waitForHTTP(`http://127.0.0.1:${backendPort}/api/health`, args.timeoutMS);
    await waitForHTTP(`http://127.0.0.1:${previewPort}/`, args.timeoutMS);
    await waitForHTTP(`http://127.0.0.1:${debugPort}/json/version`, args.timeoutMS);

    const wsURL = await openPage(debugPort, "about:blank");
    const client = await CDPClient.connect(wsURL, args.routes[0] || "/");
    await enablePage(client);
    for (const route of args.routes) {
      client.setRoute(route);
      client.clearErrors();
      const url = `http://127.0.0.1:${previewPort}${route.startsWith("/") ? route : `/${route}`}`;
      await client.send("Page.navigate", { url });
      await assertRendered(client, route, args.timeoutMS);
      if (client.errors.length > 0) {
        throw new Error(client.errors.map((error) => `${error.route} ${error.source}: ${error.text}`).join("\n"));
      }
    }
    client.close();
  } finally {
    chromeProc.kill();
    preview.kill();
    backend.kill();
    rmSync(scratchDir, { recursive: true, force: true });
  }
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : error);
  process.exit(1);
});
