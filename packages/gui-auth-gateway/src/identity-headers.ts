import type { IncomingHttpHeaders } from "node:http";

export type SessionLike = {
  user?: {
    id?: string | null;
    email?: string | null;
    name?: string | null;
  } | null;
} | null | undefined;

export const trustedIdentityHeaders = [
  "x-betterauth-user-id",
  "x-betterauth-email",
  "x-betterauth-user-name",
  "x-tailscale-user",
  "x-tailscale-user-name",
  "x-tailscale-node",
  "x-tailscale-node-id",
  "x-foxctl-tenant-id",
  "x-foxctl-workspace-id",
  "x-foxctl-workspace-root",
  "x-foxctl-session-id",
] as const;

export function stripTrustedIdentityHeaders(headers: IncomingHttpHeaders) {
  for (const header of trustedIdentityHeaders) {
    delete headers[header];
  }
}

export function sessionIdentityHeaderEntries(session: SessionLike): [string, string][] {
  const entries: [string, string][] = [];
  const user = session?.user;
  if (user?.id) {
    entries.push(["x-betterauth-user-id", user.id]);
  }
  if (user?.email) {
    entries.push(["x-betterauth-email", user.email]);
  }
  if (user?.name) {
    entries.push(["x-betterauth-user-name", user.name]);
  }
  return entries;
}

export function applySessionIdentityHeaders(headers: IncomingHttpHeaders, session: SessionLike) {
  stripTrustedIdentityHeaders(headers);
  for (const [header, value] of sessionIdentityHeaderEntries(session)) {
    headers[header] = value;
  }
}
