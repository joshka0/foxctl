import { describe, expect, test } from "bun:test";

import {
  applySessionIdentityHeaders,
  sessionIdentityHeaderEntries,
  stripTrustedIdentityHeaders,
} from "./identity-headers.js";

describe("identity header forwarding", () => {
  test("strips spoofable trusted identity headers", () => {
    const headers: Record<string, string> = {
      "x-betterauth-email": "spoof@example.com",
      "x-tailscale-user": "spoof@tailnet",
      "x-foxctl-tenant-id": "tenant-spoof",
      accept: "application/json",
    };

    stripTrustedIdentityHeaders(headers);

    expect(headers).toEqual({ accept: "application/json" });
  });

  test("builds only session-derived Better Auth headers", () => {
    const entries = sessionIdentityHeaderEntries({
      user: {
        id: "user-123",
        email: "user@example.com",
        name: "User Name",
      },
    });

    expect(entries).toEqual([
      ["x-betterauth-user-id", "user-123"],
      ["x-betterauth-email", "user@example.com"],
      ["x-betterauth-user-name", "User Name"],
    ]);
  });

  test("applies session identity after stripping spoofed headers", () => {
    const headers: Record<string, string> = {
      "x-betterauth-email": "spoof@example.com",
      "x-tailscale-user": "spoof@tailnet",
      "x-foxctl-workspace-id": "spoof-workspace",
    };

    applySessionIdentityHeaders(headers, {
      user: {
        id: "user-123",
        email: "real@example.com",
      },
    });

    expect(headers).toEqual({
      "x-betterauth-user-id": "user-123",
      "x-betterauth-email": "real@example.com",
    });
  });

  test("rebuilds identity for terminal WebSocket upgrade headers", () => {
    const headers: Record<string, string> = {
      connection: "Upgrade",
      upgrade: "websocket",
      host: "localhost:3001",
      "sec-websocket-key": "test-upgrade-key",
      "sec-websocket-version": "13",
      "x-betterauth-user-id": "spoof-user",
      "x-betterauth-email": "spoof@example.com",
      "x-tailscale-user": "spoof@tailnet",
      "x-foxctl-session-id": "spoof-session",
    };

    applySessionIdentityHeaders(headers, {
      user: {
        id: "user-123",
        email: "real@example.com",
        name: "Real User",
      },
    });

    expect(headers).toEqual({
      connection: "Upgrade",
      upgrade: "websocket",
      host: "localhost:3001",
      "sec-websocket-key": "test-upgrade-key",
      "sec-websocket-version": "13",
      "x-betterauth-user-id": "user-123",
      "x-betterauth-email": "real@example.com",
      "x-betterauth-user-name": "Real User",
    });
  });
});
