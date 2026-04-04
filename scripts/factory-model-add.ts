#!/usr/bin/env bun

import { promises as fs } from "node:fs";
import os from "node:os";
import path from "node:path";

type JSONValue =
  | string
  | number
  | boolean
  | null
  | JSONValue[]
  | { [key: string]: JSONValue };

type FactoryModel = {
  model: string;
  displayName?: string;
  baseUrl: string;
  apiKey: string;
  provider: string;
  maxOutputTokens?: number;
  noImageSupport?: boolean;
  extraArgs?: Record<string, JSONValue>;
  extraHeaders?: Record<string, JSONValue>;
};

type FactorySettings = {
  customModels?: FactoryModel[];
  [key: string]: JSONValue | FactoryModel[] | undefined;
};

function usage(): never {
  console.error(`Usage:
  bun run factory:model:add -- --display-name "GPT 5.1 Codex Mini [OpenRouter]" \\
    --model "openai/gpt-5.1-codex-mini" \\
    --base-url "https://openrouter.ai/api/v1" \\
    --provider "generic-chat-completion-api" \\
    --api-key-env OPENROUTER_API_KEY \\
    --env-file .env

Options:
  --settings PATH           Target Factory settings file. Default: ~/.factory/settings.json
  --display-name NAME       Human-readable model name. Used as the upsert key.
  --model ID                Provider model id.
  --base-url URL            Provider base URL.
  --provider NAME           One of: anthropic, openai, generic-chat-completion-api.
  --api-key VALUE           Literal API key to write.
  --api-key-env NAME        Read API key from process env or --env-file, then write the resolved value.
  --api-key-ref NAME        Write \${NAME} into settings.json instead of a literal key.
  --env-file PATH           Optional .env file to read when using --api-key-env.
  --max-output-tokens N     Optional maxOutputTokens value.
  --no-image-support        Set noImageSupport: true.
  --supports-images         Set noImageSupport: false.
  --extra-args-json JSON    JSON object merged into extraArgs.
  --extra-headers-json JSON JSON object merged into extraHeaders.
  --dry-run                 Print the updated entry without writing the file.
`);
  process.exit(1);
}

function expandHome(input: string): string {
  if (input === "~") return os.homedir();
  if (input.startsWith("~/")) return path.join(os.homedir(), input.slice(2));
  return input;
}

function parseArgs(argv: string[]) {
  const args = new Map<string, string | boolean>();
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (!arg.startsWith("--")) usage();
    const key = arg.slice(2);
    if (["dry-run", "no-image-support", "supports-images"].includes(key)) {
      args.set(key, true);
      continue;
    }
    const next = argv[i + 1];
    if (!next || next.startsWith("--")) usage();
    args.set(key, next);
    i += 1;
  }
  return args;
}

async function readEnvFile(envFilePath: string): Promise<Record<string, string>> {
  const raw = await fs.readFile(envFilePath, "utf8");
  const env: Record<string, string> = {};
  for (const line of raw.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    const eq = trimmed.indexOf("=");
    if (eq <= 0) continue;
    const key = trimmed.slice(0, eq).trim();
    const value = trimmed.slice(eq + 1).trim();
    env[key] = value;
  }
  return env;
}

function parseJSONObject(name: string, value?: string): Record<string, JSONValue> | undefined {
  if (!value) return undefined;
  const parsed = JSON.parse(value) as JSONValue;
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error(`${name} must be a JSON object`);
  }
  return parsed as Record<string, JSONValue>;
}

async function resolveAPIKey(args: Map<string, string | boolean>): Promise<string> {
  const literal = args.get("api-key");
  const envName = args.get("api-key-env");
  const envRef = args.get("api-key-ref");

  const strategies = [literal, envName, envRef].filter(Boolean);
  if (strategies.length !== 1) {
    throw new Error("Provide exactly one of --api-key, --api-key-env, or --api-key-ref");
  }

  if (typeof literal === "string") return literal;
  if (typeof envRef === "string") return `\${${envRef}}`;

  const resolvedEnvName = String(envName);
  const fromProcess = process.env[resolvedEnvName];
  if (fromProcess) return fromProcess;

  const envFile = args.get("env-file");
  if (typeof envFile === "string") {
    const env = await readEnvFile(expandHome(envFile));
    const fromFile = env[resolvedEnvName];
    if (fromFile) return fromFile;
  }

  throw new Error(`Unable to resolve ${resolvedEnvName} from process env or --env-file`);
}

async function main() {
  const args = parseArgs(process.argv.slice(2));

  const displayName = args.get("display-name");
  const model = args.get("model");
  const baseUrl = args.get("base-url");
  const provider = args.get("provider");

  if (typeof displayName !== "string" || !displayName) usage();
  if (typeof model !== "string" || !model) usage();
  if (typeof baseUrl !== "string" || !baseUrl) usage();
  if (typeof provider !== "string" || !provider) usage();

  const settingsPath = expandHome(
    typeof args.get("settings") === "string" ? String(args.get("settings")) : "~/.factory/settings.json",
  );
  const apiKey = await resolveAPIKey(args);

  const modelEntry: FactoryModel = {
    model,
    displayName,
    baseUrl,
    apiKey,
    provider,
  };

  if (typeof args.get("max-output-tokens") === "string") {
    modelEntry.maxOutputTokens = Number.parseInt(String(args.get("max-output-tokens")), 10);
    if (Number.isNaN(modelEntry.maxOutputTokens)) {
      throw new Error("--max-output-tokens must be an integer");
    }
  }

  if (args.has("no-image-support")) modelEntry.noImageSupport = true;
  if (args.has("supports-images")) modelEntry.noImageSupport = false;

  const extraArgs = parseJSONObject("extraArgs", args.get("extra-args-json") as string | undefined);
  if (extraArgs) modelEntry.extraArgs = extraArgs;

  const extraHeaders = parseJSONObject("extraHeaders", args.get("extra-headers-json") as string | undefined);
  if (extraHeaders) modelEntry.extraHeaders = extraHeaders;

  let settings: FactorySettings = {};
  try {
    const existing = await fs.readFile(settingsPath, "utf8");
    settings = JSON.parse(existing) as FactorySettings;
  } catch (error) {
    if (!(error instanceof Error) || !("code" in error) || error.code !== "ENOENT") {
      throw error;
    }
  }

  const customModels = Array.isArray(settings.customModels) ? [...settings.customModels] : [];
  const index = customModels.findIndex(
    (entry) =>
      entry.displayName === displayName ||
      (entry.model === modelEntry.model && entry.baseUrl === modelEntry.baseUrl),
  );

  if (index >= 0) {
    customModels[index] = { ...customModels[index], ...modelEntry };
  } else {
    customModels.push(modelEntry);
  }

  settings.customModels = customModels;

  if (args.has("dry-run")) {
    console.log(JSON.stringify(modelEntry, null, 2));
    return;
  }

  await fs.mkdir(path.dirname(settingsPath), { recursive: true });
  await fs.writeFile(settingsPath, `${JSON.stringify(settings, null, 2)}\n`, "utf8");
  console.log(`Updated ${settingsPath} with ${displayName}`);
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
});
