/**
 * foxctl CLI wrapper utilities for OpenCode plugins
 *
 * Provides typed interfaces for calling foxctl skills from TypeScript plugins.
 */

import { $ } from "bun";

/**
 * Result envelope from foxctl skill execution
 */
export interface AgentctlResult<T = unknown> {
  success: boolean;
  data?: T;
  error?: string;
}

interface FoxctlEnvelope<T = unknown> {
  status?: string;
  data?: T;
  error?: {
    code?: string;
    message?: string;
  };
}

/**
 * Internal format for foxctl hooks/* skills (used by shell hooks)
 *
 * NOTE: This is NOT used by OpenCode plugins. OpenCode hooks return void.
 * Context injection is done via experimental.chat.system.transform
 * or custom tools - not via hook return values.
 */
export interface LegacyHookOutput {
  decision: "approve" | "block";
  reason?: string;
  context?: string;
}

/**
 * Options for running foxctl skills
 */
export interface RunSkillOptions {
  workspace?: string;
  ephemeral?: boolean;
  timeout?: number;
}

export function formatSkillFailure(
  skill: string,
  result: Pick<AgentctlResult, "error">
): string {
  const error = result.error?.trim() || "unknown error";
  return [`foxctl skill failed`, `skill: ${skill}`, `error: ${error}`].join("\n");
}

function parseSkillEnvelope<T>(skill: string, output: string): AgentctlResult<T> {
  const parsed = JSON.parse(output) as FoxctlEnvelope<T>;

  if (parsed.status === "ok") {
    return { success: true, data: parsed.data as T };
  }

  if (parsed.status === "error") {
    const code = parsed.error?.code?.trim();
    const message = parsed.error?.message?.trim();
    const detail = [code, message].filter(Boolean).join(": ");
    return {
      success: false,
      error: detail || `Skill ${skill} returned an error envelope`,
    };
  }

  return {
    success: false,
    error: `Skill ${skill} returned unexpected status ${parsed.status ?? "missing"}`,
  };
}

/**
 * Run an foxctl skill and return typed result
 *
 * @param skill - Skill name (e.g., "hooks/task_guard", "code/complexity")
 * @param input - Input JSON object for the skill
 * @param options - Execution options
 * @returns Typed result from the skill
 *
 * @example
 * ```ts
 * const result = await runSkill<{ hook_output: HookOutput }>(
 *   "hooks/task_guard",
 *   { event: "PreToolUse", tool_name: "Edit" },
 *   { ephemeral: true }
 * );
 * ```
 */
export async function runSkill<T = unknown>(
  skill: string,
  input: Record<string, unknown>,
  options?: RunSkillOptions
): Promise<AgentctlResult<T>> {
  const args = ["run", skill, "--input", JSON.stringify(input)];
  const foxctlBin = process.env.FOXCTL_BIN || "foxctl";

  // For OpenCode integration flows, prefer inline output over CAS digests.
  if (skill === "session/summarize") {
    args.push("--no-cas");
  }

  if (options?.workspace) {
    args.push("--workspace", options.workspace);
  }
  if (options?.ephemeral) {
    args.push("--ephemeral");
  }

  try {
    const proc = Bun.spawn([foxctlBin, ...args], {
      stdout: "pipe",
      stderr: "pipe",
    });

    // Apply timeout if specified
    const timeout = options?.timeout ?? 5000;
    let timeoutId: ReturnType<typeof setTimeout> | undefined;
    const timeoutPromise = new Promise<never>((_, reject) => {
      timeoutId = setTimeout(() => {
        proc.kill();
        reject(new Error(`Skill ${skill} timed out after ${timeout}ms`));
      }, timeout);
    });

    const resultPromise = (async () => {
      const [output, stderrOutput] = await Promise.all([
        new Response(proc.stdout).text(),
        new Response(proc.stderr).text(),
      ]);
      const exitCode = await proc.exited;

      if (exitCode !== 0) {
        return {
          success: false,
          error: stderrOutput || `Exit code ${exitCode}`,
        } as AgentctlResult<T>;
      }

      return parseSkillEnvelope<T>(skill, output);
    })();

    try {
      return await Promise.race([resultPromise, timeoutPromise]);
    } finally {
      if (timeoutId) {
        clearTimeout(timeoutId);
      }
    }
  } catch (error) {
    return {
      success: false,
      error: error instanceof Error ? error.message : String(error),
    };
  }
}

/**
 * Run an foxctl skill with input from a file
 *
 * @param skill - Skill name
 * @param inputPath - Path to input JSON file
 * @param options - Execution options
 */
export async function runSkillFromFile<T = unknown>(
  skill: string,
  inputPath: string,
  options?: RunSkillOptions
): Promise<AgentctlResult<T>> {
  const args = ["run", skill, "--input-file", inputPath];
  const foxctlBin = process.env.FOXCTL_BIN || "foxctl";

  // For OpenCode integration flows, prefer inline output over CAS digests.
  if (skill === "session/summarize") {
    args.push("--no-cas");
  }

  if (options?.workspace) {
    args.push("--workspace", options.workspace);
  }
  if (options?.ephemeral) {
    args.push("--ephemeral");
  }

  const timeout = options?.timeout ?? 30000;

  const proc = Bun.spawn([foxctlBin, ...args], {
    stdout: "pipe",
    stderr: "pipe",
  });

  const timeoutId = setTimeout(() => {
    proc.kill();
  }, timeout);

  try {
    const [stdout, stderr] = await Promise.all([
      new Response(proc.stdout).text(),
      new Response(proc.stderr).text(),
    ]);
    clearTimeout(timeoutId);

    const exitCode = await proc.exited;
    if (exitCode !== 0) {
      return {
        success: false,
        error: stderr || `Skill ${skill} exited with code ${exitCode}`,
      };
    }

    return parseSkillEnvelope<T>(skill, stdout);
  } catch (error) {
    clearTimeout(timeoutId);
    proc.kill();
    return {
      success: false,
      error: error instanceof Error ? error.message : String(error),
    };
  }
}

/**
 * Get workspace root from environment or context
 *
 * Checks multiple environment variables used by different AI coding tools:
 * - OPENCODE_PROJECT_DIR (OpenCode)
 * - CLAUDE_PROJECT_DIR (Claude Code)
 * - CURSOR_PROJECT_DIR (Cursor)
 * - Falls back to cwd
 */
export function getWorkspace(fallback?: string): string {
  return (
    process.env.OPENCODE_PROJECT_DIR ||
    process.env.CLAUDE_PROJECT_DIR ||
    process.env.CURSOR_PROJECT_DIR ||
    fallback ||
    process.cwd()
  );
}

/**
 * Get session ID from environment
 */
export function getSessionId(): string | undefined {
  return (
    process.env.OPENCODE_SESSION_ID ||
    process.env.FOXCTL_SESSION_ID ||
    process.env.CLAUDE_SESSION_ID
  );
}

/**
 * Format context for injection into AI conversation
 *
 * @param title - Section title
 * @param content - Content to display
 * @param format - Content format (text, code, or json)
 */
export function formatContext(
  title: string,
  content: string,
  format: "text" | "code" | "json" = "text"
): string {
  if (format === "code") {
    return `**${title}:**\n\`\`\`\n${content}\n\`\`\``;
  }
  if (format === "json") {
    return `**${title}:**\n\`\`\`json\n${content}\n\`\`\``;
  }
  return `**${title}:**\n${content}`;
}

/**
 * Check if a tool is a write operation
 */
export function isWriteTool(toolName: string): boolean {
  const writeTools = [
    "Edit",
    "Write",
    "MultiEdit",
    "NotebookEdit",
    "Bash", // Can write files
  ];
  return writeTools.includes(toolName);
}

/**
 * Check if a tool is a read operation
 */
export function isReadTool(toolName: string): boolean {
  const readTools = ["Read", "Glob", "Grep", "LSP"];
  return readTools.includes(toolName);
}

/**
 * Extract file path from tool input
 */
export function extractFilePath(
  toolInput: Record<string, unknown>
): string | undefined {
  // Different tools use different property names
  const pathProps = ["file_path", "path", "filePath", "file"];
  for (const prop of pathProps) {
    if (typeof toolInput[prop] === "string") {
      return toolInput[prop] as string;
    }
  }
  return undefined;
}
