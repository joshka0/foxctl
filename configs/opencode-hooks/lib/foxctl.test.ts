import { afterEach, describe, expect, test } from "bun:test";
import { chmod, mkdtemp, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { formatSkillFailure, runSkill } from "./foxctl";

const originalPath = process.env.PATH;
const originalFoxctlBin = process.env.FOXCTL_BIN;
const tempDirs: string[] = [];

afterEach(async () => {
  process.env.PATH = originalPath;
  process.env.FOXCTL_BIN = originalFoxctlBin;
  await Promise.all(tempDirs.splice(0).map((dir) => rm(dir, { recursive: true, force: true })));
});

async function installFoxctlStub(script: string): Promise<void> {
  const dir = await mkdtemp(join(tmpdir(), "foxctl-opencode-hooks-"));
  tempDirs.push(dir);
  const path = join(dir, "foxctl");
  await writeFile(path, script);
  await chmod(path, 0o755);
  process.env.FOXCTL_BIN = path;
}

describe("runSkill", () => {
  test("preserves success-empty data as a successful result", async () => {
    await installFoxctlStub(`#!/usr/bin/env bash
printf '%s' '{"version":1,"status":"ok","command":"memory/query","data":{"records":[]},"meta":{"ts":"2026-05-22T00:00:00Z"},"error":{}}'
`);

    const result = await runSkill<{ records: unknown[] }>(
      "memory/query",
      { query: "missing" },
      { ephemeral: true }
    );

    expect(result).toEqual({ success: true, data: { records: [] } });
  });

  test("treats a foxctl error envelope as a failed result", async () => {
    await installFoxctlStub(`#!/usr/bin/env bash
printf '%s' '{"version":1,"status":"error","command":"memory/query","data":{},"meta":{"ts":"2026-05-22T00:00:00Z"},"error":{"code":"storage_unavailable","message":"database is locked"}}'
`);

    const result = await runSkill("memory/query", { query: "x" }, { ephemeral: true });

    expect(result).toEqual({
      success: false,
      error: "storage_unavailable: database is locked",
    });
  });

  test("treats non-terminal envelopes as failed results", async () => {
    await installFoxctlStub(`#!/usr/bin/env bash
printf '%s' '{"version":1,"status":"progress","command":"memory/query","data":{"records":[{"id":"pending"}]},"meta":{"ts":"2026-05-22T00:00:00Z"},"error":{}}'
`);

    const result = await runSkill("memory/query", { query: "x" }, { ephemeral: true });

    expect(result).toEqual({
      success: false,
      error: "Skill memory/query returned unexpected status progress",
    });
  });
});

describe("formatSkillFailure", () => {
  test("returns compact structured failure text", () => {
    expect(
      formatSkillFailure("memory/query", {
        error: "storage_unavailable: database is locked",
      })
    ).toBe(
      [
        "foxctl skill failed",
        "skill: memory/query",
        "error: storage_unavailable: database is locked",
      ].join("\n")
    );
  });
});
