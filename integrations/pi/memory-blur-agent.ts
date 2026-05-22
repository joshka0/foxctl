#!/usr/bin/env bun
import {
	AuthStorage,
	createAgentSession,
	DefaultResourceLoader,
	getAgentDir,
	ModelRegistry,
	SessionManager,
	SettingsManager,
} from "@earendil-works/pi-coding-agent";

type Options = {
	cwd: string;
	agentDir: string;
	provider: string;
	model: string;
	thinkingLevel: "off" | "minimal" | "low" | "medium" | "high" | "xhigh";
};

function parseArgs(argv: string[]): Options {
	const opts: Options = {
		cwd: process.cwd(),
		agentDir: getAgentDir(),
		provider: "",
		model: "",
		thinkingLevel: "off",
	};
	for (let i = 0; i < argv.length; i++) {
		const arg = argv[i];
		const value = argv[i + 1] || "";
		switch (arg) {
			case "--cwd":
				opts.cwd = value || opts.cwd;
				i++;
				break;
			case "--agent-dir":
				opts.agentDir = value || opts.agentDir;
				i++;
				break;
			case "--provider":
				opts.provider = value;
				i++;
				break;
			case "--model":
				opts.model = value;
				i++;
				break;
			case "--thinking-level":
				if (isThinkingLevel(value)) {
					opts.thinkingLevel = value;
				}
				i++;
				break;
			default:
				throw new Error(`unsupported argument: ${arg}`);
		}
	}
	return opts;
}

function isThinkingLevel(value: string): value is Options["thinkingLevel"] {
	return value === "off" || value === "minimal" || value === "low" || value === "medium" || value === "high" || value === "xhigh";
}

async function readStdin(): Promise<string> {
	const chunks: Buffer[] = [];
	for await (const chunk of process.stdin) {
		chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(String(chunk)));
	}
	return Buffer.concat(chunks).toString("utf8").trim();
}

function lastAssistantText(messages: unknown[]): string {
	for (let i = messages.length - 1; i >= 0; i--) {
		const message = messages[i] as { role?: string; content?: Array<{ type?: string; text?: string }> };
		if (message?.role !== "assistant" || !Array.isArray(message.content)) {
			continue;
		}
		return message.content
			.filter((block) => block?.type === "text" && typeof block.text === "string")
			.map((block) => block.text)
			.join("")
			.trim();
	}
	return "";
}

async function main(): Promise<void> {
	const opts = parseArgs(process.argv.slice(2));
	const prompt = await readStdin();
	if (!prompt) {
		throw new Error("memory blur prompt is required on stdin");
	}

	const authStorage = AuthStorage.create(`${opts.agentDir}/auth.json`);
	const modelRegistry = ModelRegistry.create(authStorage, `${opts.agentDir}/models.json`);
	const selectedModel =
		opts.provider && opts.model
			? modelRegistry.find(opts.provider, opts.model)
			: undefined;
	if (opts.provider && opts.model && !selectedModel) {
		throw new Error(`model not found: ${opts.provider}/${opts.model}`);
	}

	const settingsManager = SettingsManager.inMemory({
		compaction: { enabled: false },
		retry: { enabled: true, maxRetries: 1 },
	});
	const resourceLoader = new DefaultResourceLoader({
		cwd: opts.cwd,
		agentDir: opts.agentDir,
		settingsManager,
		noExtensions: true,
		noSkills: true,
		noPromptTemplates: true,
		noThemes: true,
		noContextFiles: true,
		systemPrompt: "Return only the JSON object requested by the user. Do not include markdown.",
	});
	await resourceLoader.reload();

	const { session } = await createAgentSession({
		cwd: opts.cwd,
		agentDir: opts.agentDir,
		authStorage,
		modelRegistry,
		model: selectedModel,
		thinkingLevel: opts.thinkingLevel,
		noTools: "all",
		resourceLoader,
		sessionManager: SessionManager.inMemory(),
		settingsManager,
	});

	let streamed = "";
	try {
		session.subscribe((event) => {
			if (event.type === "message_update" && event.assistantMessageEvent.type === "text_delta") {
				streamed += event.assistantMessageEvent.delta;
			}
		});
		await session.prompt(prompt, { source: "rpc" });
		const output = lastAssistantText(session.state.messages) || streamed.trim();
		if (!output) {
			throw new Error("pi sdk produced an empty assistant response");
		}
		process.stdout.write(output);
	} finally {
		session.dispose();
	}
}

main().catch((error) => {
	const message = error instanceof Error ? error.message : String(error);
	process.stderr.write(`pi sdk memory blur failed: ${message}\n`);
	process.exitCode = 1;
});
