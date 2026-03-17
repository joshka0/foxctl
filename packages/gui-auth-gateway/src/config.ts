import path from "node:path";
import { fileURLToPath } from "node:url";

const packageDir = path.dirname(fileURLToPath(import.meta.url));
const workspaceRoot = path.resolve(packageDir, "../../..");

function env(name: string): string | undefined {
  const value = process.env[name];
  if (!value) return undefined;
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : undefined;
}

function envInt(name: string, fallback: number): number {
  const raw = env(name);
  if (!raw) return fallback;
  const parsed = Number.parseInt(raw, 10);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function envBool(name: string, fallback: boolean): boolean {
  const raw = env(name);
  if (!raw) return fallback;
  return ["1", "true", "yes", "on"].includes(raw.toLowerCase());
}

function envList(name: string): string[] {
  const raw = env(name);
  if (!raw) return [];
  return raw
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

function requireEnv(name: string): string {
  const value = env(name);
  if (!value) {
    throw new Error(`Missing required environment variable: ${name}`);
  }
  return value;
}

export interface GatewayConfig {
  port: number;
  upstreamURL: string;
  publicBaseURL: string;
  trustedOrigins: string[];
  guiDistDir: string;
  sqlitePath: string;
  loginPath: string;
  logoutPath: string;
  allowedEmails: string[];
  authSecret: string;
  databaseURL?: string;
  smtp:
    | {
        host: string;
        port: number;
        secure: boolean;
        user?: string;
        pass?: string;
        from: string;
        replyTo?: string;
      }
    | null;
  magicLink:
    | {
        logOnly: true;
      }
    | {
        logOnly: false;
      };
}

export function loadGatewayConfig(): GatewayConfig {
  const publicBaseURL = requireEnv("BETTER_AUTH_URL");
  const trustedOrigins = envList("GUI_AUTH_TRUSTED_ORIGINS");

  const smtpHost = env("GUI_AUTH_SMTP_HOST");
  const smtpFrom = env("GUI_AUTH_SMTP_FROM");
  const smtp =
    smtpHost && smtpFrom
      ? {
          host: smtpHost,
          port: envInt("GUI_AUTH_SMTP_PORT", 587),
          secure: envBool("GUI_AUTH_SMTP_SECURE", false),
          user: env("GUI_AUTH_SMTP_USER"),
          pass: env("GUI_AUTH_SMTP_PASS"),
          from: smtpFrom,
          replyTo: env("GUI_AUTH_SMTP_REPLY_TO"),
        }
      : null;

  const logOnlyMagicLinks = envBool("GUI_AUTH_MAGIC_LINK_LOG_ONLY", smtp == null);

  return {
    port: envInt("PORT", 3005),
    upstreamURL: env("GUI_AUTH_UPSTREAM_URL") ?? "http://127.0.0.1:8090",
    publicBaseURL,
    trustedOrigins: Array.from(new Set([publicBaseURL, ...trustedOrigins])),
    guiDistDir:
      env("GUI_AUTH_DIST_DIR") ?? path.resolve(workspaceRoot, "packages/gui-agent/dist"),
    sqlitePath: env("GUI_AUTH_SQLITE_PATH") ?? path.resolve("/tmp", "gui-auth-gateway.sqlite"),
    loginPath: env("GUI_AUTH_LOGIN_PATH") ?? "/login",
    logoutPath: env("GUI_AUTH_LOGOUT_PATH") ?? "/logout",
    allowedEmails: envList("GUI_AUTH_ALLOWED_EMAILS").map((value) => value.toLowerCase()),
    authSecret: requireEnv("BETTER_AUTH_SECRET"),
    databaseURL: env("GUI_AUTH_DATABASE_URL") ?? env("AGENTCTL_POSTGRES_DSN"),
    smtp,
    magicLink: logOnlyMagicLinks ? { logOnly: true } : { logOnly: false },
  };
}
