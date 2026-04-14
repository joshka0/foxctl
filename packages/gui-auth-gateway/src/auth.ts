import { betterAuth } from "better-auth";
import { fromNodeHeaders } from "better-auth/node";
import { magicLink } from "better-auth/plugins";
import { Database as BunSQLiteDatabase } from "bun:sqlite";
import type { IncomingHttpHeaders } from "node:http";
import { Pool } from "pg";

import type { GatewayConfig } from "./config.js";
import { sendMagicLinkEmail } from "./email.js";

function isAllowedEmail(config: GatewayConfig, email: string): boolean {
  if (config.allowedEmails.length === 0) return true;
  return config.allowedEmails.includes(email.trim().toLowerCase());
}

export function createAuth(config: GatewayConfig) {
  const database = config.databaseURL
    ? new Pool({ connectionString: config.databaseURL })
    : new BunSQLiteDatabase(config.sqlitePath, { create: true });

  const auth = betterAuth({
    secret: config.authSecret,
    appName: "foxctl gui-agent",
    baseURL: config.publicBaseURL,
    trustedOrigins: config.trustedOrigins,
    database,
    emailAndPassword: {
      enabled: false,
    },
    socialProviders: {},
    plugins: [
      magicLink({
        async sendMagicLink({ email, url }) {
          if (!isAllowedEmail(config, email)) {
            throw new Error("This email address is not allowed to access gui-agent");
          }
          await sendMagicLinkEmail(config, email, url);
        },
      }),
    ],
  });

  return auth;
}

export async function getSessionFromHeaders(
  auth: ReturnType<typeof createAuth>,
  headers: IncomingHttpHeaders,
) {
  return auth.api.getSession({
    headers: fromNodeHeaders(headers),
  });
}
