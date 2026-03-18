import express, { type NextFunction, type Request, type Response } from "express";
import { fromNodeHeaders, toNodeHandler } from "better-auth/node";
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import httpProxy from "http-proxy";
import { createProxyMiddleware } from "http-proxy-middleware";

import { createAuth, getSessionFromHeaders } from "./auth.js";
import { loadGatewayConfig } from "./config.js";
import { renderLoginPage } from "./login-page.js";

const config = loadGatewayConfig();
const auth = createAuth(config);
const app = express();
const wsProxy = httpProxy.createProxyServer({
  target: config.upstreamURL,
  changeOrigin: true,
  ws: true,
  xfwd: true,
});

type AuthenticatedRequest = Request & {
  authSession?: Awaited<ReturnType<typeof getSessionFromHeaders>>;
};

function isAllowedEmail(email: string | undefined): boolean {
  if (!email) return false;
  if (config.allowedEmails.length === 0) return true;
  return config.allowedEmails.includes(email.trim().toLowerCase());
}

async function loadSession(req: AuthenticatedRequest): Promise<AuthenticatedRequest["authSession"]> {
  if (req.authSession !== undefined) {
    return req.authSession;
  }
  req.authSession = await getSessionFromHeaders(auth, req.headers);
  return req.authSession;
}

function unauthorizedJSON(res: Response) {
  res.status(401).json({
    error: {
      code: "EAUTH",
      message: "Authentication required",
    },
  });
}

function requireSession(options: { redirectToLogin?: boolean } = {}) {
  return async (req: AuthenticatedRequest, res: Response, next: NextFunction) => {
    try {
      const session = await loadSession(req);
      const email = session?.user?.email;
      if (!session || !isAllowedEmail(email)) {
        if (options.redirectToLogin) {
          const nextPath = encodeURIComponent(req.originalUrl || "/");
          res.redirect(`${config.loginPath}?next=${nextPath}`);
          return;
        }
        unauthorizedJSON(res);
        return;
      }
      next();
    } catch (error) {
      next(error);
    }
  };
}

function serveGuiIndex(_req: Request, res: Response) {
  const indexPath = path.join(config.guiDistDir, "index.html");
  if (!fs.existsSync(indexPath)) {
    res.status(503).type("text/plain").send(
      `gui-agent build output not found at ${config.guiDistDir}. Run "bun run --cwd packages/gui-agent build" before starting the public gateway.`,
    );
    return;
  }
  res.sendFile(indexPath);
}

app.disable("x-powered-by");

app.get("/healthz", (_req, res) => {
  res.status(200).json({ ok: true });
});

app.get("/readyz", (_req, res) => {
  const hasDist = fs.existsSync(path.join(config.guiDistDir, "index.html"));
  res.status(hasDist ? 200 : 503).json({ ok: hasDist });
});

app.get(config.loginPath, async (req: AuthenticatedRequest, res: Response) => {
  const session = await loadSession(req);
  const nextPath =
    typeof req.query.next === "string" && req.query.next.startsWith("/")
      ? req.query.next
      : "/";
  if (session?.user && isAllowedEmail(session.user.email)) {
    res.redirect(nextPath);
    return;
  }
  const error =
    typeof req.query.error === "string" && req.query.error.trim().length > 0
      ? req.query.error.trim()
      : undefined;
  res.status(200).type("html").send(renderLoginPage(config, error));
});

app.post(config.logoutPath, async (req: AuthenticatedRequest, res: Response, next: NextFunction) => {
  try {
    await auth.api.signOut({
      headers: fromNodeHeaders(req.headers),
    });
    res.status(204).end();
  } catch (error) {
    next(error);
  }
});

app.get("/api/auth/session", async (req: AuthenticatedRequest, res: Response, next: NextFunction) => {
  try {
    const session = await loadSession(req);
    if (!session || !isAllowedEmail(session.user?.email)) {
      unauthorizedJSON(res);
      return;
    }
    res.status(200).json(session);
  } catch (error) {
    next(error);
  }
});

app.all("/api/auth/*", toNodeHandler(auth));

const apiProxy = createProxyMiddleware({
  target: config.upstreamURL,
  changeOrigin: true,
  xfwd: true,
  ws: false,
  pathFilter: "/api",
});

app.use("/api", requireSession());
app.use(apiProxy);

app.use(
  "/assets",
  requireSession(),
  express.static(path.join(config.guiDistDir, "assets"), { fallthrough: false }),
);
app.use(
  "/vite.svg",
  requireSession(),
  express.static(path.join(config.guiDistDir, "vite.svg"), { fallthrough: false }),
);
app.use(
  "/favicon.ico",
  requireSession(),
  express.static(path.join(config.guiDistDir, "favicon.ico"), { fallthrough: false }),
);

app.get("/", requireSession({ redirectToLogin: true }), serveGuiIndex);
app.get("/*", requireSession({ redirectToLogin: true }), serveGuiIndex);

app.use((error: unknown, _req: Request, res: Response, _next: NextFunction) => {
  const message = error instanceof Error ? error.message : "Unexpected gateway failure";
  console.error("[gui-auth-gateway]", error);
  res.status(500).json({
    error: {
      code: "EGATEWAY",
      message,
    },
  });
});

const server = http.createServer(app);

server.on("upgrade", async (req, socket, head) => {
  try {
    if (!req.url?.startsWith("/ws/")) {
      socket.destroy();
      return;
    }
    const session = await getSessionFromHeaders(auth, req.headers);
    if (!session || !isAllowedEmail(session.user?.email)) {
      socket.write("HTTP/1.1 401 Unauthorized\r\nConnection: close\r\n\r\n");
      socket.destroy();
      return;
    }
    wsProxy.ws(req, socket, head);
  } catch (error) {
    console.error("[gui-auth-gateway] websocket auth failed", error);
    socket.write("HTTP/1.1 500 Internal Server Error\r\nConnection: close\r\n\r\n");
    socket.destroy();
  }
});

async function main() {
  try {
    const ctx = await auth.$context;
    await ctx.runMigrations();
  } catch (error) {
    console.error("[gui-auth-gateway] failed to run auth migrations", error);
    process.exit(1);
  }

  server.listen(config.port, () => {
    console.info(
      `[gui-auth-gateway] listening on http://0.0.0.0:${config.port} -> ${config.upstreamURL}`,
    );
  });
}

void main();
