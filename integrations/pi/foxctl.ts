/**
 * foxctl-pi-extension
 *
 * Bridges the foxctl Go daemon with pi's extension system.
 * Provides tools and commands to interact with foxctl's HTTP API.
 *
 * Index:
 *   Purpose: Expose foxctl's daemon, retrieval, repoindex, memory, room, and foxprox surfaces as Pi TUI tools.
 *   Keywords: pi extension, foxctl tools, repoindex, memory search, room tools, foxprox
 *   Related: runFoxctlSkill, defineFoxctlSkillFacade, gatherFoxctlContext, bindPiToRoom
 *   Flow: Pi tool call -> foxctl HTTP API -> skill or room endpoint -> JSON tool result
 *   Resources: foxctl web API, terminal gateway, Pi extension API
 *
 * Usage:
 *   pi --extension /path/to/foxctl.ts --foxctl-url http://localhost:8090
 *
 * Or copy to ~/.pi/extensions/ for auto-discovery.
 */

import { StringEnum } from "@earendil-works/pi-ai";
import { defineTool, type ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Key, SelectList, truncateToWidth, visibleWidth, type Component } from "@earendil-works/pi-tui";
import { Type } from "typebox";

// ============================================================================
// HTTP Client
// ============================================================================

class FoxctlClient {
	private baseUrl: string;

	constructor(baseUrl: string) {
		this.baseUrl = baseUrl.replace(/\/$/, "");
	}

	private async request<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
		let res: Response;
		try {
			res = await fetch(`${this.baseUrl}${path}`, {
				method,
				headers: body === undefined ? undefined : { "Content-Type": "application/json" },
				body: body === undefined ? undefined : JSON.stringify(body),
				signal,
			});
		} catch (error) {
			const message = error instanceof Error ? error.message : String(error);
			throw new Error(`foxctl ${method} ${path}: ${message}. Start foxctl with: foxctl web serve --dev-cors --port 8090`);
		}

		const contentType = res.headers.get("content-type") || "";
		const text = await res.text();
		const data = text && contentType.includes("application/json") ? JSON.parse(text) : text;

		if (!res.ok) {
			const message = typeof data === "object" && data !== null && "error" in data
				? JSON.stringify((data as { error: unknown }).error)
				: text || res.statusText;
			throw new Error(`foxctl ${method} ${path}: ${res.status} ${message}`);
		}

		return data as T;
	}

	async get<T>(path: string, signal?: AbortSignal): Promise<T> {
		return this.request<T>("GET", path, undefined, signal);
	}

	async post<T>(path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
		return this.request<T>("POST", path, body, signal);
	}

	async put<T>(path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
		return this.request<T>("PUT", path, body, signal);
	}

	async delete<T>(path: string, signal?: AbortSignal): Promise<T> {
		return this.request<T>("DELETE", path, undefined, signal);
	}
}

// ============================================================================
// Helpers
// ============================================================================

let activePi: ExtensionAPI | undefined;
let memoryDraftsTimer: ReturnType<typeof setInterval> | undefined;
let memoryDraftsInFlight = false;
let memoryDraftsLastRunAt = 0;

function getPi(): ExtensionAPI {
	if (!activePi) throw new Error("foxctl extension API is not initialized");
	return activePi;
}

function getClient(pi: ExtensionAPI): FoxctlClient {
	const url = pi.getFlag("foxctl-url") as string | undefined;
	return new FoxctlClient(url || "http://localhost:8090");
}

function getGatewayURL(pi: ExtensionAPI, override?: string): string {
	const value = override || pi.getFlag("foxctl-gateway-url");
	return typeof value === "string" && value.trim() ? value.trim() : "http://localhost:8765";
}

function getGatewayClient(pi: ExtensionAPI, override?: string): FoxctlClient {
	return new FoxctlClient(getGatewayURL(pi, override));
}

function truncate(str: string, max: number): string {
	return str.length > max ? str.slice(0, max - 3) + "..." : str;
}

function path(...segments: string[]): string {
	return segments.map((segment) => encodeURIComponent(segment)).join("/");
}

function query(params: Record<string, string | number | boolean | undefined>): string {
	const search = new URLSearchParams();
	for (const [key, value] of Object.entries(params)) {
		if (value !== undefined && value !== "") search.set(key, String(value));
	}
	const encoded = search.toString();
	return encoded ? `?${encoded}` : "";
}

function clampTerminalDimension(value: number | undefined, fallback: number): number {
	if (value === undefined || !Number.isFinite(value)) return fallback;
	return Math.max(1, Math.min(1000, Math.floor(value)));
}

function buildRoomTerminalLinks(gatewayURL: string, room: string, cols = 120, rows = 36): Record<string, unknown> {
	const terminalURL = new URL(gatewayURL);
	terminalURL.pathname = `/terminal/${path(room)}`;
	terminalURL.search = "";

	const websocketURL = new URL(gatewayURL);
	websocketURL.protocol = websocketURL.protocol === "https:" ? "wss:" : "ws:";
	websocketURL.pathname = `/ws/terminal/${path(room)}`;
	websocketURL.search = new URLSearchParams({
		cols: String(clampTerminalDimension(cols, 120)),
		rows: String(clampTerminalDimension(rows, 36)),
	}).toString();

	return {
		room_id: room,
		gateway_url: gatewayURL.replace(/\/$/, ""),
		terminal_url: terminalURL.toString(),
		websocket_url: websocketURL.toString(),
		protocol: {
			binary_frames: "raw terminal input/output bytes",
			text_frames: "JSON control messages only, currently resize and server errors",
			scope: "compatibility room terminal endpoint for local/tailnet dogfood only",
		},
	};
}

function formatRoomTerminalLinks(details: Record<string, unknown>): string {
	return [
		`Room terminal dogfood: ${String(details.room_id || "")}`,
		`Gateway: ${String(details.gateway_url || "")}`,
		`Browser: ${String(details.terminal_url || "")}`,
		`WebSocket: ${String(details.websocket_url || "")}`,
		"Protocol: binary frames carry terminal bytes; text frames are JSON controls only.",
		"Scope: compatibility endpoint only, not a durable workbench attachment.",
	].join("\n");
}

function isFlagEnabled(pi: ExtensionAPI, name: string): boolean {
	const value = pi.getFlag(name);
	return value === true || value === "true" || value === "1" || value === "yes";
}

function getStringFlag(pi: ExtensionAPI, name: string, fallback = ""): string {
	const value = pi.getFlag(name);
	return typeof value === "string" && value.trim() ? value.trim() : fallback;
}

function getNumberFlag(pi: ExtensionAPI, name: string, fallback: number, min: number): number {
	const value = pi.getFlag(name);
	const parsed = typeof value === "number" ? value : Number.parseInt(String(value || ""), 10);
	if (!Number.isFinite(parsed)) return fallback;
	return Math.max(min, Math.floor(parsed));
}

function getWorkspace(pi: ExtensionAPI): string {
	const value = pi.getFlag("foxctl-workspace");
	return typeof value === "string" && value.trim() ? value.trim() : ".";
}

function getRoom(pi: ExtensionAPI): string {
	const value = pi.getFlag("foxctl-room");
	return typeof value === "string" ? value.trim() : "";
}

function getEpic(pi: ExtensionAPI): string {
	const value = pi.getFlag("foxctl-epic");
	return typeof value === "string" ? value.trim() : "";
}

function getMilestone(pi: ExtensionAPI): string {
	const value = pi.getFlag("foxctl-milestone");
	return typeof value === "string" ? value.trim() : "";
}

function getStory(pi: ExtensionAPI): string {
	const value = pi.getFlag("foxctl-story");
	return typeof value === "string" ? value.trim() : "";
}

function getActor(pi: ExtensionAPI): string {
	const value = pi.getFlag("foxctl-actor");
	return typeof value === "string" && value.trim() ? value.trim() : "actor:pi:local";
}

function getPiSession(pi: ExtensionAPI): string {
	const value = pi.getFlag("foxctl-session");
	if (typeof value === "string" && value.trim()) return value.trim();
	return `pi:${getActor(pi)}`;
}

function getTransportEndpoint(pi: ExtensionAPI): string {
	const value = pi.getFlag("foxctl-transport-endpoint");
	return typeof value === "string" ? value.trim() : "";
}

function memoryDraftsIntervalMs(pi: ExtensionAPI): number {
	return getNumberFlag(pi, "foxctl-memory-drafts-interval-seconds", 900, 10) * 1000;
}

async function maybeRunAutomaticMemoryDrafts(pi: ExtensionAPI, signal?: AbortSignal, reason = "hook", force = false): Promise<void> {
	if (!isFlagEnabled(pi, "foxctl-memory-drafts-auto")) return;
	if (memoryDraftsInFlight) return;
	const now = Date.now();
	const interval = memoryDraftsIntervalMs(pi);
	if (!force && memoryDraftsLastRunAt > 0 && now - memoryDraftsLastRunAt < interval) return;

	memoryDraftsInFlight = true;
	try {
		const applyDrafts = isFlagEnabled(pi, "foxctl-memory-drafts-apply");
		const dryRun = !applyDrafts || isFlagEnabled(pi, "foxctl-memory-drafts-dry-run");
		const vaultPath = getStringFlag(pi, "foxctl-vault-path");
		const body: Record<string, unknown> = {
			workspace: getWorkspace(pi),
			apply_drafts: applyDrafts,
			dry_run: dryRun,
			lookback: getStringFlag(pi, "foxctl-memory-drafts-lookback", "24h"),
			limit: getNumberFlag(pi, "foxctl-memory-drafts-limit", 20, 1),
			source: `pi:${reason}`,
		};
		if (vaultPath) body.vault_path = vaultPath;
		if (isFlagEnabled(pi, "foxctl-memory-drafts-blur-agent")) {
			body.blur_with_agent = true;
			body.blur_agent = getStringFlag(pi, "foxctl-memory-drafts-blur-backend", "pi");
			const agentBin = getStringFlag(pi, "foxctl-memory-drafts-blur-agent-bin");
			const agentProvider = getStringFlag(pi, "foxctl-memory-drafts-blur-agent-provider");
			const agentModel = getStringFlag(pi, "foxctl-memory-drafts-blur-agent-model");
			const foxctlAgentID = getStringFlag(pi, "foxctl-memory-drafts-blur-foxctl-agent-id");
			const piMode = getStringFlag(pi, "foxctl-memory-drafts-blur-pi-mode", "sdk");
			const piSDKBin = getStringFlag(pi, "foxctl-memory-drafts-blur-pi-sdk-bin", "bun");
			const piSDKScript = getStringFlag(pi, "foxctl-memory-drafts-blur-pi-sdk-script");
			const piAgentDir = getStringFlag(pi, "foxctl-memory-drafts-blur-pi-agent-dir");
			const piThinking = getStringFlag(pi, "foxctl-memory-drafts-blur-pi-thinking", "off");
			if (agentBin) body.blur_agent_bin = agentBin;
			if (agentProvider) body.blur_agent_provider = agentProvider;
			if (agentModel) body.blur_agent_model = agentModel;
			if (foxctlAgentID) body.foxctl_agent_id = foxctlAgentID;
			if (piMode) body.pi_mode = piMode;
			if (piSDKBin) body.pi_sdk_bin = piSDKBin;
			if (piSDKScript) body.pi_sdk_script = piSDKScript;
			if (piAgentDir) body.pi_agent_dir = piAgentDir;
			if (piThinking) body.pi_thinking = piThinking;
		}
		const result = await getClient(pi).post<{ data?: { report?: Record<string, unknown> } }>(
			"/api/context/memory-drafts",
			body,
			signal,
		);
		const report = result.data?.report || {};
		console.info(
			`foxctl memory drafts auto-run (${reason}): scanned=${String(report.feedback_scanned || 0)} planned=${String(report.drafts_planned || 0)} written=${String(report.drafts_written || 0)}`,
		);
		memoryDraftsLastRunAt = Date.now();
	} catch (error) {
		console.warn(`foxctl memory drafts auto-run failed (${reason}): ${error instanceof Error ? error.message : String(error)}`);
	} finally {
		memoryDraftsInFlight = false;
	}
}

function startAutomaticMemoryDrafts(pi: ExtensionAPI, signal?: AbortSignal): void {
	if (!isFlagEnabled(pi, "foxctl-memory-drafts-auto")) return;
	void maybeRunAutomaticMemoryDrafts(pi, signal, "session_start", true);
	if (memoryDraftsTimer !== undefined) return;
	memoryDraftsTimer = setInterval(() => {
		void maybeRunAutomaticMemoryDrafts(pi, undefined, "interval", true);
	}, memoryDraftsIntervalMs(pi));
	(memoryDraftsTimer as unknown as { unref?: () => void }).unref?.();
}

function commandToArgv(command: string): string[] {
	const trimmed = command.trim();
	if (!trimmed) throw new Error("cmd must not be empty");
	if (trimmed.startsWith("[")) {
		const parsed = JSON.parse(trimmed);
		if (!Array.isArray(parsed) || !parsed.every((item) => typeof item === "string")) {
			throw new Error("cmd JSON must be an array of strings");
		}
		return parsed;
	}
	return ["sh", "-lc", trimmed];
}

function formatFoxctlContext(parts: Record<string, unknown>): string {
	const sections: string[] = ["Foxctl workspace context:"];

	// Epic context summary header
	const epicCtx = parts.epic_context as Record<string, unknown> | undefined;
	const epicHealth = parts.epic_health as Record<string, unknown> | undefined;
	const epicNext = parts.epic_next as Record<string, unknown> | undefined;
	if (epicCtx) {
		const epic = epicCtx.epic as Record<string, unknown> | undefined;
		const status = epicCtx.status as Record<string, unknown> | undefined;
		if (epic) {
			sections.push(`\nEpic: ${epic.title || epic.id} (${epic.status || "unknown"})`);
		}
		if (status) {
			sections.push(`  Milestones: ${status.milestone_count}, Stories: ${status.story_count}, Open: ${status.open_story_count}`);
		}
		const health = epicHealth?.health as Record<string, unknown> | undefined;
		if (health) {
			sections.push(`  Health: ${health.status}${health.warnings && (health.warnings as string[]).length > 0 ? ` — ${(health.warnings as string[]).join(", ")}` : ""}`);
		}
		const next = epicNext?.next as Array<Record<string, unknown>> | undefined;
		if (next && next.length > 0) {
			sections.push(`  Next: ${next[0].action}${next[0].title ? ` — ${next[0].title}` : ""}${next[0].reason ? ` (${next[0].reason})` : ""}`);
		}
		const milestone = parts.milestone as Record<string, unknown> | undefined;
		if (milestone?.milestone) {
			const ms = milestone.milestone as Record<string, unknown>;
			sections.push(`  Milestone: ${ms.title || ms.id} (${ms.status || "open"})`);
		}
		const story = parts.story as Record<string, unknown> | undefined;
		if (story?.story) {
			const st = story.story as Record<string, unknown>;
			sections.push(`  Story: ${st.title || st.id} (${st.status || "accepted"})`);
		}
	}

	sections.push(JSON.stringify(parts, null, 2));
	return sections.join("\n");
}

function skillEndpoint(skill: string): string {
	return `/api/skills/${skill.split("/").map((segment) => encodeURIComponent(segment)).join("/")}`;
}

function recordFromJSON(value: string | undefined): Record<string, unknown> {
	if (!value || !value.trim()) return {};
	const parsed = JSON.parse(value);
	if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
		throw new Error("input must be a JSON object");
	}
	return parsed as Record<string, unknown>;
}

function withWorkspace(pi: ExtensionAPI, input: Record<string, unknown>): Record<string, unknown> {
	const out: Record<string, unknown> = { ...input };
	const explicitWorkspace = typeof out.workspace === "string" && out.workspace.trim() ? out.workspace.trim() : "";
	const workspace = explicitWorkspace || getWorkspace(pi);
	if (workspace) {
		if (out.workspace === undefined || out.workspace === "") out.workspace = workspace;
	}
	return out;
}

async function runFoxctlSkill(pi: ExtensionAPI, skill: string, input: Record<string, unknown>, signal?: AbortSignal): Promise<Record<string, unknown>> {
	return getClient(pi).post<Record<string, unknown>>(skillEndpoint(skill), withWorkspace(pi, input), signal);
}

async function runRoomAgile(pi: ExtensionAPI, params: Record<string, unknown>, signal?: AbortSignal): Promise<Record<string, unknown>> {
	const room = typeof params.room_id === "string" && params.room_id.trim() ? params.room_id.trim() : getRoom(pi);
	if (!room) throw new Error("room_id is required or set --foxctl-room");
	const input = withWorkspace(pi, {
		...params,
		room_id: undefined,
		sender: typeof params.sender === "string" && params.sender.trim() ? params.sender : getActor(pi),
		actor: typeof params.actor === "string" && params.actor.trim() ? params.actor : getActor(pi),
	});
	return getClient(pi).post<Record<string, unknown>>(`/api/rooms/${path(room)}/agile`, input, signal);
}

function jsonToolResult(details: Record<string, unknown>): { content: Array<{ type: "text"; text: string }>; details: Record<string, unknown> } {
	return {
		content: [{ type: "text", text: JSON.stringify(details, null, 2) }],
		details,
	};
}

type SkillFacadeOptions = {
	name: string;
	label: string;
	description: string;
	skill: string;
	parameters: any;
	input?: (params: any, pi: ExtensionAPI) => Record<string, unknown>;
};


function defineFoxctlSkillFacade(options: SkillFacadeOptions) {
	return defineTool({
		name: options.name,
		label: options.label,
		description: options.description,
		parameters: options.parameters,
		async execute(_toolCallId, params, signal, _onUpdate, ctx) {
			const pi = getPi();
			const input = options.input ? options.input(params, pi) : { ...(params as Record<string, unknown>) };
			const result = await runFoxctlSkill(pi, options.skill, input, signal);
			return jsonToolResult(result);
		},
	});
}

async function bindPiToRoom(pi: ExtensionAPI, room: string, signal?: AbortSignal): Promise<Record<string, unknown>> {
	const client = getClient(pi);
	const workspace = getWorkspace(pi);
	const actor = getActor(pi);
	const session = getPiSession(pi);
	const transportEndpoint = getTransportEndpoint(pi);
	const member = {
		actor_id: actor,
		role: "participant",
		delivery_binding: {
			mux_backend: "pi",
			mux_session: session,
			transport_kind: "pi-extension",
			transport_endpoint: transportEndpoint,
			health: "unknown",
		},
	};

	try {
		await client.post<Record<string, unknown>>("/api/rooms", {
			workspace_id: workspace,
			id: room,
			title: room,
			members: [member],
		}, signal);
	} catch (error) {
		const message = error instanceof Error ? error.message : String(error);
		if (!message.includes("409") && !message.includes("already")) throw error;
	}

	const { role: _role, ...binding } = member;
	return client.put<Record<string, unknown>>(
		`/api/rooms/${path(room)}/members/${path(actor)}/binding${query({ workspace_id: workspace })}`,
		{ ...binding, actor_id: actor },
		signal,
	);
}

async function runOptionalFoxctlSkill(pi: ExtensionAPI, skill: string, input: Record<string, unknown>, signal?: AbortSignal): Promise<Record<string, unknown>> {
	try {
		return await runFoxctlSkill(pi, skill, input, signal);
	} catch (error) {
		return {
			ok: false,
			error: error instanceof Error ? error.message : String(error),
		};
	}
}

async function listControlProposals(
	pi: ExtensionAPI,
	signal?: AbortSignal,
	options?: { workspace?: string; limit?: number; status?: string },
): Promise<Record<string, unknown>> {
	const workspace = options?.workspace?.trim() || getWorkspace(pi);
	const limit = options?.limit ?? 12;
	const status = options?.status?.trim();
	const client = getClient(pi);
	const inbox = await client.get<Record<string, unknown>>(
		`/api/context/control-proposals${query({ workspace, limit, status })}`,
		signal,
	);
	return {
		source: {
			inbox: "web:/api/context/control-proposals",
			proposal: "web:/api/context/control-proposals/{id}",
			decision: "web:/api/context/control-proposals/{id}/decisions",
		},
		workspace,
		filters: {
			limit,
			status: status || "all",
		},
		inbox,
	};
}

async function listActiveJobs(pi: ExtensionAPI, signal?: AbortSignal, limit = 12): Promise<Record<string, unknown>> {
	const client = getClient(pi);
	const jobs = await client.get<Record<string, unknown>>(
		`/api/jobs${query({ state: "running", limit })}`,
		signal,
	);
	return {
		source: "web:/api/jobs?state=running",
		jobs,
	};
}

async function getFoxctlSnapshot(pi: ExtensionAPI, signal?: AbortSignal, prompt = ""): Promise<Record<string, unknown>> {
	const client = getClient(pi);
	const workspace = getWorkspace(pi);
	const room = getRoom(pi);
	const actor = getActor(pi);
	const promptQuery = prompt.trim();
	const snapshot: Record<string, unknown> = {
		workspace,
		actor,
	};

	snapshot.health = await client.get<Record<string, unknown>>("/api/health", signal);
	snapshot.context = await client.get<Record<string, unknown>>(
		`/api/context/overview${query({ workspace, limit: 4 })}`,
		signal,
	);
	snapshot.tasks = await client.get<Record<string, unknown>>(
		`/api/tasks${query({ workspace, limit: 12 })}`,
		signal,
	);
	snapshot.rooms = await client.get<Record<string, unknown>>(
		`/api/rooms${query({ workspace_id: workspace, actor_id: actor, limit: 12 })}`,
		signal,
	);
	snapshot.control_proposals = await listControlProposals(pi, signal, { workspace, limit: 6 });
	snapshot.active_jobs = await listActiveJobs(pi, signal, 6);
	if (room) {
		snapshot.room_inbox = await client.get<Record<string, unknown>>(
			`/api/rooms/${path(room)}/inbox${query({ workspace_id: workspace, actor_id: actor, limit: 8 })}`,
			signal,
		);
	}
	if (promptQuery && isFlagEnabled(pi, "foxctl-memory-context")) {
		snapshot.memory_context = await runOptionalFoxctlSkill(pi, "memory/query", {
			query: promptQuery,
			limit: 5,
			include_content: false,
		}, signal);
		snapshot.session_recall = await runOptionalFoxctlSkill(pi, "session/recall", {
			query: promptQuery,
			limit: 3,
		}, signal);
	}

	const epicID = getEpicOverride(pi);
	if (room && epicID && isFlagEnabled(pi, "foxctl-epic-context")) {
		try {
			snapshot.epic_context = await runRoomAgile(pi, {
				action: "epic_resume",
				epic_id: epicID,
				limit: 100,
			}, signal);
			snapshot.epic_health = await runRoomAgile(pi, {
				action: "epic_health",
				epic_id: epicID,
				limit: 100,
			}, signal);
			snapshot.epic_next = await runRoomAgile(pi, {
				action: "epic_next",
				epic_id: epicID,
				limit: 100,
			}, signal);
			const milestoneID = getMilestoneOverride(pi);
			if (milestoneID) {
				snapshot.milestone = await runRoomAgile(pi, {
					action: "milestone_show",
					milestone_id: milestoneID,
					limit: 100,
				}, signal);
			}
			const storyID = getStoryOverride(pi);
			if (storyID) {
				snapshot.story = await runRoomAgile(pi, {
					action: "story_show",
					story_id: storyID,
					limit: 100,
				}, signal);
			}
		} catch (e) {
			snapshot.epic_context_error = e instanceof Error ? e.message : String(e);
		}
	}

	return snapshot;
}

async function gatherFoxctlContext(pi: ExtensionAPI, signal?: AbortSignal): Promise<Record<string, unknown>> {
	const client = getClient(pi);
	const workspace = getWorkspace(pi);
	const room = getRoom(pi);
	const actor = getActor(pi);
	const result: Record<string, unknown> = {
		workspace,
		actor,
		room: room || undefined,
	};

	result.health = await client.get<Record<string, unknown>>("/api/health", signal);
	result.context = await client.get<Record<string, unknown>>(
		`/api/context/overview${query({ workspace, limit: 8 })}`,
		signal,
	);
	result.rooms = await client.get<Record<string, unknown>>(
		`/api/rooms${query({ workspace_id: workspace, actor_id: actor, limit: 12 })}`,
		signal,
	);
	result.tasks = await client.get<Record<string, unknown>>(
		`/api/tasks${query({ workspace, limit: 20 })}`,
		signal,
	);
	result.control_proposals = await listControlProposals(pi, signal, { workspace, limit: 20 });
	result.active_jobs = await listActiveJobs(pi, signal, 20);
	if (room) {
		result.room_status = await client.get<Record<string, unknown>>(
			`/api/rooms/${path(room)}/status${query({ workspace_id: workspace, actor_id: actor })}`,
			signal,
		);
		result.room_inbox = await client.get<Record<string, unknown>>(
			`/api/rooms/${path(room)}/inbox${query({ workspace_id: workspace, actor_id: actor, only: "pending", limit: 20 })}`,
			signal,
		);
	}

	return result;
}

// ============================================================================
// TUI Components (M5: Rich TUI Panel)
// ============================================================================

/** Runtime flag overrides for selector commands. These take precedence over pi.getFlag(). */
const _flagOverrides: Record<string, string> = {};

/** Get a flag value, checking runtime overrides first. */
function getOverriddenFlag(pi: ExtensionAPI, name: string): string {
	if (name in _flagOverrides) return _flagOverrides[name];
	const value = pi.getFlag(name);
	return typeof value === "string" ? value.trim() : "";
}

function getEpicOverride(pi: ExtensionAPI): string {
	return getOverriddenFlag(pi, "foxctl-epic");
}

function getMilestoneOverride(pi: ExtensionAPI): string {
	return getOverriddenFlag(pi, "foxctl-milestone");
}

function getStoryOverride(pi: ExtensionAPI): string {
	return getOverriddenFlag(pi, "foxctl-story");
}

interface EpicWidgetData {
	epic?: { id: string; title?: string; status?: string };
	milestone?: { id: string; title?: string; status?: string };
	story?: { id: string; title?: string; status?: string };
	health?: { status?: string; warnings?: string[] };
	next?: Array<{ action?: string; title?: string; reason?: string }>;
}

class EpicStatusWidget implements Component {
	private cachedWidth?: number;
	private cachedLines?: string[];

	constructor(
		private data: EpicWidgetData,
		private theme: { fg: (color: string, text: string) => string },
	) {}

	update(data: EpicWidgetData): void {
		this.data = data;
		this.cachedWidth = undefined;
		this.cachedLines = undefined;
	}

	invalidate(): void {
		this.cachedWidth = undefined;
		this.cachedLines = undefined;
	}

	render(width: number): string[] {
		if (this.cachedLines && this.cachedWidth === width) return this.cachedLines;
		const lines: string[] = [];
		const t = this.theme;
		const { epic, milestone, story, health, next } = this.data;

		if (!epic) {
			lines.push(t.fg("dim", " No active epic — use /epic-select or set --foxctl-epic"));
			this.padLines(lines, width);
			this.cachedWidth = width;
			this.cachedLines = lines;
			return lines;
		}

		// Line 1: Epic header
		const epicTitle = truncateToWidth((epic.title || epic.id), width - 20, "");
		const epicStatus = epic.status || "?";
		lines.push(` ${t.fg("accent", "Epic:")} ${epicTitle}  ${t.fg("dim", `[${epicStatus}]`)}`);

		// Line 2: Milestone
		if (milestone) {
			const msTitle = truncateToWidth((milestone.title || milestone.id), width - 28, "");
			lines.push(` ${t.fg("muted", "MS:")} ${msTitle}  ${t.fg("dim", `[${milestone.status || "open"}]`)}`);
		}

		// Line 3: Current story
		if (story) {
			const stTitle = truncateToWidth((story.title || story.id), width - 22, "");
			lines.push(` ${t.fg("muted", "Story:")} ${stTitle}  ${t.fg("dim", `[${story.status || "accepted"}]`)}`);
		}

		// Line 4: Health warnings
		if (health?.warnings && health.warnings.length > 0) {
			const warnText = health.warnings.join(", ");
			lines.push(` ${t.fg("warning", "⚠")} ${t.fg("warning", truncateToWidth(warnText, width - 4))}`);
		}

		// Line 5: Next action
		if (next && next.length > 0) {
			const n = next[0]!;
			const nextText = `${n.action || "next"}${n.title ? ` — ${n.title}` : ""}${n.reason ? ` (${n.reason})` : ""}`;
			lines.push(` ${t.fg("success", "→")} ${t.fg("dim", truncateToWidth(nextText, width - 4))}`);
		}

		this.padLines(lines, width);
		this.cachedWidth = width;
		this.cachedLines = lines;
		return lines;
	}

	private padLines(lines: string[], width: number): void {
		for (let i = 0; i < lines.length; i++) {
			const vis = visibleWidth(lines[i]!);
			if (vis < width) lines[i] += " ".repeat(width - vis);
			if (vis > width) lines[i] = truncateToWidth(lines[i]!, width, "");
		}
	}
}

async function fetchEpicWidgetData(pi: ExtensionAPI, signal?: AbortSignal): Promise<EpicWidgetData> {
	const room = getRoom(pi);
	const epicID = getEpicOverride(pi);
	if (!room || !epicID) return {};

	const data: EpicWidgetData = {};

	try {
		const epicRes = await runRoomAgile(pi, { action: "epic_resume", epic_id: epicID, limit: 200 }, signal);
		const epicData = (epicRes.result as Record<string, unknown>)?.data as Record<string, unknown> | undefined;
		const epic = epicData?.epic as Record<string, unknown> | undefined;
		if (epic) data.epic = { id: String(epic.id || epicID), title: String(epic.title || ""), status: String(epic.status || "") };

		// Resolve active milestone
		const milestoneID = getMilestoneOverride(pi);
		if (milestoneID && epicData?.milestones) {
			const milestones = epicData.milestones as Array<Record<string, unknown>>;
			const ms = milestones.find((m) => m.id === milestoneID);
			if (ms) data.milestone = { id: String(ms.id), title: String(ms.title || ""), status: String(ms.status || "open") };
		}

		// Resolve active story
		const storyID = getStoryOverride(pi);
		if (storyID && epicData?.stories) {
			const stories = epicData.stories as Array<Record<string, unknown>>;
			const st = stories.find((s) => s.id === storyID);
			if (st) data.story = { id: String(st.id), title: String(st.title || ""), status: String(st.status || "accepted") };
		}
	} catch { /* epic_resume failed, partial data */ }

	try {
		const healthRes = await runRoomAgile(pi, { action: "epic_health", epic_id: epicID, limit: 200 }, signal);
		const h = (healthRes.result as Record<string, unknown>)?.data as Record<string, unknown> | undefined;
		const health = h?.health as Record<string, unknown> | undefined;
		if (health) {
			data.health = {
				status: String(health.status || ""),
				warnings: Array.isArray(health.warnings) ? health.warnings.map(String) : [],
			};
		}
	} catch { /* health unavailable */ }

	try {
		const nextRes = await runRoomAgile(pi, { action: "epic_next", epic_id: epicID, limit: 200 }, signal);
		const n = (nextRes.result as Record<string, unknown>)?.data as Record<string, unknown> | undefined;
		if (Array.isArray(n?.next)) {
			data.next = (n!.next as Array<Record<string, unknown>>).map((item) => ({
				action: String(item.action || ""),
				title: String(item.title || ""),
				reason: String(item.reason || ""),
			}));
		}
	} catch { /* next unavailable */ }

	return data;
}

async function refreshEpicWidget(pi: ExtensionAPI, ctx: { signal?: AbortSignal }): Promise<void> {
	const widget = (pi as any).__foxctlEpicWidget as EpicStatusWidget | undefined;
	if (!widget) return;
	try {
		const data = await fetchEpicWidgetData(pi, ctx.signal);
		widget.update(data);
	} catch { /* non-fatal */ }
}

function buildSelectListTheme(theme: { fg: (color: string, text: string) => string }) {
	return {
		selectedPrefix: (s: string) => theme.fg("accent", s),
		selectedText: (s: string) => theme.fg("accent", s),
		description: (s: string) => theme.fg("muted", s),
		scrollInfo: (s: string) => theme.fg("dim", s),
		noMatch: (s: string) => theme.fg("dim", s),
	};
}

// ============================================================================
// Tools
// ============================================================================

// --- Status / Health ---

const FoxctlStatusParams = Type.Object({});

const foxctlStatusTool = defineTool({
	name: "foxctl_status",
	label: "Foxctl Status",
	description: "Get foxctl daemon status, version, and health information.",
	parameters: FoxctlStatusParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const status = await client.get<Record<string, unknown>>("/api/health", signal);
		return {
			content: [{ type: "text", text: JSON.stringify(status, null, 2) }],
			details: status,
		};
	},
});

const FoxctlHealthParams = Type.Object({});

const foxctlHealthTool = defineTool({
	name: "foxctl_health",
	label: "Foxctl Health",
	description: "Check foxctl daemon health and readiness.",
	parameters: FoxctlHealthParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const health = await client.get<Record<string, unknown>>("/api/health", signal);
		return {
			content: [{ type: "text", text: JSON.stringify(health, null, 2) }],
			details: health,
		};
	},
});

// --- Skills ---

const FoxctlSkillsListParams = Type.Object({});

const foxctlSkillsListTool = defineTool({
	name: "foxctl_skills_list",
	label: "Foxctl Skills List",
	description: "List all available skills in the foxctl daemon.",
	parameters: FoxctlSkillsListParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const skills = await client.get<{ skills?: Array<{ name: string; description?: string }> }>("/api/skills", signal);
		const list = skills.skills || [];
		const text = list.length
			? list.map((s) => `- ${s.name}${s.description ? `: ${s.description}` : ""}`).join("\n")
			: "No skills found";
		return {
			content: [{ type: "text", text }],
			details: skills,
		};
	},
});

const FoxctlSkillRunParams = Type.Object({
	skill: Type.String({ description: "Name of the skill to run" }),
	input: Type.Optional(Type.String({ description: "JSON input for the skill" })),
});

const foxctlSkillRunTool = defineTool({
	name: "foxctl_skill_run",
	label: "Foxctl Skill Run",
	description: "Run a foxctl skill by name with optional JSON input.",
	parameters: FoxctlSkillRunParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const body = params.input ? JSON.parse(params.input) : {};
		const result = await client.post<Record<string, unknown>>("/api/skills/run", { skill: params.skill, input: body }, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlSkillDetailParams = Type.Object({
	skill: Type.String({ description: "Name of the skill" }),
});

const foxctlSkillDetailTool = defineTool({
	name: "foxctl_skill_detail",
	label: "Foxctl Skill Detail",
	description: "Get detailed information about a specific foxctl skill.",
	parameters: FoxctlSkillDetailParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const detail = await client.get<Record<string, unknown>>(`/api/skills/manifest/${path(params.skill)}`, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(detail, null, 2) }],
			details: detail,
		};
	},
});

// --- Agents ---

const FoxctlAgentsListParams = Type.Object({});

const foxctlAgentsListTool = defineTool({
	name: "foxctl_agents_list",
	label: "Foxctl Agents List",
	description: "List all registered foxctl agents.",
	parameters: FoxctlAgentsListParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const agents = await client.get<{ agents?: Array<{ id: string; name?: string; status?: string }> }>(
			"/api/agents",
			signal,
		);
		const list = agents.agents || [];
		const text = list.length
			? list.map((a) => `- ${a.id}${a.name ? ` (${a.name})` : ""}${a.status ? ` [${a.status}]` : ""}`).join("\n")
			: "No agents found";
		return {
			content: [{ type: "text", text }],
			details: agents,
		};
	},
});

const FoxctlAgentDetailParams = Type.Object({
	agent_id: Type.String({ description: "Agent ID" }),
});

const foxctlAgentDetailTool = defineTool({
	name: "foxctl_agent_detail",
	label: "Foxctl Agent Detail",
	description: "Get detailed information about a specific foxctl agent.",
	parameters: FoxctlAgentDetailParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const detail = await client.get<Record<string, unknown>>(`/api/agents/${path(params.agent_id)}`, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(detail, null, 2) }],
			details: detail,
		};
	},
});

const FoxctlAgentSpawnParams = Type.Object({
	agent_id: Type.String({ description: "Agent ID to spawn" }),
	input: Type.Optional(Type.String({ description: "JSON input for the agent" })),
});

const foxctlAgentSpawnTool = defineTool({
	name: "foxctl_agent_spawn",
	label: "Foxctl Agent Spawn",
	description: "Spawn a foxctl agent with optional input.",
	parameters: FoxctlAgentSpawnParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const body = params.input ? JSON.parse(params.input) : {};
		const result = await client.post<Record<string, unknown>>("/api/agents/spawn", { agent_id: params.agent_id, ...body }, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlAgentAskParams = Type.Object({
	agent_id: Type.String({ description: "Agent ID" }),
	message: Type.String({ description: "Message to send to the agent" }),
});

const foxctlAgentAskTool = defineTool({
	name: "foxctl_agent_ask",
	label: "Foxctl Agent Ask",
	description: "Send a message to a foxctl agent and get a response.",
	parameters: FoxctlAgentAskParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.post<Record<string, unknown>>(`/api/agents/${path(params.agent_id)}/ask`, { message: params.message }, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- Rooms ---

const FoxctlRoomsListParams = Type.Object({});

const foxctlRoomsListTool = defineTool({
	name: "foxctl_rooms_list",
	label: "Foxctl Rooms List",
	description: "List all foxctl rooms.",
	parameters: FoxctlRoomsListParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const client = getClient(pi);
		const rooms = await client.get<{ rooms?: Array<{ id: string; name?: string }> }>(
			`/api/rooms${query({ workspace_id: getWorkspace(pi), actor_id: getActor(pi), limit: 50 })}`,
			signal,
		);
		const list = rooms.rooms || [];
		const text = list.length ? list.map((r) => `- ${r.id}${r.name ? `: ${r.name}` : ""}`).join("\n") : "No rooms found";
		return {
			content: [{ type: "text", text }],
			details: rooms,
		};
	},
});

const FoxctlRoomDetailParams = Type.Object({
	room_id: Type.String({ description: "Room ID" }),
});

const foxctlRoomDetailTool = defineTool({
	name: "foxctl_room_detail",
	label: "Foxctl Room Detail",
	description: "Get details about a specific foxctl room.",
	parameters: FoxctlRoomDetailParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const client = getClient(pi);
		const detail = await client.get<Record<string, unknown>>(
			`/api/rooms/${path(params.room_id)}${query({ workspace_id: getWorkspace(pi), actor_id: getActor(pi) })}`,
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(detail, null, 2) }],
			details: detail,
		};
	},
});

const FoxctlRoomMessagesParams = Type.Object({
	room_id: Type.String({ description: "Room ID" }),
	limit: Type.Optional(Type.Number({ description: "Max messages to return", default: 50 })),
});

const foxctlRoomMessagesTool = defineTool({
	name: "foxctl_room_messages",
	label: "Foxctl Room Messages",
	description: "Get messages from a foxctl room.",
	parameters: FoxctlRoomMessagesParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const client = getClient(pi);
		const limit = params.limit ?? 50;
		const msgs = await client.get<Record<string, unknown>>(
			`/api/rooms/${path(params.room_id)}/messages${query({ workspace_id: getWorkspace(pi), limit })}`,
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(msgs, null, 2) }],
			details: msgs,
		};
	},
});

const FoxctlRoomSendParams = Type.Object({
	room_id: Type.String({ description: "Room ID" }),
	message: Type.String({ description: "Message text" }),
});

const foxctlRoomSendTool = defineTool({
	name: "foxctl_room_send",
	label: "Foxctl Room Send",
	description: "Send a message to a foxctl room.",
	parameters: FoxctlRoomSendParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.post<Record<string, unknown>>(
			`/api/rooms/${path(params.room_id)}/messages`,
			{ workspace_id: getWorkspace(getPi()), sender: getActor(getPi()), body: params.message },
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlRoomInboxParams = Type.Object({
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
	actor_id: Type.Optional(Type.String({ description: "Actor ID; defaults to --foxctl-actor" })),
	only: Type.Optional(StringEnum(["pending", "unread", "acked", "resolved", "all"] as const, { default: "pending" })),
	limit: Type.Optional(Type.Number({ description: "Max messages to return", default: 50 })),
});

const foxctlRoomInboxTool = defineTool({
	name: "foxctl_room_inbox",
	label: "Foxctl Room Inbox",
	description: "Read the room inbox for the Pi actor.",
	parameters: FoxctlRoomInboxParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const room = params.room_id || getRoom(pi);
		if (!room) throw new Error("room_id required or set --foxctl-room");
		const actor = params.actor_id || getActor(pi);
		const result = await getClient(pi).get<Record<string, unknown>>(
			`/api/rooms/${path(room)}/inbox${query({
				workspace_id: getWorkspace(pi),
				actor_id: actor,
				only: params.only || "pending",
				limit: params.limit ?? 50,
			})}`,
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlRoomStatusParams = Type.Object({
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
});

const foxctlRoomStatusTool = defineTool({
	name: "foxctl_room_status",
	label: "Foxctl Room Status",
	description: "Get room delivery/member status for the current Pi actor.",
	parameters: FoxctlRoomStatusParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const room = params.room_id || getRoom(pi);
		if (!room) throw new Error("room_id required or set --foxctl-room");
		const result = await getClient(pi).get<Record<string, unknown>>(
			`/api/rooms/${path(room)}/status${query({ workspace_id: getWorkspace(pi), actor_id: getActor(pi) })}`,
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlRoomControlSnapshotParams = Type.Object({
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
});

const foxctlRoomControlSnapshotTool = defineTool({
	name: "foxctl_room_control_snapshot",
	label: "Foxctl Room Control Snapshot",
	description: "Get the room control-plane snapshot for the current Pi actor.",
	parameters: FoxctlRoomControlSnapshotParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const room = params.room_id || getRoom(pi);
		if (!room) throw new Error("room_id required or set --foxctl-room");
		const result = await getClient(pi).get<Record<string, unknown>>(
			`/api/rooms/${path(room)}/control-snapshot${query({ workspace_id: getWorkspace(pi), actor_id: getActor(pi) })}`,
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlRoomBindPiParams = Type.Object({
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
});

const foxctlRoomBindPiTool = defineTool({
	name: "foxctl_room_bind_pi",
	label: "Foxctl Room Bind Pi",
	description: "Create or update this Pi extension as a room participant transport binding.",
	parameters: FoxctlRoomBindPiParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const room = params.room_id || getRoom(pi);
		if (!room) throw new Error("room_id required or set --foxctl-room");
		const result = await bindPiToRoom(pi, room, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlRoomTerminalLinksParams = Type.Object({
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
	gateway_url: Type.Optional(Type.String({ description: "Terminal gateway URL; defaults to --foxctl-gateway-url" })),
	cols: Type.Optional(Type.Number({ description: "Initial terminal columns", default: 120 })),
	rows: Type.Optional(Type.Number({ description: "Initial terminal rows", default: 36 })),
});

const foxctlRoomTerminalLinksTool = defineTool({
	name: "foxctl_room_terminal_links",
	label: "Foxctl Room Terminal Links",
	description: "Return local/tailnet browser and WebSocket links for the current room terminal compatibility endpoint.",
	parameters: FoxctlRoomTerminalLinksParams,
	async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
		const pi = getPi();
		const room = params.room_id || getRoom(pi);
		if (!room) throw new Error("room_id required or set --foxctl-room");
		const details = buildRoomTerminalLinks(
			getGatewayURL(pi, params.gateway_url),
			room,
			params.cols ?? 120,
			params.rows ?? 36,
		);
		return {
			content: [{ type: "text", text: formatRoomTerminalLinks(details) }],
			details,
		};
	},
});

const FoxctlRoomTerminalRegisterParams = Type.Object({
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
	gateway_url: Type.Optional(Type.String({ description: "Terminal gateway URL; defaults to --foxctl-gateway-url" })),
	tmux_session: Type.Optional(Type.String({ description: "Existing tmux session to attach; gateway defaults to room-derived session" })),
	max_connections: Type.Optional(Type.Number({ description: "Optional gateway room connection limit" })),
	cols: Type.Optional(Type.Number({ description: "Initial terminal columns", default: 120 })),
	rows: Type.Optional(Type.Number({ description: "Initial terminal rows", default: 36 })),
});

const foxctlRoomTerminalRegisterTool = defineTool({
	name: "foxctl_room_terminal_register",
	label: "Foxctl Room Terminal Register",
	description: "Register a room with the foxctl terminal gateway and return local/tailnet dogfood terminal links.",
	parameters: FoxctlRoomTerminalRegisterParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const room = params.room_id || getRoom(pi);
		if (!room) throw new Error("room_id required or set --foxctl-room");
		const registration = await getGatewayClient(pi, params.gateway_url).post<Record<string, unknown>>(
			"/api/rooms",
			{
				room_id: room,
				tmux_session: params.tmux_session,
				max_connections: params.max_connections,
			},
			signal,
		);
		const details = {
			...buildRoomTerminalLinks(
				getGatewayURL(pi, params.gateway_url),
				room,
				params.cols ?? 120,
				params.rows ?? 36,
			),
			registration,
		};
		return {
			content: [{ type: "text", text: formatRoomTerminalLinks(details) }],
			details,
		};
	},
});

const FoxctlRoomTasksParams = Type.Object({
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
	status: Type.Optional(StringEnum(["pending", "in_progress", "blocked", "completed", "failed", "canceled", "all"] as const, { default: "all" })),
	limit: Type.Optional(Type.Number({ description: "Max room messages to scan for linked tasks", default: 200 })),
});

const foxctlRoomTasksTool = defineTool({
	name: "foxctl_room_tasks",
	label: "Foxctl Room Tasks",
	description: "List tasks linked to a foxctl room.",
	parameters: FoxctlRoomTasksParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const room = params.room_id || getRoom(pi);
		if (!room) throw new Error("room_id required or set --foxctl-room");
		const result = await getClient(pi).get<Record<string, unknown>>(
			`/api/rooms/${path(room)}/tasks${query({
				workspace_id: getWorkspace(pi),
				status: params.status && params.status !== "all" ? params.status : undefined,
				limit: params.limit ?? 200,
			})}`,
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlRoomTaskCreateParams = Type.Object({
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
	title: Type.String({ description: "Task title" }),
	description: Type.Optional(Type.String({ description: "Task description" })),
	scope_path: Type.Optional(Type.String({ description: "Optional scope path" })),
	parent_id: Type.Optional(Type.String({ description: "Optional parent task ID" })),
	depends_on: Type.Optional(Type.Array(Type.String(), { description: "Task IDs this task depends on" })),
	milestone_id: Type.Optional(Type.String({ description: "Optional room milestone message ID" })),
});

const foxctlRoomTaskCreateTool = defineTool({
	name: "foxctl_room_task_create",
	label: "Foxctl Room Task Create",
	description: "Create a task linked to the current foxctl room.",
	parameters: FoxctlRoomTaskCreateParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const room = params.room_id || getRoom(pi);
		if (!room) throw new Error("room_id required or set --foxctl-room");
		const result = await getClient(pi).post<Record<string, unknown>>(
			`/api/rooms/${path(room)}/tasks`,
			{
				workspace_id: getWorkspace(pi),
				actor_id: getActor(pi),
				title: params.title,
				description: params.description,
				scope_path: params.scope_path,
				parent_id: params.parent_id,
				depends_on: params.depends_on,
				milestone_id: params.milestone_id,
			},
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlRoomTaskActionParams = Type.Object({
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
	task_id: Type.String({ description: "Task ID" }),
	action: StringEnum(["claim", "touch", "block", "unblock", "complete", "abandon", "assign", "reassign", "reclaim"] as const),
	recipient: Type.Optional(Type.String({ description: "Recipient actor for assign/reassign" })),
	reason: Type.Optional(Type.String({ description: "Reason for block/abandon/reclaim/reassign" })),
	notes: Type.Optional(Type.String({ description: "Completion or assignment notes" })),
	gotchas: Type.Optional(Type.String({ description: "Completion gotchas" })),
});

const foxctlRoomTaskActionTool = defineTool({
	name: "foxctl_room_task_action",
	label: "Foxctl Room Task Action",
	description: "Claim, touch, block, unblock, complete, abandon, assign, reassign, or reclaim a room task.",
	parameters: FoxctlRoomTaskActionParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const room = params.room_id || getRoom(pi);
		if (!room) throw new Error("room_id required or set --foxctl-room");
		const result = await getClient(pi).post<Record<string, unknown>>(
			`/api/rooms/${path(room)}/tasks/${path(params.task_id)}/${path(params.action)}`,
			{
				workspace_id: getWorkspace(pi),
				actor_id: getActor(pi),
				recipient: params.recipient,
				reason: params.reason,
				notes: params.notes,
				gotchas: params.gotchas,
			},
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlRoomMessageAckParams = Type.Object({
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
	message_id: Type.String({ description: "Room message ID to acknowledge" }),
});

const foxctlRoomMessageAckTool = defineTool({
	name: "foxctl_room_message_ack",
	label: "Foxctl Room Message Ack",
	description: "Acknowledge a room message for the current Pi actor.",
	parameters: FoxctlRoomMessageAckParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const room = params.room_id || getRoom(pi);
		if (!room) throw new Error("room_id required or set --foxctl-room");
		const result = await getClient(pi).post<Record<string, unknown>>(
			`/api/rooms/${path(room)}/messages/${path(params.message_id)}/ack`,
			{ workspace_id: getWorkspace(pi), actor_id: getActor(pi) },
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlRoomMessagesResolveParams = Type.Object({
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
	message_ids: Type.Optional(Type.Array(Type.String(), { description: "Message IDs to resolve" })),
	all: Type.Optional(Type.Boolean({ description: "Resolve all matching messages" })),
	only: Type.Optional(Type.Array(Type.String(), { description: "Filters such as ack, reply, task, assigned" })),
	mode: Type.Optional(StringEnum(["acked", "read"] as const, { default: "acked" })),
});

const foxctlRoomMessagesResolveTool = defineTool({
	name: "foxctl_room_messages_resolve",
	label: "Foxctl Room Messages Resolve",
	description: "Resolve room messages in bulk. Coordinator role is required by foxctl.",
	parameters: FoxctlRoomMessagesResolveParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const room = params.room_id || getRoom(pi);
		if (!room) throw new Error("room_id required or set --foxctl-room");
		const result = await getClient(pi).post<Record<string, unknown>>(
			`/api/rooms/${path(room)}/messages/resolve`,
			{
				workspace_id: getWorkspace(pi),
				actor_id: getActor(pi),
				message_ids: params.message_ids,
				all: params.all,
				only: params.only,
				mode: params.mode,
			},
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlRoomLoopParams = Type.Object({
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
});

const foxctlRoomLoopTool = defineTool({
	name: "foxctl_room_loop",
	label: "Foxctl Room Loop",
	description: "Get room loop delivery/pulse configuration and cursor status.",
	parameters: FoxctlRoomLoopParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const room = params.room_id || getRoom(pi);
		if (!room) throw new Error("room_id required or set --foxctl-room");
		const result = await getClient(pi).get<Record<string, unknown>>(
			`/api/rooms/${path(room)}/loop${query({ workspace_id: getWorkspace(pi), actor_id: getActor(pi) })}`,
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- Tasks ---

const FoxctlTasksListParams = Type.Object({
	status: Type.Optional(StringEnum(["pending", "in_progress", "completed", "failed", "all"] as const, { default: "all" })),
});

const foxctlTasksListTool = defineTool({
	name: "foxctl_tasks_list",
	label: "Foxctl Tasks List",
	description: "List foxctl tasks with optional status filter.",
	parameters: FoxctlTasksListParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const query = params.status && params.status !== "all" ? `?status=${params.status}` : "";
		const tasks = await client.get<{ tasks?: Array<{ id: string; title?: string; status?: string }> }>(`/api/tasks${query}`, signal);
		const list = tasks.tasks || [];
		const text = list.length
			? list.map((t) => `- ${t.id}: ${t.title || "(untitled)"} [${t.status || "unknown"}]`).join("\n")
			: "No tasks found";
		return {
			content: [{ type: "text", text }],
			details: tasks,
		};
	},
});

const FoxctlTaskDetailParams = Type.Object({
	task_id: Type.String({ description: "Task ID" }),
});

const foxctlTaskDetailTool = defineTool({
	name: "foxctl_task_detail",
	label: "Foxctl Task Detail",
	description: "Get details about a specific foxctl task.",
	parameters: FoxctlTaskDetailParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const detail = await client.get<Record<string, unknown>>(`/api/tasks/${path(params.task_id)}`, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(detail, null, 2) }],
			details: detail,
		};
	},
});

const FoxctlTaskCompleteParams = Type.Object({
	task_id: Type.String({ description: "Task ID" }),
});

const foxctlTaskCompleteTool = defineTool({
	name: "foxctl_task_complete",
	label: "Foxctl Task Complete",
	description: "Mark a foxctl task as completed.",
	parameters: FoxctlTaskCompleteParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.post<Record<string, unknown>>(`/api/tasks/${path(params.task_id)}/complete`, {}, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- Search / Memory ---

const FoxctlSearchParams = Type.Object({
	query: Type.String({ description: "Search query" }),
	limit: Type.Optional(Type.Number({ description: "Max results", default: 10 })),
});

const foxctlSearchTool = defineTool({
	name: "foxctl_search",
	label: "Foxctl Search",
	description: "Search foxctl memory/semantic index.",
	parameters: FoxctlSearchParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const limit = params.limit ?? 10;
		const result = await client.get<Record<string, unknown>>(`/api/search?q=${encodeURIComponent(params.query)}&limit=${limit}`, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- Room Agile ---

const FoxctlRoomAgileParams = Type.Object({
	action: StringEnum([
		"epic_show", "epic_resume", "epic_health", "epic_next",
		"milestone_show", "story_show",
		// M4 mutating actions:
		"story_state", "story_validate", "story_propose", "story_accept",
	] as const, {
		description: "Room-agile action (read or mutating)",
	}),
	room_id: Type.Optional(Type.String({ description: "Room id; defaults to --foxctl-room" })),
	workspace: Type.Optional(Type.String({ description: "Workspace root; defaults to --foxctl-workspace" })),
	sender: Type.Optional(Type.String({ description: "Sender actor id; defaults to --foxctl-actor" })),
	actor: Type.Optional(Type.String({ description: "Actor id for actor-specific reads; defaults to --foxctl-actor" })),
	epic_id: Type.Optional(Type.String({ description: "Epic id for epic-specific actions" })),
	milestone_id: Type.Optional(Type.String({ description: "Milestone id for milestone_show" })),
	story_id: Type.Optional(Type.String({ description: "Story id for story_show" })),
	limit: Type.Optional(Type.Number({ description: "Maximum room messages to inspect", default: 200 })),
	// M4 mutating-action fields:
	state: Type.Optional(Type.String({ description: "Target story state: in_progress, in_review, done, waived" })),
	verdict: Type.Optional(Type.String({ description: "Validation verdict: pass, fail, waived" })),
	validator_type: Type.Optional(Type.String({ description: "Validator type: human, agent, harness" })),
	title: Type.Optional(Type.String({ description: "Story title for story_propose" })),
	goal: Type.Optional(Type.String({ description: "Story goal for story_propose" })),
	notes: Type.Optional(Type.String({ description: "Notes for story_propose or story_validate" })),
	proposal_id: Type.Optional(Type.String({ description: "Proposal ID for story_accept" })),
	command: Type.Optional(Type.String({ description: "Command that produced validation evidence" })),
	artifact: Type.Optional(Type.String({ description: "Artifact digest (sha256:...) for validation evidence" })),
});

const foxctlRoomAgileTool = defineTool({
	name: "foxctl_room_agile",
	label: "Foxctl Room Agile",
	description: "Run foxctl room-agile epic, milestone, story, and story lifecycle actions through the daemon API.",
	parameters: FoxctlRoomAgileParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const result = await runRoomAgile(getPi(), params as Record<string, unknown>, signal);
		return jsonToolResult(result);
	},
});

// --- M4 Story Lifecycle Tools ---

const FoxctlStoryStartParams = Type.Object({
	story_id: Type.String({ description: "Story ID to start" }),
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
	notes: Type.Optional(Type.String({ description: "Optional notes" })),
});

const foxctlStoryStartTool = defineTool({
	name: "foxctl_story_start",
	label: "Foxctl Story Start",
	description: "Move a story to in_progress state.",
	parameters: FoxctlStoryStartParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const result = await runRoomAgile(getPi(), {
			action: "story_state",
			story_id: params.story_id,
			state: "in_progress",
			notes: params.notes,
		}, signal);
		return jsonToolResult(result);
	},
});

const FoxctlStoryReviewParams = Type.Object({
	story_id: Type.String({ description: "Story ID to move to review" }),
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
	notes: Type.Optional(Type.String({ description: "Optional notes" })),
});

const foxctlStoryReviewTool = defineTool({
	name: "foxctl_story_review",
	label: "Foxctl Story Review",
	description: "Move a story to in_review state.",
	parameters: FoxctlStoryReviewParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const result = await runRoomAgile(getPi(), {
			action: "story_state",
			story_id: params.story_id,
			state: "in_review",
			notes: params.notes,
		}, signal);
		return jsonToolResult(result);
	},
});

const FoxctlStoryValidateParams = Type.Object({
	story_id: Type.String({ description: "Story ID to validate" }),
	verdict: StringEnum(["pass", "fail", "waived"] as const, { description: "Validation verdict" }),
	validator_type: StringEnum(["human", "agent", "harness"] as const, { description: "Who/what is validating" }),
	command: Type.Optional(Type.String({ description: "Command that ran" })),
	artifact: Type.Optional(Type.String({ description: "Artifact digest" })),
	notes: Type.Optional(Type.String({ description: "Validation notes" })),
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
});

const foxctlStoryValidateTool = defineTool({
	name: "foxctl_story_validate",
	label: "Foxctl Story Validate",
	description: "Attach a validation record to a story.",
	parameters: FoxctlStoryValidateParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const result = await runRoomAgile(getPi(), {
			action: "story_validate",
			story_id: params.story_id,
			verdict: params.verdict,
			validator_type: params.validator_type,
			command: params.command,
			artifact: params.artifact,
			notes: params.notes,
		}, signal);
		return jsonToolResult(result);
	},
});

// --- Skill-backed foxctl tool facades ---

const InlineModeEnum = StringEnum(["auto", "full", "preview", "artifact_only"] as const, { default: "auto" });

const FoxctlToolRunParams = Type.Object({
	command: Type.String({ description: "OpenAPI-enabled foxctl skill command, e.g. fs/read, code/smart_search, repo/index_search" }),
	input: Type.Optional(Type.String({ description: "JSON object input for the command" })),
	workspace: Type.Optional(Type.String({ description: "Workspace root; defaults to --foxctl-workspace" })),
});

const foxctlToolRunTool = defineTool({
	name: "foxctl_tool_run",
	label: "Foxctl Tool Run",
	description: "Run an OpenAPI-enabled foxctl skill as a Pi tool, scoped to the configured workspace root.",
	parameters: FoxctlToolRunParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const input = recordFromJSON(params.input);
		if (params.workspace) input.workspace = params.workspace;
		const result = await runFoxctlSkill(pi, params.command, input, signal);
		return jsonToolResult(result);
	},
});

const FoxctlFSListParams = Type.Object({
	path: Type.Optional(Type.String({ description: "Directory to list", default: "." })),
	include: Type.Optional(Type.Array(Type.String(), { description: "Include glob patterns" })),
	exclude: Type.Optional(Type.Array(Type.String(), { description: "Exclude glob patterns" })),
	max_entries: Type.Optional(Type.Number({ description: "Maximum entries", default: 200 })),
	show_hidden: Type.Optional(Type.Boolean({ description: "Show hidden files", default: false })),
	workspace: Type.Optional(Type.String({ description: "Workspace root; defaults to --foxctl-workspace" })),
});

const foxctlFSListTool = defineFoxctlSkillFacade({
	name: "foxctl_fs_list",
	label: "Foxctl FS List",
	description: "List files or directories through foxctl's workspace-scoped fs/ls skill.",
	skill: "fs/ls",
	parameters: FoxctlFSListParams,
});

const FoxctlFSReadParams = Type.Object({
	path: Type.String({ description: "File path to read" }),
	max_bytes: Type.Optional(Type.Number({ description: "Maximum preview bytes" })),
	workspace: Type.Optional(Type.String({ description: "Workspace root; defaults to --foxctl-workspace" })),
});

const foxctlFSReadTool = defineFoxctlSkillFacade({
	name: "foxctl_fs_read",
	label: "Foxctl FS Read",
	description: "Read a workspace file through foxctl's CAS-backed fs/read skill.",
	skill: "fs/read",
	parameters: FoxctlFSReadParams,
});

const foxctlFilesystemReadTool = defineTool({
	name: "foxctl_filesystem_read",
	label: "Foxctl Filesystem Read",
	description: "Alias for foxctl_fs_read.",
	parameters: FoxctlFSReadParams,
	async execute(toolCallId, params, signal, onUpdate, ctx) {
		return foxctlFSReadTool.execute(toolCallId, params, signal, onUpdate, ctx);
	},
});

const FoxctlFSFindParams = Type.Object({
	query: Type.Optional(Type.String({ description: "Fuzzy filename/path query" })),
	path: Type.Optional(Type.String({ description: "Starting directory", default: "." })),
	pattern: Type.Optional(Type.String({ description: "Glob pattern, e.g. *.go" })),
	type: Type.Optional(StringEnum(["file", "directory", "any"] as const, { default: "file" })),
	max_depth: Type.Optional(Type.Number({ description: "Maximum depth, 0 means unlimited", default: 0 })),
	hidden: Type.Optional(Type.Boolean({ description: "Include hidden files", default: false })),
	sort_by: Type.Optional(StringEnum(["relevance", "name", "size", "modified"] as const, { default: "relevance" })),
	max_results: Type.Optional(Type.Number({ description: "Maximum results", default: 100 })),
	workspace: Type.Optional(Type.String({ description: "Workspace root; defaults to --foxctl-workspace" })),
});

const foxctlFSFindTool = defineFoxctlSkillFacade({
	name: "foxctl_fs_find",
	label: "Foxctl FS Find",
	description: "Find files through foxctl's ranked fs/find skill.",
	skill: "fs/find",
	parameters: FoxctlFSFindParams,
});

const FoxctlTextGrepParams = Type.Object({
	pattern: Type.String({ description: "Go RE2 regex pattern" }),
	path: Type.Optional(Type.String({ description: "Directory or file", default: "." })),
	ci: Type.Optional(Type.Boolean({ description: "Case-insensitive search", default: false })),
	include: Type.Optional(Type.Array(Type.String(), { description: "Include glob patterns" })),
	exclude: Type.Optional(Type.Array(Type.String(), { description: "Exclude glob patterns" })),
	max_matches: Type.Optional(Type.Number({ description: "Maximum matches", default: 100000 })),
	workspace: Type.Optional(Type.String({ description: "Workspace root; defaults to --foxctl-workspace" })),
});

const foxctlTextGrepTool = defineFoxctlSkillFacade({
	name: "foxctl_text_grep",
	label: "Foxctl Text Grep",
	description: "Search workspace text with foxctl's text/grep skill.",
	skill: "text/grep",
	parameters: FoxctlTextGrepParams,
});

const FoxctlCodeSearchParams = Type.Object({
	query: Type.String({ description: "Natural-language code question" }),
	sources: Type.Optional(Type.Array(Type.String(), { description: "Candidate sources, e.g. symbols, ripgrep, semantic" })),
	repo_index_mode: Type.Optional(StringEnum(["auto", "search", "dag", "off"] as const, { default: "auto" })),
	inline_mode: Type.Optional(InlineModeEnum),
	limit: Type.Optional(Type.Number({ description: "Convenience max snippet count" })),
	max_candidates: Type.Optional(Type.Number({ description: "Maximum generated candidates" })),
	max_snippets: Type.Optional(Type.Number({ description: "Maximum snippets returned" })),
	workspace: Type.Optional(Type.String({ description: "Workspace root; defaults to --foxctl-workspace" })),
});

const foxctlCodeSearchTool = defineFoxctlSkillFacade({
	name: "foxctl_code_search",
	label: "Foxctl Code Search",
	description: "Search code through foxctl's smart search pipeline with repo-index-aware candidates.",
	skill: "code/smart_search",
	parameters: FoxctlCodeSearchParams,
	input(params) {
		const limit = typeof params.limit === "number" && params.limit > 0 ? params.limit : undefined;
		const limits: Record<string, unknown> = {};
		if (typeof params.max_candidates === "number" && params.max_candidates > 0) limits.max_candidates = params.max_candidates;
		if (typeof params.max_snippets === "number" && params.max_snippets > 0) limits.max_snippets = params.max_snippets;
		if (limit !== undefined && limits.max_snippets === undefined) limits.max_snippets = limit;
		if (limit !== undefined && limits.max_candidates === undefined) limits.max_candidates = Math.max(limit * 4, limit);
		return {
			question: params.query,
			sources: params.sources,
			repo_index_mode: params.repo_index_mode || "auto",
			inline_mode: params.inline_mode || "auto",
			limits: Object.keys(limits).length ? limits : undefined,
			workspace: params.workspace,
		};
	},
});

const FoxctlCodeSemanticSearchParams = Type.Object({
	query: Type.String({ description: "Natural-language query" }),
	scope: Type.Optional(Type.Array(Type.String(), { description: "Search scopes, e.g. symbols, sessions, memories, tasks, codemaps, context" })),
	profile: Type.Optional(Type.String({ description: "Retrieval profile, e.g. default or code" })),
	limit: Type.Optional(Type.Number({ description: "Maximum results", default: 20 })),
	repo_index_mode: Type.Optional(StringEnum(["auto", "search", "dag", "off"] as const, { default: "auto" })),
	format: Type.Optional(StringEnum(["json", "tree"] as const, { default: "json" })),
	inline_mode: Type.Optional(InlineModeEnum),
	workspace: Type.Optional(Type.String({ description: "Workspace root; defaults to --foxctl-workspace" })),
});

const foxctlCodeSemanticSearchTool = defineFoxctlSkillFacade({
	name: "foxctl_code_semantic_search",
	label: "Foxctl Code Semantic Search",
	description: "Run foxctl's code-oriented semantic search over symbols and repoindex by default.",
	skill: "code/semantic_search",
	parameters: FoxctlCodeSemanticSearchParams,
	input(params) {
		const scope = Array.isArray(params.scope) && params.scope.length > 0 ? params.scope : ["symbols"];
		return {
			query: params.query,
			scope,
			profile: params.profile || "code",
			limit: params.limit,
			repo_index_mode: params.repo_index_mode || "search",
			format: params.format || "json",
			inline_mode: params.inline_mode || "full",
			workspace: params.workspace,
		};
	},
});

const FoxctlCodeContextGrepParams = Type.Object({
	pattern: Type.Optional(Type.String({ description: "Regex or literal pattern for ripgrep mode" })),
	pattern_mode: Type.Optional(StringEnum(["regex", "literal"] as const, { default: "regex" })),
	path: Type.Optional(Type.String({ description: "Directory or file", default: "." })),
	mode: Type.Optional(StringEnum(["ripgrep", "ast", "line"] as const)),
	ast_pattern: Type.Optional(Type.String({ description: "Structural ast-grep pattern" })),
	language: Type.Optional(Type.String({ description: "AST language" })),
	file_path: Type.Optional(Type.String({ description: "File path for line expansion mode" })),
	line_start: Type.Optional(Type.Number({ description: "Start line for line expansion" })),
	line_end: Type.Optional(Type.Number({ description: "End line for line expansion" })),
	expand_to: Type.Optional(StringEnum(["function", "block", "class"] as const)),
	max_blocks: Type.Optional(Type.Number({ description: "Maximum code blocks", default: 50 })),
	inline_mode: Type.Optional(InlineModeEnum),
	workspace: Type.Optional(Type.String({ description: "Workspace root; defaults to --foxctl-workspace" })),
});

const foxctlCodeContextGrepTool = defineFoxctlSkillFacade({
	name: "foxctl_code_context_grep",
	label: "Foxctl Code Context Grep",
	description: "Search code and return surrounding functions/classes through foxctl's code/context_grep skill.",
	skill: "code/context_grep",
	parameters: FoxctlCodeContextGrepParams,
});

const FoxctlRepoindexSearchParams = Type.Object({
	query: Type.String({ description: "Short natural-language or symbol-name repo index query" }),
	limit: Type.Optional(Type.Number({ description: "Maximum results", default: 20 })),
	inline_mode: Type.Optional(InlineModeEnum),
	workspace: Type.Optional(Type.String({ description: "Workspace root; defaults to --foxctl-workspace" })),
});

const foxctlRepoindexSearchTool = defineFoxctlSkillFacade({
	name: "foxctl_repoindex_search",
	label: "Foxctl Repoindex Search",
	description: "Search repoindex nodes and projected anchors through foxctl.",
	skill: "repo/index_search",
	parameters: FoxctlRepoindexSearchParams,
});

const FoxctlRepoindexDAGParams = Type.Object({
	query: Type.String({ description: "Short natural-language or symbol-name repo index query" }),
	mode: Type.Optional(StringEnum(["fts", "semantic", "hybrid"] as const)),
	k: Type.Optional(Type.Number({ description: "Number of seed nodes", default: 10 })),
	node_kinds: Type.Optional(Type.Array(Type.String(), { description: "Node kinds: symbol, file, package, concept" })),
	edge_sets: Type.Optional(Type.Array(Type.String(), { description: "Edge sets: structural, doc, all" })),
	edge_types: Type.Optional(Type.Array(Type.String(), { description: "Explicit edge types" })),
	direction: Type.Optional(StringEnum(["out", "in"] as const)),
	depth: Type.Optional(Type.Number({ description: "Traversal depth", default: 2 })),
	budget: Type.Optional(Type.Number({ description: "Maximum nodes", default: 80 })),
	include_anchors: Type.Optional(Type.Boolean({ description: "Include anchors", default: false })),
	render: Type.Optional(StringEnum(["tree", "mermaid"] as const)),
	inline_mode: Type.Optional(InlineModeEnum),
	workspace: Type.Optional(Type.String({ description: "Workspace root; defaults to --foxctl-workspace" })),
});

const foxctlRepoindexDAGTool = defineFoxctlSkillFacade({
	name: "foxctl_repoindex_dag",
	label: "Foxctl Repoindex DAG",
	description: "Search and expand a compact repoindex explanation graph through foxctl.",
	skill: "code/dag_grep",
	parameters: FoxctlRepoindexDAGParams,
});

const FoxctlRepoindexExpandParams = Type.Object({
	seeds: Type.Array(Type.String(), { description: "Seed node IDs" }),
	edge_types: Type.Optional(Type.Array(Type.String(), { description: "Edge types to traverse" })),
	direction: Type.Optional(StringEnum(["out", "in"] as const)),
	depth: Type.Optional(Type.Number({ description: "Traversal depth", default: 1 })),
	budget: Type.Optional(Type.Number({ description: "Maximum nodes", default: 50 })),
	per_node_cap: Type.Optional(Type.Number({ description: "Maximum edges per node", default: 50 })),
	inline_mode: Type.Optional(InlineModeEnum),
	workspace: Type.Optional(Type.String({ description: "Workspace root; defaults to --foxctl-workspace" })),
});

const foxctlRepoindexExpandTool = defineFoxctlSkillFacade({
	name: "foxctl_repoindex_expand",
	label: "Foxctl Repoindex Expand",
	description: "Expand repoindex graph edges from seed node IDs through foxctl.",
	skill: "repo/index_expand",
	parameters: FoxctlRepoindexExpandParams,
});

const FoxctlRepoindexOpenParams = Type.Object({
	id: Type.String({ description: "Repoindex node ID" }),
	workspace: Type.Optional(Type.String({ description: "Workspace root; defaults to --foxctl-workspace" })),
});

const foxctlRepoindexOpenTool = defineFoxctlSkillFacade({
	name: "foxctl_repoindex_open",
	label: "Foxctl Repoindex Open",
	description: "Open a repoindex node by ID through foxctl.",
	skill: "repo/index_open",
	parameters: FoxctlRepoindexOpenParams,
});

const FoxctlRefactorScoutParams = Type.Object({
	path: Type.Optional(Type.String({ description: "File or directory to analyze", default: "." })),
	language: Type.Optional(StringEnum(["auto", "go", "python", "javascript", "typescript", "elixir", "rust"] as const, { default: "auto" })),
	focus: Type.Optional(StringEnum(["all", "slop", "dead"] as const, { default: "all" })),
	view: Type.Optional(StringEnum(["raw", "grouped", "entrypoints", "summary"] as const, { default: "grouped" })),
	include_tests: Type.Optional(Type.Boolean({ description: "Include test files", default: false })),
	max_results: Type.Optional(Type.Number({ description: "Maximum findings", default: 100 })),
	min_score: Type.Optional(Type.Number({ description: "Minimum score", default: 50 })),
	rule_set: Type.Optional(StringEnum(["conservative", "default", "aggressive"] as const, { default: "default" })),
	workspace: Type.Optional(Type.String({ description: "Workspace root; defaults to --foxctl-workspace" })),
});

const foxctlRefactorScoutTool = defineFoxctlSkillFacade({
	name: "foxctl_refactor_scout",
	label: "Foxctl Refactor Scout",
	description: "Run foxctl's deterministic read-only refactor scout.",
	skill: "code/refactor_scout",
	parameters: FoxctlRefactorScoutParams,
});

const foxctlRefactorPlanTool = defineFoxctlSkillFacade({
	name: "foxctl_refactor_plan",
	label: "Foxctl Refactor Plan",
	description: "Read-only refactor planning alias backed by foxctl_refactor_scout with an entrypoint-oriented view.",
	skill: "code/refactor_scout",
	parameters: FoxctlRefactorScoutParams,
	input(params) {
		return {
			...params,
			view: params.view || "entrypoints",
		};
	},
});

const FoxctlMemoryQueryParams = Type.Object({
	query: Type.Optional(Type.String({ description: "Natural-language memory query" })),
	file: Type.Optional(Type.String({ description: "Associated file path filter" })),
	kinds: Type.Optional(Type.String({ description: "Comma-separated memory kinds" })),
	lifecycle_states: Type.Optional(Type.String({ description: "Comma-separated lifecycle states" })),
	session_id: Type.Optional(Type.String({ description: "Session ID filter" })),
	limit: Type.Optional(Type.Number({ description: "Maximum records", default: 10 })),
	include_content: Type.Optional(Type.Boolean({ description: "Include full content", default: false })),
	workspace: Type.Optional(Type.String({ description: "Workspace root; defaults to --foxctl-workspace" })),
});

const foxctlMemoryQueryTool = defineFoxctlSkillFacade({
	name: "foxctl_memory_query",
	label: "Foxctl Memory Query",
	description: "Query canonical foxctl memory records with lifecycle and trust labels.",
	skill: "memory/query",
	parameters: FoxctlMemoryQueryParams,
});

const foxctlMemorySearchTool = defineTool({
	name: "foxctl_memory_search",
	label: "Foxctl Memory Search",
	description: "Alias for foxctl_memory_query.",
	parameters: FoxctlMemoryQueryParams,
	async execute(toolCallId, params, signal, onUpdate, ctx) {
		return foxctlMemoryQueryTool.execute(toolCallId, params, signal, onUpdate, ctx);
	},
});

const FoxctlSessionRecallParams = Type.Object({
	query: Type.String({ description: "Natural-language session recall query" }),
	limit: Type.Optional(Type.Number({ description: "Maximum sessions", default: 5 })),
	project: Type.Optional(Type.String({ description: "Project filter" })),
	session_id: Type.Optional(Type.String({ description: "Session ID filter" })),
	workspace: Type.Optional(Type.String({ description: "Workspace root; defaults to --foxctl-workspace" })),
});

const foxctlSessionRecallTool = defineFoxctlSkillFacade({
	name: "foxctl_session_recall",
	label: "Foxctl Session Recall",
	description: "Recall relevant prior sessions through foxctl's session/recall skill.",
	skill: "session/recall",
	parameters: FoxctlSessionRecallParams,
});

const FoxctlBlackboardPostParams = Type.Object({
	content: Type.String({ description: "Content to post" }),
	tags: Type.Optional(Type.Array(Type.String(), { description: "Optional tags" })),
});

const foxctlBlackboardPostTool = defineTool({
	name: "foxctl_blackboard_post",
	label: "Foxctl Blackboard Post",
	description: "Post a message to the foxctl blackboard.",
	parameters: FoxctlBlackboardPostParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.post<Record<string, unknown>>("/api/blackboard", { content: params.content, tags: params.tags }, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlBlackboardListParams = Type.Object({
	tag: Type.Optional(Type.String({ description: "Filter by tag" })),
	limit: Type.Optional(Type.Number({ description: "Max results", default: 20 })),
});

const foxctlBlackboardListTool = defineTool({
	name: "foxctl_blackboard_list",
	label: "Foxctl Blackboard List",
	description: "List foxctl blackboard entries.",
	parameters: FoxctlBlackboardListParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		let url = `/api/blackboard?limit=${params.limit ?? 20}`;
		if (params.tag) url += `&tag=${encodeURIComponent(params.tag)}`;
		const result = await client.get<Record<string, unknown>>(url, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- Codemaps ---

const FoxctlCodemapsListParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace filter" })),
});

const foxctlCodemapsListTool = defineTool({
	name: "foxctl_codemaps_list",
	label: "Foxctl Codemaps List",
	description: "List available codemaps.",
	parameters: FoxctlCodemapsListParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const url = params.workspace ? `/api/codemaps?workspace=${encodeURIComponent(params.workspace)}` : "/api/codemaps";
		const result = await client.get<Record<string, unknown>>(url, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlCodemapDetailParams = Type.Object({
	codemap_id: Type.String({ description: "Codemap ID" }),
});

const foxctlCodemapDetailTool = defineTool({
	name: "foxctl_codemap_detail",
	label: "Foxctl Codemap Detail",
	description: "Get details of a specific codemap.",
	parameters: FoxctlCodemapDetailParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>(`/api/codemaps/${path(params.codemap_id)}`, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- Orchestration Board ---

const FoxctlBoardParams = Type.Object({});

const foxctlBoardTool = defineTool({
	name: "foxctl_board",
	label: "Foxctl Board",
	description: "Get the current foxctl orchestration board state.",
	parameters: FoxctlBoardParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>("/api/orchestration/board-get", signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlBoardDispatchParams = Type.Object({
	card_id: Type.String({ description: "Card ID to dispatch" }),
});

const foxctlBoardDispatchTool = defineTool({
	name: "foxctl_board_dispatch",
	label: "Foxctl Board Dispatch",
	description: "Dispatch a card on the foxctl orchestration board.",
	parameters: FoxctlBoardDispatchParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.post<Record<string, unknown>>("/api/orchestration/dispatch-issue", { issue_id: params.card_id }, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- Jobs ---

const FoxctlJobsListParams = Type.Object({
	status: Type.Optional(StringEnum(["pending", "running", "completed", "failed", "all"] as const, { default: "all" })),
});

const foxctlJobsListTool = defineTool({
	name: "foxctl_jobs_list",
	label: "Foxctl Jobs List",
	description: "List foxctl jobs with optional status filter.",
	parameters: FoxctlJobsListParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const query = params.status && params.status !== "all" ? `?status=${params.status}` : "";
		const result = await client.get<Record<string, unknown>>(`/api/jobs${query}`, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlJobDetailParams = Type.Object({
	job_id: Type.String({ description: "Job ID" }),
});

const foxctlJobDetailTool = defineTool({
	name: "foxctl_job_detail",
	label: "Foxctl Job Detail",
	description: "Get details about a specific foxctl job.",
	parameters: FoxctlJobDetailParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>(`/api/jobs/${path(params.job_id)}`, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlJobCancelParams = Type.Object({
	job_id: Type.String({ description: "Job ID" }),
});

const foxctlJobCancelTool = defineTool({
	name: "foxctl_job_cancel",
	label: "Foxctl Job Cancel",
	description: "Cancel a running foxctl job.",
	parameters: FoxctlJobCancelParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.post<Record<string, unknown>>(`/api/jobs/${path(params.job_id)}/cancel`, {}, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- Workspaces ---

const FoxctlWorkspacesListParams = Type.Object({});

const foxctlWorkspacesListTool = defineTool({
	name: "foxctl_workspaces_list",
	label: "Foxctl Workspaces List",
	description: "List foxctl workspaces.",
	parameters: FoxctlWorkspacesListParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>("/api/workspaces", signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlWorkspaceSwitchParams = Type.Object({
	workspace: Type.String({ description: "Workspace name or path" }),
});

const foxctlWorkspaceSwitchTool = defineTool({
	name: "foxctl_workspace_switch",
	label: "Foxctl Workspace Switch",
	description: "Switch to a different foxctl workspace.",
	parameters: FoxctlWorkspaceSwitchParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.post<Record<string, unknown>>("/api/workspaces/switch", { workspace: params.workspace }, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- Sessions ---

const FoxctlSessionsListParams = Type.Object({});

const foxctlSessionsListTool = defineTool({
	name: "foxctl_sessions_list",
	label: "Foxctl Sessions List",
	description: "List foxctl sessions.",
	parameters: FoxctlSessionsListParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>("/api/sessions", signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlSessionDetailParams = Type.Object({
	session_id: Type.String({ description: "Session ID" }),
});

const foxctlSessionDetailTool = defineTool({
	name: "foxctl_session_detail",
	label: "Foxctl Session Detail",
	description: "Get details about a specific foxctl session.",
	parameters: FoxctlSessionDetailParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>(`/api/sessions/${path(params.session_id)}`, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- Companion ---

const FoxctlCompanionChatParams = Type.Object({
	message: Type.String({ description: "Message to send to the companion" }),
});

const foxctlCompanionChatTool = defineTool({
	name: "foxctl_companion_chat",
	label: "Foxctl Companion Chat",
	description: "Chat with the foxctl companion.",
	parameters: FoxctlCompanionChatParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.post<Record<string, unknown>>("/api/companion/chat", { message: params.message }, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlCompanionMemoryParams = Type.Object({
	action: StringEnum(["get", "set", "delete"] as const),
	key: Type.Optional(Type.String({ description: "Memory key" })),
	value: Type.Optional(Type.String({ description: "Memory value (for set)" })),
});

const foxctlCompanionMemoryTool = defineTool({
	name: "foxctl_companion_memory",
	label: "Foxctl Companion Memory",
	description: "Manage foxctl companion memory.",
	parameters: FoxctlCompanionMemoryParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		let result: Record<string, unknown>;
		switch (params.action) {
			case "get":
				result = await client.get<Record<string, unknown>>(`/api/companion/memory/${path(params.key || "default")}/context`, signal);
				break;
			case "set":
				throw new Error("foxctl_companion_memory set requires a conversation-scoped memory endpoint; use companion chat/context APIs instead");
				break;
			case "delete":
				result = await client.delete<Record<string, unknown>>(`/api/companion/memory/${path(params.key || "default")}`, signal);
				break;
		}
		return {
			content: [{ type: "text", text: JSON.stringify(result!, null, 2) }],
			details: result!,
		};
	},
});

// --- Context Plane ---

const FoxctlContextParams = Type.Object({});

const foxctlContextTool = defineTool({
	name: "foxctl_context",
	label: "Foxctl Context",
	description: "Get the current foxctl context plane overview.",
	parameters: FoxctlContextParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.get<Record<string, unknown>>(
			`/api/context/overview${query({ workspace: getWorkspace(pi), limit: 8 })}`,
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const ControlDecisionKindEnum = StringEnum(
	["approve", "reject", "defer", "escalate", "needs_clarification", "request_harness"] as const,
);
const ControlAuthorityModeEnum = StringEnum(
	["coordinator_policy", "room_consensus", "human_approval", "human_override"] as const,
);

const ControlEvidenceRefSchema = Type.Object({
	type: Type.String({ description: "Evidence ref kind, e.g. path, task, session, artifact" }),
	ref: Type.String({ description: "Opaque evidence ref value" }),
	workspace_id: Type.Optional(Type.String({ description: "Optional workspace ID override" })),
	title: Type.Optional(Type.String({ description: "Optional human-readable evidence title" })),
	excerpt: Type.Optional(Type.String({ description: "Optional evidence snippet" })),
});

const FoxctlControlInboxParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	limit: Type.Optional(Type.Number({ description: "Maximum proposals to return", default: 20 })),
	status: Type.Optional(Type.String({ description: "Optional proposal status filter" })),
});

const foxctlControlInboxTool = defineTool({
	name: "foxctl_control_inbox",
	label: "Foxctl Control Inbox",
	description: "List coordinator control proposals for cockpit triage.",
	parameters: FoxctlControlInboxParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const result = await listControlProposals(pi, signal, {
			workspace: params.workspace,
			limit: params.limit ?? 20,
			status: params.status,
		});
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlControlInspectParams = Type.Object({
	proposal_id: Type.String({ description: "Control proposal ID" }),
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
});

const foxctlControlInspectTool = defineTool({
	name: "foxctl_control_inspect",
	label: "Foxctl Control Inspect",
	description: "Inspect one coordinator control proposal by ID.",
	parameters: FoxctlControlInspectParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const workspace = params.workspace?.trim() || getWorkspace(pi);
		const client = getClient(pi);
		const proposal = await client.get<Record<string, unknown>>(
			`/api/context/control-proposals/${path(params.proposal_id)}${query({ workspace })}`,
			signal,
		);
		const result = {
			source: "web:/api/context/control-proposals/{id}",
			workspace,
			proposal,
		};
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlControlDecideParams = Type.Object({
	proposal_id: Type.String({ description: "Control proposal ID" }),
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	decision: ControlDecisionKindEnum,
	authority_mode: ControlAuthorityModeEnum,
	approval_actor: Type.Optional(Type.String({ description: "Actor recording the decision; defaults to --foxctl-actor" })),
	reason: Type.Optional(Type.String({ description: "Decision rationale for coordinator auditability" })),
	evidence_refs: Type.Array(ControlEvidenceRefSchema, { description: "Supporting evidence refs (required)" }),
	policy_id: Type.Optional(Type.String({ description: "Optional policy ID (required for coordinator_policy approvals unless policy_hash is set)" })),
	policy_version: Type.Optional(Type.String({ description: "Optional policy version" })),
	policy_hash: Type.Optional(Type.String({ description: "Optional policy hash (required with coordinator_policy approvals when policy_id is omitted)" })),
	room_consensus_id: Type.Optional(Type.String({ description: "Optional room consensus artifact ID" })),
	harness_run_ids: Type.Optional(Type.Array(Type.String(), { description: "Optional harness run IDs backing this decision" })),
});

const foxctlControlDecideTool = defineTool({
	name: "foxctl_control_decide",
	label: "Foxctl Control Decide",
	description: "Append a typed CoordinatorDecision for a control proposal (approve/reject/clarify/override by authority_mode).",
	parameters: FoxctlControlDecideParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const workspace = params.workspace?.trim() || getWorkspace(pi);
		const client = getClient(pi);
		const result = await client.post<Record<string, unknown>>(
			`/api/context/control-proposals/${path(params.proposal_id)}/decisions`,
			{
				workspace,
				decision: params.decision,
				authority_mode: params.authority_mode,
				approval_actor: params.approval_actor?.trim() || getActor(pi),
				reason: params.reason,
				evidence_refs: params.evidence_refs,
				policy_id: params.policy_id,
				policy_version: params.policy_version,
				policy_hash: params.policy_hash,
				room_consensus_id: params.room_consensus_id,
				harness_run_ids: params.harness_run_ids,
			},
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const GatherContextParams = Type.Object({});

const gatherContextTool = defineTool({
	name: "gather_context",
	label: "Gather Context",
	description: "Gather foxctl health, workspace context, tasks, rooms, and current room inbox/status in one call.",
	parameters: GatherContextParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const result = await gatherFoxctlContext(getPi(), signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const foxctlGatherContextTool = defineTool({
	name: "foxctl_gather_context",
	label: "Foxctl Gather Context",
	description: "Alias for gather_context.",
	parameters: GatherContextParams,
	async execute(toolCallId, params, signal, onUpdate, ctx) {
		return gatherContextTool.execute(toolCallId, params, signal, onUpdate, ctx);
	},
});

// --- Mux (tmux/zellij) ---

const FoxctlMuxListParams = Type.Object({});

const foxctlMuxListTool = defineTool({
	name: "foxctl_mux_list",
	label: "Foxctl Mux List",
	description: "List tmux/zellij panes via foxctl.",
	parameters: FoxctlMuxListParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>("/api/mux/panes", signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlMuxReadParams = Type.Object({
	pane_id: Type.String({ description: "Pane ID" }),
	lines: Type.Optional(Type.Number({ description: "Number of lines to read", default: 50 })),
});

const foxctlMuxReadTool = defineTool({
	name: "foxctl_mux_read",
	label: "Foxctl Mux Read",
	description: "Read output from a tmux/zellij pane.",
	parameters: FoxctlMuxReadParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>(
			`/api/mux/read${query({ target: params.pane_id, lines: params.lines ?? 50 })}`,
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- Mailbox ---

const FoxctlMailboxSendParams = Type.Object({
	room_id: Type.String({ description: "Room ID" }),
	message: Type.String({ description: "Message text" }),
});

const foxctlMailboxSendTool = defineTool({
	name: "foxctl_mailbox_send",
	label: "Foxctl Mailbox Send",
	description: "Send a message via the foxctl mailbox.",
	parameters: FoxctlMailboxSendParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.post<Record<string, unknown>>(
			`/api/rooms/${path(params.room_id)}/messages`,
			{ workspace_id: getWorkspace(getPi()), sender: getActor(getPi()), body: params.message },
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlMailboxListParams = Type.Object({
	room_id: Type.String({ description: "Room ID" }),
});

const foxctlMailboxListTool = defineTool({
	name: "foxctl_mailbox_list",
	label: "Foxctl Mailbox List",
	description: "List messages in a foxctl mailbox.",
	parameters: FoxctlMailboxListParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>(`/api/rooms/${path(params.room_id)}/messages`, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- CAS ---

const FoxctlCasListParams = Type.Object({});

const foxctlCasListTool = defineTool({
	name: "foxctl_cas_list",
	label: "Foxctl CAS List",
	description: "List objects in the foxctl CAS store.",
	parameters: FoxctlCasListParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>("/api/cas", signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlCasGetParams = Type.Object({
	hash: Type.String({ description: "Object hash" }),
});

const foxctlCasGetTool = defineTool({
	name: "foxctl_cas_get",
	label: "Foxctl CAS Get",
	description: "Get a specific object from the foxctl CAS store.",
	parameters: FoxctlCasGetParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>(`/api/cas/${path(params.hash)}/read`, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- Console ---

const FoxctlConsoleListParams = Type.Object({});

const foxctlConsoleListTool = defineTool({
	name: "foxctl_console_list",
	label: "Foxctl Console List",
	description: "List foxctl console sessions.",
	parameters: FoxctlConsoleListParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>("/api/console/sessions", signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlConsoleAskParams = Type.Object({
	console_id: Type.String({ description: "Console session ID" }),
	message: Type.String({ description: "Message to send" }),
});

const foxctlConsoleAskTool = defineTool({
	name: "foxctl_console_ask",
	label: "Foxctl Console Ask",
	description: "Send a message to a foxctl console session.",
	parameters: FoxctlConsoleAskParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.post<Record<string, unknown>>(`/api/console/sessions/${path(params.console_id)}/messages`, { message: params.message }, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- Stats ---

const FoxctlStatsParams = Type.Object({});

const foxctlStatsTool = defineTool({
	name: "foxctl_stats",
	label: "Foxctl Stats",
	description: "Get foxctl job statistics and task graph insights.",
	parameters: FoxctlStatsParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>("/api/stats", signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- Logs ---

const FoxctlLogsParams = Type.Object({
	query: Type.Optional(Type.String({ description: "Log query string" })),
	limit: Type.Optional(Type.Number({ description: "Max log lines", default: 100 })),
});

const foxctlLogsTool = defineTool({
	name: "foxctl_logs",
	label: "Foxctl Logs",
	description: "Query foxctl observability logs.",
	parameters: FoxctlLogsParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		let url = `/api/logs?limit=${params.limit ?? 100}`;
		if (params.query) url += `&q=${encodeURIComponent(params.query)}`;
		const result = await client.get<Record<string, unknown>>(url, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- Reservations ---

const FoxctlReservationsListParams = Type.Object({});

const foxctlReservationsListTool = defineTool({
	name: "foxctl_reservations_list",
	label: "Foxctl Reservations List",
	description: "List file reservations in foxctl.",
	parameters: FoxctlReservationsListParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>("/api/reservations", signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- SQLite ---

const FoxctlSqliteQueryParams = Type.Object({
	query: Type.String({ description: "SQL query to execute" }),
});

const foxctlSqliteQueryTool = defineTool({
	name: "foxctl_sqlite_query",
	label: "Foxctl SQLite Query",
	description: "Execute a SQLite query via foxctl's SQLite browser.",
	parameters: FoxctlSqliteQueryParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.post<Record<string, unknown>>("/api/sqlite/foxctl.db/query", { query: params.query }, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- V2 Runtime ---

const FoxctlV2RunsListParams = Type.Object({});

const foxctlV2RunsListTool = defineTool({
	name: "foxctl_v2_runs_list",
	label: "Foxctl V2 Runs List",
	description: "List foxctl v2 runtime runs.",
	parameters: FoxctlV2RunsListParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>("/api/v2/runs", signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlV2RunDetailParams = Type.Object({
	run_id: Type.String({ description: "Run ID" }),
});

const foxctlV2RunDetailTool = defineTool({
	name: "foxctl_v2_run_detail",
	label: "Foxctl V2 Run Detail",
	description: "Get details of a foxctl v2 runtime run.",
	parameters: FoxctlV2RunDetailParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>(`/api/v2/runs/${path(params.run_id)}`, signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- Foxprox ---

const FoxctlFoxproxRoomsParams = Type.Object({});

const foxctlFoxproxRoomsTool = defineTool({
	name: "foxctl_foxprox_rooms",
	label: "Foxctl Foxprox Rooms",
	description: "List rooms via foxprox bridge.",
	parameters: FoxctlFoxproxRoomsParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>("/api/foxprox/rooms", signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlFoxproxSpawnParams = Type.Object({
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
	cmd: Type.String({ description: "Shell command, or JSON argv array such as [\"codex\",\"--help\"]" }),
	cwd: Type.Optional(Type.String({ description: "Working directory" })),
	adapter: Type.Optional(Type.String({ description: "Adapter name, e.g. codex" })),
	role: Type.Optional(Type.String({ description: "Room role for the spawned session" })),
	submit_key: Type.Optional(Type.String({ description: "Submit key, e.g. enter or ctrl-j" })),
	can_mutate: Type.Optional(Type.Boolean({ description: "Whether the spawned session may mutate files" })),
});

const foxctlFoxproxSpawnTool = defineTool({
	name: "foxctl_foxprox_spawn",
	label: "Foxctl Foxprox Spawn",
	description: "Spawn an agent in a room via foxprox bridge.",
	parameters: FoxctlFoxproxSpawnParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const room = params.room_id || getRoom(pi);
		if (!room) throw new Error("room_id required or set --foxctl-room");
		const client = getClient(pi);
		const result = await client.post<Record<string, unknown>>(
			`/api/foxprox/foxctl-rooms/${path(room)}/spawn-cli`,
			{
				workspace_id: getWorkspace(pi),
				agent_id: getActor(pi),
				cmd: commandToArgv(params.cmd),
				cwd: params.cwd,
				adapter: params.adapter,
				role: params.role,
				submit_key: params.submit_key,
				can_mutate: params.can_mutate,
			},
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlFoxproxRoomSessionsParams = Type.Object({
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
});

const foxctlFoxproxRoomSessionsTool = defineTool({
	name: "foxctl_foxprox_room_sessions",
	label: "Foxctl Foxprox Room Sessions",
	description: "List foxprox terminal sessions attached to a foxctl room.",
	parameters: FoxctlFoxproxRoomSessionsParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const room = params.room_id || getRoom(pi);
		if (!room) throw new Error("room_id required or set --foxctl-room");
		const result = await getClient(pi).get<Record<string, unknown>>(
			`/api/foxprox/foxctl-rooms/${path(room)}/sessions${query({ workspace_id: getWorkspace(pi) })}`,
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlFoxproxRoomMessageParams = Type.Object({
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
	text: Type.String({ description: "Text to submit to a target foxprox room session" }),
	target_agent_id: Type.Optional(Type.String({ description: "Target room agent/session actor" })),
	submit_key: Type.Optional(Type.String({ description: "Submit key, e.g. enter or ctrl-j" })),
	await_activity_ms: Type.Optional(Type.Number({ description: "How long to wait for terminal activity" })),
	await_ready_ms: Type.Optional(Type.Number({ description: "How long to wait for target readiness" })),
	terminal_policy: Type.Optional(Type.String({ description: "Terminal routing policy" })),
	correlation_id: Type.Optional(Type.String({ description: "Caller-provided correlation ID" })),
});

const foxctlFoxproxRoomMessageTool = defineTool({
	name: "foxctl_foxprox_room_message",
	label: "Foxctl Foxprox Room Message",
	description: "Send text through foxprox to a room-attached terminal session.",
	parameters: FoxctlFoxproxRoomMessageParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const room = params.room_id || getRoom(pi);
		if (!room) throw new Error("room_id required or set --foxctl-room");
		const result = await getClient(pi).post<Record<string, unknown>>(
			`/api/foxprox/foxctl-rooms/${path(room)}/messages`,
			{
				workspace_id: getWorkspace(pi),
				source: getActor(pi),
				target_agent_id: params.target_agent_id,
				text: params.text,
				submit_key: params.submit_key,
				await_activity_ms: params.await_activity_ms,
				await_ready_ms: params.await_ready_ms,
				terminal_policy: params.terminal_policy,
				correlation_id: params.correlation_id,
			},
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlFoxproxStopSessionParams = Type.Object({
	room_id: Type.Optional(Type.String({ description: "Room ID; defaults to --foxctl-room" })),
	session_id: Type.String({ description: "Foxprox session ID to stop" }),
});

const foxctlFoxproxStopSessionTool = defineTool({
	name: "foxctl_foxprox_stop_session",
	label: "Foxctl Foxprox Stop Session",
	description: "Stop a foxprox terminal session attached to a room.",
	parameters: FoxctlFoxproxStopSessionParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const room = params.room_id || getRoom(pi);
		if (!room) throw new Error("room_id required or set --foxctl-room");
		const result = await getClient(pi).delete<Record<string, unknown>>(
			`/api/foxprox/foxctl-rooms/${path(room)}/sessions/${path(params.session_id)}${query({ workspace_id: getWorkspace(pi) })}`,
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- MCP ---

const FoxctlMcpStatusParams = Type.Object({});

const foxctlMcpStatusTool = defineTool({
	name: "foxctl_mcp_status",
	label: "Foxctl MCP Status",
	description: "Get foxctl MCP (Model Context Protocol) status.",
	parameters: FoxctlMcpStatusParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>("/api/mcp/status", signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

const FoxctlMcpToolsParams = Type.Object({});

const foxctlMcpToolsTool = defineTool({
	name: "foxctl_mcp_tools",
	label: "Foxctl MCP Tools",
	description: "List available MCP tools via foxctl.",
	parameters: FoxctlMcpToolsParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>("/api/mcp/tools", signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- OpenAPI ---

const FoxctlOpenapiParams = Type.Object({});

const foxctlOpenapiTool = defineTool({
	name: "foxctl_openapi",
	label: "Foxctl OpenAPI",
	description: "Get the foxctl OpenAPI specification.",
	parameters: FoxctlOpenapiParams,
	async execute(_toolCallId, _params, signal, _onUpdate, ctx) {
		const client = getClient(getPi());
		const result = await client.get<Record<string, unknown>>("/api/openapi.json", signal);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// ============================================================================
// ContextWiki Tools
// ============================================================================

// --- Context Show ---

const FoxctlContextShowParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
});

const foxctlContextShowTool = defineTool({
	name: "foxctl_context_show",
	label: "Foxctl Context Show",
	description: "Show the current ContextWiki top-of-mind bundle for the workspace.",
	parameters: FoxctlContextShowParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.get<Record<string, unknown>>(
			`/api/context/show${query({workspace: params.workspace || getWorkspace(pi)})}`,
			signal,
		);
		return jsonToolResult(result);
	},
});

// --- Context Overview ---

const FoxctlContextOverviewParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	limit: Type.Optional(Type.Number({ description: "Max items per category", default: 8 })),
});

const foxctlContextOverviewTool = defineTool({
	name: "foxctl_context_overview",
	label: "Foxctl Context Overview",
	description: "Get the ContextWiki overview: proposals, evidence imports, promotion jobs, and next merge tasks.",
	parameters: FoxctlContextOverviewParams,
	async execute(_toolCallId, params, signal, _onUpdate, ctx) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.get<Record<string, unknown>>(
			`/api/context/overview${query({ workspace: getWorkspace(pi), limit: params.limit ?? 8 })}`,
			signal,
		);
		return {
			content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
			details: result,
		};
	},
});

// --- Context Report ---

const FoxctlContextReportParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
});

const foxctlContextReportTool = defineTool({
	name: "foxctl_context_report",
	label: "Foxctl Context Report",
	description: "Build a synthesized ContextWiki current-state report.",
	parameters: FoxctlContextReportParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.get<Record<string, unknown>>(
			`/api/context/report${query({workspace: params.workspace || getWorkspace(pi)})}`,
			signal,
		);
		return jsonToolResult(result);
	},
});

// --- Context Next ---

const FoxctlContextNextParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
});

const foxctlContextNextTool = defineTool({
	name: "foxctl_context_next",
	label: "Foxctl Context Next",
	description: "Select the next ContextWiki task candidate from the workspace.",
	parameters: FoxctlContextNextParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.get<Record<string, unknown>>(
			`/api/context/next${query({workspace: params.workspace || getWorkspace(pi)})}`,
			signal,
		);
		return jsonToolResult(result);
	},
});

// --- Context Capture ---

const FoxctlContextCaptureParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	summary: Type.String({ description: "Compact handoff summary of what was accomplished" }),
	task_id: Type.Optional(Type.String({ description: "Task identifier" })),
	phase: Type.Optional(Type.String({ description: "Phase name (research, implementation, review, etc.)", default: "work" })),
	outcome: Type.Optional(StringEnum(["partial", "complete", "blocked"] as const, { default: "partial" })),
	observations: Type.Optional(Type.Array(Type.String(), { description: "Observations made during this work phase" })),
	tensions: Type.Optional(Type.Array(Type.String(), { description: "Tensions encountered during this work phase" })),
	next_actions: Type.Optional(Type.Array(Type.String(), { description: "Recommended next actions for whoever picks up this work" })),
	file_touched: Type.Optional(Type.Array(Type.String(), { description: "Files touched during this work phase" })),
	evidence_refs: Type.Optional(Type.Array(Type.String(), { description: "Evidence references (file paths, URLs, task IDs)" })),
});

const foxctlContextCaptureTool = defineTool({
	name: "foxctl_context_capture",
	label: "Foxctl Context Capture",
	description: "Capture a structured handoff into the ContextWiki control plane.",
	parameters: FoxctlContextCaptureParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.post<Record<string, unknown>>(
			"/api/context/capture",
			{ ...params, workspace: params.workspace || getWorkspace(pi) },
			signal,
		);
		return jsonToolResult(result);
	},
});

// --- Context Dispatch ---

const FoxctlContextDispatchParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	task_id: Type.Optional(Type.String({ description: "Explicit task ID (defaults to context next)" })),
});

const foxctlContextDispatchTool = defineTool({
	name: "foxctl_context_dispatch",
	label: "Foxctl Context Dispatch",
	description: "Build a bounded ContextWiki task packet for the next task.",
	parameters: FoxctlContextDispatchParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.get<Record<string, unknown>>(
			`/api/context/dispatch${query({workspace: params.workspace || getWorkspace(pi), task_id: params.task_id})}`,
			signal,
		);
		return jsonToolResult(result);
	},
});

// --- Context Handoffs ---

const FoxctlContextHandoffsParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	limit: Type.Optional(Type.Number({ description: "Max handoffs to return", default: 10 })),
});

const foxctlContextHandoffsTool = defineTool({
	name: "foxctl_context_handoffs",
	label: "Foxctl Context Handoffs",
	description: "List recorded ContextWiki handoffs from previous sessions.",
	parameters: FoxctlContextHandoffsParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.get<Record<string, unknown>>(
			`/api/context/handoffs${query({workspace: params.workspace || getWorkspace(pi), limit: params.limit ?? 10})}`,
			signal,
		);
		return jsonToolResult(result);
	},
});

// --- Context Observe ---

const FoxctlContextObserveParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	statement: Type.String({ description: "The observation statement (factual, repeatable)" }),
	area: Type.Optional(Type.String({ description: "Subsystem or area (e.g. memory, auth, api)" })),
	project: Type.Optional(Type.String({ description: "Project name override" })),
	confidence: Type.Optional(Type.Number({ description: "Confidence from 0.0 to 1.0", default: 0.7 })),
	evidence_refs: Type.Optional(Type.Array(Type.String(), { description: "Evidence references (file paths, URLs, task IDs)" })),
});

const foxctlContextObserveTool = defineTool({
	name: "foxctl_context_observe",
	label: "Foxctl Context Observe",
	description: "Record an observation into the ContextWiki control plane.",
	parameters: FoxctlContextObserveParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.post<Record<string, unknown>>(
			"/api/context/observe",
			{ ...params, workspace: params.workspace || getWorkspace(pi) },
			signal,
		);
		return jsonToolResult(result);
	},
});

// --- Context Tension ---

const FoxctlContextTensionParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	statement: Type.String({ description: "The tension statement" }),
	kind: Type.Optional(StringEnum(["contradiction", "performance", "complexity", "dependency", "usability"] as const, { default: "contradiction" })),
	impact: Type.Optional(StringEnum(["low", "medium", "high"] as const, { default: "medium" })),
	status: Type.Optional(StringEnum(["open", "investigating", "resolved"] as const, { default: "open" })),
	related_refs: Type.Optional(Type.Array(Type.String(), { description: "Related references" })),
});

const foxctlContextTensionTool = defineTool({
	name: "foxctl_context_tension",
	label: "Foxctl Context Tension",
	description: "Record a tension into the ContextWiki control plane.",
	parameters: FoxctlContextTensionParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.post<Record<string, unknown>>(
			"/api/context/tension",
			{ ...params, workspace: params.workspace || getWorkspace(pi) },
			signal,
		);
		return jsonToolResult(result);
	},
});

// --- Context Infer ---

const FoxctlContextInferParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	summary: Type.String({ description: "Compact summary text to analyze for observations/tensions" }),
	apply: Type.Optional(Type.Boolean({ description: "Persist the inferred items (default false = dry run)", default: false })),
	project: Type.Optional(Type.String({ description: "Project name override" })),
	area: Type.Optional(Type.String({ description: "Area name override" })),
});

const foxctlContextInferTool = defineTool({
	name: "foxctl_context_infer",
	label: "Foxctl Context Infer",
	description: "Infer ContextWiki observations and tensions from a summary text.",
	parameters: FoxctlContextInferParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.post<Record<string, unknown>>(
			"/api/context/infer",
			{ ...params, workspace: params.workspace || getWorkspace(pi) },
			signal,
		);
		return jsonToolResult(result);
	},
});

// ============================================================================
// Vault Tools
// ============================================================================

// --- Vault Search ---

const FoxctlVaultSearchParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	query: Type.String({ description: "Search query" }),
	limit: Type.Optional(Type.Number({ description: "Max results", default: 20 })),
});

const foxctlVaultSearchTool = defineTool({
	name: "foxctl_vault_search",
	label: "Foxctl Vault Search",
	description: "Search the Obsidian vault index for matching notes.",
	parameters: FoxctlVaultSearchParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.get<Record<string, unknown>>(
			`/api/vault/search${query({workspace: params.workspace || getWorkspace(pi), query: params.query, limit: params.limit ?? 20})}`,
			signal,
		);
		return jsonToolResult(result);
	},
});

// --- Vault Promote ---

const FoxctlVaultPromoteParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	slug: Type.String({ description: "URL-safe slug for the note (e.g. 'memory-search-architecture')" }),
	content: Type.String({ description: "Full markdown content of the note" }),
});

const foxctlVaultPromoteTool = defineTool({
	name: "foxctl_vault_promote",
	label: "Foxctl Vault Promote",
	description: "Create an evergreen promotion draft in the vault inbox.",
	parameters: FoxctlVaultPromoteParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.post<Record<string, unknown>>(
			"/api/vault/promote",
			{ ...params, workspace: params.workspace || getWorkspace(pi) },
			signal,
		);
		return jsonToolResult(result);
	},
});

// --- Vault Append ---

const FoxctlVaultAppendParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	path: Type.String({ description: "Vault note path (e.g. 'inbox/drafted-from-foxctl/my-note.md')" }),
	heading: Type.String({ description: "Heading to append under" }),
	content: Type.String({ description: "Markdown content to append" }),
});

const foxctlVaultAppendTool = defineTool({
	name: "foxctl_vault_append",
	label: "Foxctl Vault Append",
	description: "Append content under a heading in an existing vault note.",
	parameters: FoxctlVaultAppendParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.post<Record<string, unknown>>(
			"/api/vault/append",
			{ ...params, workspace: params.workspace || getWorkspace(pi) },
			signal,
		);
		return jsonToolResult(result);
	},
});

// --- Vault Bridge ---

const FoxctlVaultBridgeParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	docs_root: Type.Optional(Type.String({ description: "Repo docs root (default: <workspace>/docs)" })),
});

const foxctlVaultBridgeTool = defineTool({
	name: "foxctl_vault_bridge",
	label: "Foxctl Vault Bridge",
	description: "Reconcile repo docs with vault notes.",
	parameters: FoxctlVaultBridgeParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.post<Record<string, unknown>>(
			"/api/vault/bridge",
			{ ...params, workspace: params.workspace || getWorkspace(pi) },
			signal,
		);
		return jsonToolResult(result);
	},
});

// --- Vault Graph ---

const FoxctlVaultGraphParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
});

const foxctlVaultGraphTool = defineTool({
	name: "foxctl_vault_graph",
	label: "Foxctl Vault Graph",
	description: "Generate structured vault notes from the repo index graph.",
	parameters: FoxctlVaultGraphParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.post<Record<string, unknown>>(
			"/api/vault/graph",
			{ ...params, workspace: params.workspace || getWorkspace(pi) },
			signal,
		);
		return jsonToolResult(result);
	},
});

// --- Vault Index Build ---

const FoxctlVaultIndexBuildParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
});

const foxctlVaultIndexBuildTool = defineTool({
	name: "foxctl_vault_index_build",
	label: "Foxctl Vault Index Build",
	description: "Rebuild the local Obsidian vault index.",
	parameters: FoxctlVaultIndexBuildParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.post<Record<string, unknown>>(
			"/api/vault/index-build",
			{ ...params, workspace: params.workspace || getWorkspace(pi) },
			signal,
		);
		return jsonToolResult(result);
	},
});

// --- Vault Stats ---

const FoxctlVaultStatsParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
});

const foxctlVaultStatsTool = defineTool({
	name: "foxctl_vault_stats",
	label: "Foxctl Vault Stats",
	description: "Get Obsidian vault index statistics.",
	parameters: FoxctlVaultStatsParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.get<Record<string, unknown>>(
			`/api/vault/stats${query({workspace: params.workspace || getWorkspace(pi)})}`,
			signal,
		);
		return jsonToolResult(result);
	},
});

// ============================================================================
// Memory Write Tools
// ============================================================================

// --- Memory Put ---

const FoxctlMemoryPutParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	name: Type.String({ description: "Unique memory name (e.g. 'auth-architecture', 'gotcha-turso-conn-pool')" }),
	content: Type.String({ description: "Full content of the memory record" }),
	summary: Type.String({ description: "Short summary (used for search/BM25 ranking)" }),
	kind: Type.Optional(StringEnum(["knowledge", "decision", "gotcha", "preference", "pattern"] as const, { default: "knowledge" })),
	tags: Type.Optional(Type.Array(Type.String(), { description: "Tags for categorization" })),
	file_refs: Type.Optional(Type.Array(Type.String(), { description: "Related file paths" })),
});

const foxctlMemoryPutTool = defineTool({
	name: "foxctl_memory_put",
	label: "Foxctl Memory Put",
	description: "Store a knowledge record in foxctl's memory store.",
	parameters: FoxctlMemoryPutParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.post<Record<string, unknown>>(
			"/api/memory/put",
			{ ...params, workspace: params.workspace || getWorkspace(pi) },
			signal,
		);
		return jsonToolResult(result);
	},
});

// --- Memory Curator ---

const FoxctlMemoryCuratorParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	mode: Type.Optional(StringEnum(["dry_run", "apply"] as const, { default: "dry_run" })),
	limit: Type.Optional(Type.Number({ description: "Max records to examine", default: 100 })),
});

const foxctlMemoryCuratorTool = defineFoxctlSkillFacade({
	name: "foxctl_memory_curator",
	label: "Foxctl Memory Curator",
	description: "Run a deterministic curator report on the memory store. Identifies stale, duplicate, or low-quality records. Use dry_run mode to preview changes before applying.",
	skill: "memory_curator_report",
	parameters: FoxctlMemoryCuratorParams,
	input: (params, pi) => withWorkspace(pi, { ...params }),
});

// --- Context Memory Drafts ---

const FoxctlContextMemoryDraftsParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	vault_path: Type.Optional(Type.String({ description: "Obsidian vault path; defaults to daemon FOXCTL_CONTEXTWIKI_VAULT_PATH/FOXCTL_OBSIDIAN_VAULT_PATH" })),
	apply_drafts: Type.Optional(Type.Boolean({ description: "Write planned inbox drafts and prepared review proposals", default: false })),
	dry_run: Type.Optional(Type.Boolean({ description: "Prevent draft writes even when apply_drafts is true", default: true })),
	lookback: Type.Optional(Type.String({ description: "Retrieval feedback lookback window, e.g. 24h", default: "24h" })),
	limit: Type.Optional(Type.Number({ description: "Max memory drafts to plan", default: 20 })),
	blur_with_agent: Type.Optional(Type.Boolean({ description: "Ask a configured real agent to blur each planned draft", default: false })),
	blur_agent: Type.Optional(Type.String({ description: "Blur backend: pi, hermes, claude, foxctl, or command", default: "pi" })),
	blur_agent_bin: Type.Optional(Type.String({ description: "Executable override for the selected blur backend" })),
	blur_agent_provider: Type.Optional(Type.String({ description: "Provider override for Pi or Hermes blur backends" })),
	blur_agent_model: Type.Optional(Type.String({ description: "Model override for Pi, Hermes, or Claude blur backends" })),
	blur_agent_command: Type.Optional(Type.Array(Type.String(), { description: "Command argv for the command blur backend" })),
	blur_agent_prompt_mode: Type.Optional(Type.String({ description: "Prompt delivery for command backend: stdin or arg", default: "stdin" })),
	pi_mode: Type.Optional(Type.String({ description: "Pi runner mode: sdk or cli", default: "sdk" })),
	pi_sdk_bin: Type.Optional(Type.String({ description: "Executable for the Pi SDK runner", default: "bun" })),
	pi_sdk_script: Type.Optional(Type.String({ description: "Pi SDK memory blur runner script path" })),
	pi_sdk_cwd: Type.Optional(Type.String({ description: "Working directory passed to Pi SDK session" })),
	pi_agent_dir: Type.Optional(Type.String({ description: "Pi agent directory for auth and models" })),
	pi_thinking: Type.Optional(Type.String({ description: "Pi SDK thinking level", default: "off" })),
	foxctl_agent_id: Type.Optional(Type.String({ description: "Existing foxctl agent id for the foxctl blur backend" })),
	foxctl_dispatcher: Type.Optional(Type.String({ description: "Optional foxctl agent ask dispatcher" })),
	foxctl_conversation_id: Type.Optional(Type.String({ description: "Optional foxctl agent conversation id" })),
	pi_no_extensions: Type.Optional(Type.Boolean({ description: "Disable Pi extension discovery for blur runs", default: true })),
	hermes_ignore_rules: Type.Optional(Type.Boolean({ description: "Pass --ignore-rules to Hermes blur runs", default: true })),
	hermes_ignore_user_config: Type.Optional(Type.Boolean({ description: "Pass --ignore-user-config to Hermes blur runs", default: false })),
});

const foxctlContextMemoryDraftsTool = defineTool({
	name: "foxctl_context_memory_drafts",
	label: "Foxctl Context Memory Drafts",
	description: "Plan or write Obsidian inbox memory drafts from typed contextengine retrieval feedback. Canonical merges remain review-gated.",
	parameters: FoxctlContextMemoryDraftsParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.post<Record<string, unknown>>(
			"/api/context/memory-drafts",
			{
				...params,
				workspace: params.workspace || getWorkspace(pi),
				dry_run: params.dry_run ?? true,
			},
			signal,
		);
		return jsonToolResult(result);
	},
});

// ============================================================================
// Code Symbols
// ============================================================================

const FoxctlCodeSymbolsParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	path: Type.String({ description: "File path relative to workspace root" }),
});

const foxctlCodeSymbolsTool = defineFoxctlSkillFacade({
	name: "foxctl_code_symbols",
	label: "Foxctl Code Symbols",
	description: "Extract code symbols (functions, types, interfaces, methods) from a file. Use to understand the API surface of a file before reading it.",
	skill: "code_symbols",
	parameters: FoxctlCodeSymbolsParams,
	input: (params, pi) => withWorkspace(pi, { ...params }),
});

// ============================================================================
// Embedding Flush
// ============================================================================

const FoxctlEmbeddingFlushParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	batch_size: Type.Optional(Type.Number({ description: "Jobs per batch", default: 50 })),
	max_duration: Type.Optional(Type.Number({ description: "Max seconds", default: 120 })),
});

const foxctlEmbeddingFlushTool = defineTool({
	name: "foxctl_embedding_flush",
	label: "Foxctl Embedding Flush",
	description: "Process queued embedding jobs for semantic search indexing.",
	parameters: FoxctlEmbeddingFlushParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.post<Record<string, unknown>>(
			"/api/embedding/flush",
			{ ...params, workspace: params.workspace || getWorkspace(pi) },
			signal,
		);
		return jsonToolResult(result);
	},
});

// ============================================================================
// Publish Context
// ============================================================================

const FoxctlPublishContextParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	context: Type.String({ description: "Structured context to broadcast (markdown): current task, findings, blockers, needs" }),
});

const foxctlPublishContextTool = defineTool({
	name: "foxctl_publish_context",
	label: "Foxctl Publish Context",
	description: "Publish current context to the room for other agents to read.",
	parameters: FoxctlPublishContextParams,
	async execute(_toolCallId, params, signal) {
		const pi = getPi();
		const client = getClient(pi);
		const result = await client.post<Record<string, unknown>>(
			"/api/context/publish",
			{ ...params, workspace: params.workspace || getWorkspace(pi) },
			signal,
		);
		return jsonToolResult(result);
	},
});

// ============================================================================
// Session Extract Learnings
// ============================================================================

const FoxctlSessionExtractLearningsParams = Type.Object({
	workspace: Type.Optional(Type.String({ description: "Workspace override; defaults to --foxctl-workspace" })),
	session_id: Type.String({ description: "Session ID to extract from" }),
	dry_run: Type.Optional(Type.Boolean({ description: "Preview only, don't persist (default false)", default: false })),
});

const foxctlSessionExtractLearningsTool = defineFoxctlSkillFacade({
	name: "foxctl_session_extract_learnings",
	label: "Foxctl Session Extract Learnings",
	description: "Extract actionable learnings (gotchas, decisions, preferences, anti-patterns) from a session and store them as memory records. Call this at the end of a session to persist knowledge.",
	skill: "session_extract_learnings",
	parameters: FoxctlSessionExtractLearningsParams,
	input: (params, pi) => withWorkspace(pi, { ...params }),
});

// ============================================================================
// Extension Factory
// ============================================================================

/**
 * Registers foxctl tools, slash commands, and context hooks in Pi.
 *
 * Index:
 *   Purpose: Main Pi extension entrypoint for foxctl tool registration and hook wiring.
 *   Keywords: pi extension entrypoint, foxctl tool bridge, pi context hook
 *   Related: defineFoxctlSkillFacade, gatherFoxctlContext, bindPiToRoom
 *
 * [[domain:pi-foxctl-extension]]
 * [[protocol:pi-foxctl-tool-bridge]]
 * [[doc:integrations/pi/README.md#foxctl-pi-extension]]
 */
export default function foxctlExtension(pi: ExtensionAPI) {
	activePi = pi;

	// Register CLI flag for daemon URL
	pi.registerFlag("foxctl-url", {
		description: "URL of the foxctl daemon",
		type: "string",
		default: "http://localhost:8090",
	});
	pi.registerFlag("foxctl-gateway-url", {
		description: "URL of the foxctl terminal gateway for compatibility room terminals",
		type: "string",
		default: "http://localhost:8765",
	});
	pi.registerFlag("foxctl-workspace", {
		description: "foxctl workspace path or ID for context, rooms, and tasks",
		type: "string",
		default: ".",
	});
	pi.registerFlag("foxctl-room", {
		description: "foxctl room ID to bind context and inbox reads to",
		type: "string",
		default: "",
	});
	pi.registerFlag("foxctl-epic", {
		description: "foxctl room-agile epic ID to use for epic commands",
		type: "string",
		default: "",
	});
	pi.registerFlag("foxctl-milestone", {
		description: "foxctl room-agile milestone ID to use for milestone commands",
		type: "string",
		default: "",
	});
	pi.registerFlag("foxctl-story", {
		description: "foxctl room-agile story ID to use for story commands",
		type: "string",
		default: "",
	});
	pi.registerFlag("foxctl-actor", {
		description: "foxctl actor ID for Pi room membership and messages",
		type: "string",
		default: "actor:pi:local",
	});
	pi.registerFlag("foxctl-session", {
		description: "foxctl room session label for this Pi instance",
		type: "string",
		default: "",
	});
	pi.registerFlag("foxctl-transport-endpoint", {
		description: "Optional transport endpoint advertised in room member binding",
		type: "string",
		default: "",
	});
	pi.registerFlag("foxctl-room-bind", {
		description: "Bind this Pi actor into --foxctl-room at session start",
		type: "boolean",
		default: false,
	});
	pi.registerFlag("foxctl-context", {
		description: "Inject foxctl workspace/task/room context before each agent run",
		type: "boolean",
		default: false,
	});
	pi.registerFlag("foxctl-memory-context", {
		description: "Include foxctl memory/query and session/recall evidence in --foxctl-context hook injection",
		type: "boolean",
		default: true,
	});
	pi.registerFlag("foxctl-vault-path", {
		description: "Optional Obsidian vault path for foxctl memory draft writes",
		type: "string",
		default: "",
	});
	pi.registerFlag("foxctl-memory-drafts-auto", {
		description: "Automatically create Obsidian inbox memory drafts from retrieval feedback in background hooks",
		type: "boolean",
		default: false,
	});
	pi.registerFlag("foxctl-memory-drafts-apply", {
		description: "When --foxctl-memory-drafts-auto is enabled, write inbox drafts instead of previewing only",
		type: "boolean",
		default: true,
	});
	pi.registerFlag("foxctl-memory-drafts-dry-run", {
		description: "Force automatic memory drafts to preview mode even when apply is enabled",
		type: "boolean",
		default: false,
	});
	pi.registerFlag("foxctl-memory-drafts-interval-seconds", {
		description: "Background memory draft interval in seconds",
		type: "string",
		default: "900",
	});
	pi.registerFlag("foxctl-memory-drafts-lookback", {
		description: "Retrieval feedback lookback window for automatic memory drafts",
		type: "string",
		default: "24h",
	});
	pi.registerFlag("foxctl-memory-drafts-limit", {
		description: "Maximum automatic memory drafts per pass",
		type: "string",
		default: "20",
	});
	pi.registerFlag("foxctl-memory-drafts-blur-agent", {
		description: "Use a real agent to blur automatic memory drafts into structural schemas",
		type: "boolean",
		default: false,
	});
	pi.registerFlag("foxctl-memory-drafts-blur-backend", {
		description: "Automatic memory draft blur backend: pi, hermes, claude, or foxctl",
		type: "string",
		default: "pi",
	});
	pi.registerFlag("foxctl-memory-drafts-blur-agent-bin", {
		description: "Executable override for automatic memory draft blur backend",
		type: "string",
		default: "",
	});
	pi.registerFlag("foxctl-memory-drafts-blur-agent-provider", {
		description: "Provider override for automatic memory draft blur backend",
		type: "string",
		default: "",
	});
	pi.registerFlag("foxctl-memory-drafts-blur-agent-model", {
		description: "Model override for automatic memory draft blur backend",
		type: "string",
		default: "",
	});
	pi.registerFlag("foxctl-memory-drafts-blur-foxctl-agent-id", {
		description: "Existing foxctl agent id for automatic memory draft blur backend",
		type: "string",
		default: "",
	});
	pi.registerFlag("foxctl-memory-drafts-blur-pi-mode", {
		description: "Automatic memory draft Pi runner mode: sdk or cli",
		type: "string",
		default: "sdk",
	});
	pi.registerFlag("foxctl-memory-drafts-blur-pi-sdk-bin", {
		description: "Executable for automatic memory draft Pi SDK runner",
		type: "string",
		default: "bun",
	});
	pi.registerFlag("foxctl-memory-drafts-blur-pi-sdk-script", {
		description: "Pi SDK memory blur runner script path",
		type: "string",
		default: "",
	});
	pi.registerFlag("foxctl-memory-drafts-blur-pi-agent-dir", {
		description: "Pi agent directory for automatic memory draft blur auth/models",
		type: "string",
		default: "",
	});
	pi.registerFlag("foxctl-memory-drafts-blur-pi-thinking", {
		description: "Pi SDK thinking level for automatic memory draft blur",
		type: "string",
		default: "off",
	});
	pi.registerFlag("foxctl-epic-context", {
		description: "Inject active room-agile epic resume/health/next into --foxctl-context when --foxctl-epic is set",
		type: "boolean",
		default: true,
	});
	pi.registerFlag("foxctl-ui-status", {
		description: "Show foxctl health and room status in the Pi UI",
		type: "boolean",
		default: true,
	});

	// --- Register all tools ---
	const tools = [
		// Status / Health
		foxctlStatusTool,
		foxctlHealthTool,
		// Skills
		foxctlSkillsListTool,
		foxctlSkillRunTool,
		foxctlSkillDetailTool,
		// Agents
		foxctlAgentsListTool,
		foxctlAgentDetailTool,
		foxctlAgentSpawnTool,
		foxctlAgentAskTool,
		// Rooms
		foxctlRoomsListTool,
		foxctlRoomDetailTool,
		foxctlRoomMessagesTool,
		foxctlRoomSendTool,
		foxctlRoomInboxTool,
		foxctlRoomStatusTool,
		foxctlRoomControlSnapshotTool,
		foxctlRoomBindPiTool,
		foxctlRoomTerminalLinksTool,
		foxctlRoomTerminalRegisterTool,
		foxctlRoomTasksTool,
		foxctlRoomTaskCreateTool,
		foxctlRoomTaskActionTool,
		foxctlRoomMessageAckTool,
		foxctlRoomMessagesResolveTool,
		foxctlRoomLoopTool,
		// Tasks
		foxctlTasksListTool,
		foxctlTaskDetailTool,
		foxctlTaskCompleteTool,
		// Search / Memory
		foxctlSearchTool,
		foxctlRoomAgileTool,
		foxctlStoryStartTool,
		foxctlStoryReviewTool,
		foxctlStoryValidateTool,
		// Skill-backed foxctl tool facades
		foxctlToolRunTool,
		foxctlFSListTool,
		foxctlFSReadTool,
		foxctlFilesystemReadTool,
		foxctlFSFindTool,
		foxctlTextGrepTool,
		foxctlCodeSearchTool,
		foxctlCodeSemanticSearchTool,
		foxctlCodeContextGrepTool,
		foxctlRepoindexSearchTool,
		foxctlRepoindexDAGTool,
		foxctlRepoindexExpandTool,
		foxctlRepoindexOpenTool,
		foxctlRefactorScoutTool,
		foxctlRefactorPlanTool,
		foxctlMemoryQueryTool,
		foxctlMemorySearchTool,
		foxctlSessionRecallTool,
		foxctlBlackboardPostTool,
		foxctlBlackboardListTool,
		// Codemaps
		foxctlCodemapsListTool,
		foxctlCodemapDetailTool,
		// Orchestration
		foxctlBoardTool,
		foxctlBoardDispatchTool,
		// Jobs
		foxctlJobsListTool,
		foxctlJobDetailTool,
		foxctlJobCancelTool,
		// Workspaces
		foxctlWorkspacesListTool,
		foxctlWorkspaceSwitchTool,
		// Sessions
		foxctlSessionsListTool,
		foxctlSessionDetailTool,
		// Companion
		foxctlCompanionChatTool,
		foxctlCompanionMemoryTool,
		// Context Plane
		foxctlContextTool,
		foxctlControlInboxTool,
		foxctlControlInspectTool,
		foxctlControlDecideTool,
		gatherContextTool,
		foxctlGatherContextTool,
		// ContextWiki
		foxctlContextShowTool,
		foxctlContextOverviewTool,
		foxctlContextReportTool,
		foxctlContextNextTool,
		foxctlContextCaptureTool,
		foxctlContextDispatchTool,
		foxctlContextHandoffsTool,
		foxctlContextObserveTool,
		foxctlContextTensionTool,
		foxctlContextInferTool,
		// Vault
		foxctlVaultSearchTool,
		foxctlVaultPromoteTool,
		foxctlVaultAppendTool,
		foxctlVaultBridgeTool,
		foxctlVaultGraphTool,
		foxctlVaultIndexBuildTool,
		foxctlVaultStatsTool,
		// Memory Write
		foxctlMemoryPutTool,
		foxctlMemoryCuratorTool,
		foxctlContextMemoryDraftsTool,
		// Code Symbols
		foxctlCodeSymbolsTool,
		// Embedding
		foxctlEmbeddingFlushTool,
		// Publish Context
		foxctlPublishContextTool,
		// Session Learnings
		foxctlSessionExtractLearningsTool,
		// Mux
		foxctlMuxListTool,
		foxctlMuxReadTool,
		// Mailbox
		foxctlMailboxSendTool,
		foxctlMailboxListTool,
		// CAS
		foxctlCasListTool,
		foxctlCasGetTool,
		// Console
		foxctlConsoleListTool,
		foxctlConsoleAskTool,
		// Stats
		foxctlStatsTool,
		// Logs
		foxctlLogsTool,
		// Reservations
		foxctlReservationsListTool,
		// SQLite
		foxctlSqliteQueryTool,
		// V2 Runtime
		foxctlV2RunsListTool,
		foxctlV2RunDetailTool,
		// Foxprox
		foxctlFoxproxRoomsTool,
		foxctlFoxproxSpawnTool,
		foxctlFoxproxRoomSessionsTool,
		foxctlFoxproxRoomMessageTool,
		foxctlFoxproxStopSessionTool,
		// MCP
		foxctlMcpStatusTool,
		foxctlMcpToolsTool,
		// OpenAPI
		foxctlOpenapiTool,
	];

	for (const tool of tools) {
		pi.registerTool(tool);
	}

	// --- Register commands ---
	const focusedFoxctlToolNames = [
		"foxctl_tool_run",
		"foxctl_fs_list",
		"foxctl_fs_read",
		"foxctl_filesystem_read",
		"foxctl_fs_find",
		"foxctl_text_grep",
		"foxctl_code_search",
		"foxctl_code_semantic_search",
		"foxctl_code_context_grep",
		"foxctl_repoindex_search",
		"foxctl_repoindex_dag",
		"foxctl_repoindex_expand",
		"foxctl_repoindex_open",
		"foxctl_refactor_scout",
		"foxctl_refactor_plan",
		"foxctl_memory_query",
		"foxctl_memory_search",
		"foxctl_session_recall",
		"foxctl_room_agile",
		"foxctl_story_start",
		"foxctl_story_review",
		"foxctl_story_validate",
		"foxctl_control_inbox",
		"foxctl_control_inspect",
		"foxctl_control_decide",
		"foxctl_context_show",
		"foxctl_context_overview",
		"foxctl_context_report",
		"foxctl_context_next",
		"foxctl_context_capture",
		"foxctl_context_dispatch",
		"foxctl_context_handoffs",
		"foxctl_context_observe",
		"foxctl_context_tension",
		"foxctl_context_infer",
		"foxctl_vault_search",
		"foxctl_vault_promote",
		"foxctl_vault_append",
		"foxctl_vault_bridge",
		"foxctl_vault_graph",
		"foxctl_vault_index_build",
		"foxctl_vault_stats",
		"foxctl_memory_put",
		"foxctl_memory_curator",
		"foxctl_context_memory_drafts",
		"foxctl_code_symbols",
		"foxctl_embedding_flush",
		"foxctl_publish_context",
		"foxctl_session_extract_learnings",
	];

	const notifyGatherContext = async (ctx: Parameters<Parameters<typeof pi.registerCommand>[1]["handler"]>[1]) => {
		try {
			const gathered = await gatherFoxctlContext(getPi(), ctx.signal);
			ctx.ui.notify(`Context: ${JSON.stringify(gathered).slice(0, 500)}`, "info");
		} catch (e) {
			ctx.ui.notify(`foxctl context unavailable: ${e instanceof Error ? e.message : String(e)}`, "error");
		}
	};

	pi.registerCommand("foxctl-status", {
		description: "Show foxctl daemon status",
		handler: async (_args, ctx) => {
			const client = getClient(getPi());
			try {
				const status = await client.get<Record<string, unknown>>("/api/health", ctx.signal);
				ctx.ui.notify(`foxctl: ${JSON.stringify(status)}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxctl unreachable: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("status", {
		description: "Alias for foxctl-status",
		handler: async (_args, ctx) => {
			const client = getClient(getPi());
			try {
				const status = await client.get<Record<string, unknown>>("/api/health", ctx.signal);
				ctx.ui.notify(`foxctl: ${JSON.stringify(status)}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxctl unreachable: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("foxctl-skills", {
		description: "List foxctl skills",
		handler: async (_args, ctx) => {
			const client = getClient(getPi());
			try {
				const skills = await client.get<{ skills?: Array<{ name: string }> }>("/api/skills", ctx.signal);
				const list = skills.skills?.map((s) => s.name).join(", ") || "none";
				ctx.ui.notify(`Skills: ${truncate(list, 200)}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxctl unreachable: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("foxctl-tools", {
		description: "List focused foxctl Pi tool wrappers",
		handler: async (_args, ctx) => {
			ctx.ui.notify(`Foxctl tools: ${focusedFoxctlToolNames.join(", ")}`, "info");
		},
	});

	pi.registerCommand("foxctl-agents", {
		description: "List foxctl agents",
		handler: async (_args, ctx) => {
			const client = getClient(getPi());
			try {
				const agents = await client.get<{ agents?: Array<{ id: string; name?: string }> }>("/api/agents", ctx.signal);
				const list = agents.agents?.map((a) => a.name || a.id).join(", ") || "none";
				ctx.ui.notify(`Agents: ${truncate(list, 200)}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxctl unreachable: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("foxctl-rooms", {
		description: "List foxctl rooms",
		handler: async (_args, ctx) => {
			const client = getClient(getPi());
			try {
				const rooms = await client.get<{ rooms?: Array<{ id: string; name?: string }> }>(
					`/api/rooms${query({ workspace_id: getWorkspace(getPi()), actor_id: getActor(getPi()), limit: 50 })}`,
					ctx.signal,
				);
				const list = rooms.rooms?.map((r) => r.name || r.id).join(", ") || "none";
				ctx.ui.notify(`Rooms: ${truncate(list, 200)}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxctl unreachable: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("foxctl-room-bind", {
		description: "Bind this Pi actor into the configured foxctl room",
		handler: async (_args, ctx) => {
			const room = getRoom(getPi());
			if (!room) {
				ctx.ui.notify("Set --foxctl-room before binding Pi to a room", "warning");
				return;
			}
			try {
				await bindPiToRoom(getPi(), room, ctx.signal);
				ctx.ui.notify(`Bound ${getActor(getPi())} to room ${room}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxctl room bind failed: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("foxctl-terminal", {
		description: "Register and show the compatibility browser terminal for the configured foxctl room",
		handler: async (_args, ctx) => {
			const pi = getPi();
			const room = getRoom(pi);
			if (!room) {
				ctx.ui.notify("Set --foxctl-room before opening a room terminal", "warning");
				return;
			}
			try {
				const registration = await getGatewayClient(pi).post<Record<string, unknown>>(
					"/api/rooms",
					{ room_id: room },
					ctx.signal,
				);
				const links = buildRoomTerminalLinks(getGatewayURL(pi), room);
				const details = {
					...links,
					registration,
				};
				ctx.ui.notify(`Room terminal: ${String(links["terminal_url"])}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxctl terminal gateway failed: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("foxctl-room-inbox", {
		description: "Show pending messages for the configured foxctl room",
		handler: async (_args, ctx) => {
			const room = getRoom(getPi());
			if (!room) {
				ctx.ui.notify("Set --foxctl-room before reading room inbox", "warning");
				return;
			}
			try {
				const inbox = await getClient(getPi()).get<Record<string, unknown>>(
					`/api/rooms/${path(room)}/inbox${query({
						workspace_id: getWorkspace(getPi()),
						actor_id: getActor(getPi()),
						only: "pending",
						limit: 20,
					})}`,
					ctx.signal,
				);
				ctx.ui.notify(`Inbox: ${JSON.stringify(inbox).slice(0, 300)}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxctl room inbox failed: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("inbox", {
		description: "Alias for foxctl-room-inbox",
		handler: async (_args, ctx) => {
			const room = getRoom(getPi());
			if (!room) {
				ctx.ui.notify("Set --foxctl-room before reading room inbox", "warning");
				return;
			}
			try {
				const inbox = await getClient(getPi()).get<Record<string, unknown>>(
					`/api/rooms/${path(room)}/inbox${query({
						workspace_id: getWorkspace(getPi()),
						actor_id: getActor(getPi()),
						only: "pending",
						limit: 20,
					})}`,
					ctx.signal,
				);
				ctx.ui.notify(`Inbox: ${JSON.stringify(inbox).slice(0, 300)}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxctl room inbox failed: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("foxctl-room-status", {
		description: "Show status for the configured foxctl room",
		handler: async (_args, ctx) => {
			const room = getRoom(getPi());
			if (!room) {
				ctx.ui.notify("Set --foxctl-room before checking room status", "warning");
				return;
			}
			try {
				const status = await getClient(getPi()).get<Record<string, unknown>>(
					`/api/rooms/${path(room)}/status${query({ workspace_id: getWorkspace(getPi()), actor_id: getActor(getPi()) })}`,
					ctx.signal,
				);
				ctx.ui.notify(`Room status: ${JSON.stringify(status).slice(0, 300)}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxctl room status failed: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	const notifyRoomAgile = async (
		ctx: Parameters<Parameters<typeof pi.registerCommand>[1]["handler"]>[1],
		action: string,
		extra: Record<string, unknown> = {},
	) => {
		try {
			const result = await runRoomAgile(getPi(), { action, limit: 200, ...extra }, ctx.signal);
			ctx.ui.notify(`${action}: ${JSON.stringify(result).slice(0, 700)}`, "info");
		} catch (e) {
			ctx.ui.notify(`foxctl room-agile ${action} failed: ${e instanceof Error ? e.message : String(e)}`, "error");
		}
	};

	pi.registerCommand("epic", {
		description: "Show the configured foxctl room-agile epic, or list epics when --foxctl-epic is unset",
		handler: async (_args, ctx) => {
			const epicID = getEpicOverride(getPi());
			await notifyRoomAgile(ctx, "epic_show", epicID ? { epic_id: epicID } : {});
		},
	});

	pi.registerCommand("epic-next", {
		description: "Show next actions for the configured foxctl room-agile epic",
		handler: async (_args, ctx) => {
			const epicID = getEpicOverride(getPi());
			if (!epicID) {
				ctx.ui.notify("Set --foxctl-epic before using /epic-next", "warning");
				return;
			}
			await notifyRoomAgile(ctx, "epic_next", { epic_id: epicID });
		},
	});

	pi.registerCommand("epic-health", {
		description: "Show health warnings for the configured foxctl room-agile epic",
		handler: async (_args, ctx) => {
			const epicID = getEpicOverride(getPi());
			if (!epicID) {
				ctx.ui.notify("Set --foxctl-epic before using /epic-health", "warning");
				return;
			}
			await notifyRoomAgile(ctx, "epic_health", { epic_id: epicID });
		},
	});

	pi.registerCommand("milestones", {
		description: "Show foxctl room-agile milestones",
		handler: async (_args, ctx) => {
			const milestoneID = getMilestoneOverride(getPi());
			await notifyRoomAgile(ctx, "milestone_show", milestoneID ? { milestone_id: milestoneID } : {});
		},
	});

	pi.registerCommand("stories", {
		description: "Show foxctl room-agile stories",
		handler: async (_args, ctx) => {
			const storyID = getStoryOverride(getPi());
			await notifyRoomAgile(ctx, "story_show", storyID ? { story_id: storyID } : {});
		},
	});

	// M4 Story Lifecycle commands

	pi.registerCommand("story-start", {
		description: "Mark a story as in_progress",
		handler: async (args, ctx) => {
			const storyID = args.trim() || getStoryOverride(getPi());
			if (!storyID) {
				ctx.ui.notify("Usage: /story-start <story-id> or set --foxctl-story", "warning");
				return;
			}
			await notifyRoomAgile(ctx, "story_state", { story_id: storyID, state: "in_progress" });
			await refreshEpicWidget(getPi(), ctx);
		},
	});

	pi.registerCommand("story-review", {
		description: "Mark a story as in_review",
		handler: async (args, ctx) => {
			const storyID = args.trim() || getStoryOverride(getPi());
			if (!storyID) {
				ctx.ui.notify("Usage: /story-review <story-id> or set --foxctl-story", "warning");
				return;
			}
			await notifyRoomAgile(ctx, "story_state", { story_id: storyID, state: "in_review" });
			await refreshEpicWidget(getPi(), ctx);
		},
	});

	pi.registerCommand("story-validate", {
		description: "Validate a story with verdict and validator type",
		handler: async (args, ctx) => {
			const storyID = getStoryOverride(getPi());
			if (!storyID) {
				ctx.ui.notify("Set --foxctl-story before using /story-validate", "warning");
				return;
			}
			const parts = args.trim().split(/\s+/);
			const verdict = parts[0] || "pass";
			const validatorType = parts[1] || "human";
			await notifyRoomAgile(ctx, "story_validate", {
				story_id: storyID,
				verdict,
				validator_type: validatorType,
			});
			await refreshEpicWidget(getPi(), ctx);
		},
	});

	// M5: Interactive selectors and widget refresh commands

	pi.registerCommand("epic-select", {
		description: "Interactively select an active epic from the current room",
		handler: async (_args, ctx) => {
			const room = getRoom(getPi());
			if (!room) {
				ctx.ui.notify("Set --foxctl-room before selecting an epic", "warning");
				return;
			}
			try {
				const epicRes = await runRoomAgile(getPi(), { action: "epic_show", limit: 200 }, ctx.signal);
				const epicData = (epicRes.result as Record<string, unknown>)?.data as Record<string, unknown> | undefined;
				const epics = (epicData?.epics || []) as Array<Record<string, unknown>>;
				if (epics.length === 0) {
					ctx.ui.notify("No epics found in this room", "info");
					return;
				}

				const result = await ctx.ui.custom<{ id: string; title: string } | undefined>(
					(tui, theme, _keybindings, done) => {
						const selectTheme = buildSelectListTheme(theme);
						const items = epics.map((ep) => ({
							value: String(ep.id || ""),
							label: String(ep.title || ep.id || "untitled"),
							description: String(ep.status || ""),
						}));
						const list = new SelectList(items, 10, selectTheme);
						list.onSelect = (item) => done({ id: item.value, title: item.label });
						list.onCancel = () => done(undefined);
						return list;
					},
					{ overlay: true },
				);

				if (result) {
					_flagOverrides["foxctl-epic"] = result.id;
					ctx.ui.notify(`Active epic: ${result.title} (${result.id})`, "info");
					await refreshEpicWidget(getPi(), ctx);
				}
			} catch (e) {
				ctx.ui.notify(`epic-select failed: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("milestone-select", {
		description: "Interactively select a milestone for the active epic",
		handler: async (_args, ctx) => {
			const epicID = getEpicOverride(getPi());
			if (!epicID) {
				ctx.ui.notify("Set --foxctl-epic before selecting a milestone", "warning");
				return;
			}
			try {
				const res = await runRoomAgile(getPi(), { action: "epic_resume", epic_id: epicID, limit: 200 }, ctx.signal);
				const epicData = (res.result as Record<string, unknown>)?.data as Record<string, unknown> | undefined;
				const milestones = (epicData?.milestones || []) as Array<Record<string, unknown>>;
				if (milestones.length === 0) {
					ctx.ui.notify("No milestones found for this epic", "info");
					return;
				}

				const result = await ctx.ui.custom<{ id: string; title: string } | undefined>(
					(tui, theme, _kb, done) => {
						const selectTheme = buildSelectListTheme(theme);
						const items = milestones.map((ms) => ({
							value: String(ms.id || ""),
							label: String(ms.title || ms.id || "untitled"),
							description: String(ms.status || "open"),
						}));
						const list = new SelectList(items, 10, selectTheme);
						list.onSelect = (item) => done({ id: item.value, title: item.label });
						list.onCancel = () => done(undefined);
						return list;
					},
					{ overlay: true },
				);

				if (result) {
					_flagOverrides["foxctl-milestone"] = result.id;
					ctx.ui.notify(`Active milestone: ${result.title} (${result.id})`, "info");
					await refreshEpicWidget(getPi(), ctx);
				}
			} catch (e) {
				ctx.ui.notify(`milestone-select failed: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("story-select", {
		description: "Interactively select a story for the active epic",
		handler: async (_args, ctx) => {
			const epicID = getEpicOverride(getPi());
			if (!epicID) {
				ctx.ui.notify("Set --foxctl-epic before selecting a story", "warning");
				return;
			}
			try {
				const res = await runRoomAgile(getPi(), { action: "epic_resume", epic_id: epicID, limit: 200 }, ctx.signal);
				const epicData = (res.result as Record<string, unknown>)?.data as Record<string, unknown> | undefined;
				const stories = (epicData?.stories || []) as Array<Record<string, unknown>>;
				if (stories.length === 0) {
					ctx.ui.notify("No stories found for this epic", "info");
					return;
				}

				const result = await ctx.ui.custom<{ id: string; title: string } | undefined>(
					(tui, theme, _kb, done) => {
						const selectTheme = buildSelectListTheme(theme);
						const items = stories.map((st) => ({
							value: String(st.id || ""),
							label: String(st.title || st.id || "untitled"),
							description: String(st.status || "accepted"),
						}));
						const list = new SelectList(items, 10, selectTheme);
						list.onSelect = (item) => done({ id: item.value, title: item.label });
						list.onCancel = () => done(undefined);
						return list;
					},
					{ overlay: true },
				);

				if (result) {
					_flagOverrides["foxctl-story"] = result.id;
					ctx.ui.notify(`Active story: ${result.title} (${result.id})`, "info");
					await refreshEpicWidget(getPi(), ctx);
				}
			} catch (e) {
				ctx.ui.notify(`story-select failed: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("epic-refresh", {
		description: "Refresh the epic status widget from the foxctl daemon",
		handler: async (_args, ctx) => {
			await refreshEpicWidget(getPi(), ctx);
			ctx.ui.notify("Epic widget refreshed", "info");
		},
	});

	pi.registerShortcut(Key.ctrlShift("e"), {
		description: "Refresh foxctl epic status widget",
		handler: async (ctx) => {
			if (!ctx.hasUI) return;
			await refreshEpicWidget(getPi(), ctx);
		},
	});

	pi.registerShortcut(Key.ctrlShift("n"), {
		description: "Quick-select next story from active epic",
		handler: async (ctx) => {
			if (!ctx.hasUI) return;
			const epicID = getEpicOverride(getPi());
			if (!epicID) {
				ctx.ui.notify("No active epic — use /epic-select first", "warning");
				return;
			}
			try {
				const nextRes = await runRoomAgile(getPi(), { action: "epic_next", epic_id: epicID, limit: 200 }, ctx.signal);
				const n = (nextRes.result as Record<string, unknown>)?.data as Record<string, unknown> | undefined;
				const next = (n?.next || []) as Array<Record<string, unknown>>;
				if (next.length === 0) {
					ctx.ui.notify("No next actions available", "info");
					return;
				}
				const first = next[0]!;
				const storyID = String(first.story_id || first.id || "");
				if (storyID) {
					_flagOverrides["foxctl-story"] = storyID;
					ctx.ui.notify(`Next story: ${first.title || storyID}`, "info");
					await refreshEpicWidget(getPi(), ctx);
				} else {
					ctx.ui.notify(`Next action: ${first.action || "unknown"} — ${first.reason || ""}`, "info");
				}
			} catch (e) {
				ctx.ui.notify(`Next story failed: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("tasks", {
		description: "Show tasks for the configured foxctl room",
		handler: async (_args, ctx) => {
			const room = getRoom(getPi());
			if (!room) {
				ctx.ui.notify("Set --foxctl-room before listing room tasks", "warning");
				return;
			}
			try {
				const tasks = await getClient(getPi()).get<Record<string, unknown>>(
					`/api/rooms/${path(room)}/tasks${query({ workspace_id: getWorkspace(getPi()), limit: 200 })}`,
					ctx.signal,
				);
				ctx.ui.notify(`Room tasks: ${JSON.stringify(tasks).slice(0, 400)}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxctl room tasks failed: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("loop", {
		description: "Show loop status for the configured foxctl room",
		handler: async (_args, ctx) => {
			const room = getRoom(getPi());
			if (!room) {
				ctx.ui.notify("Set --foxctl-room before checking room loop", "warning");
				return;
			}
			try {
				const loop = await getClient(getPi()).get<Record<string, unknown>>(
					`/api/rooms/${path(room)}/loop${query({ workspace_id: getWorkspace(getPi()), actor_id: getActor(getPi()) })}`,
					ctx.signal,
				);
				ctx.ui.notify(`Room loop: ${JSON.stringify(loop).slice(0, 400)}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxctl room loop failed: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("foxctl-foxprox-sessions", {
		description: "Show foxprox sessions for the configured foxctl room",
		handler: async (_args, ctx) => {
			const room = getRoom(getPi());
			if (!room) {
				ctx.ui.notify("Set --foxctl-room before listing foxprox sessions", "warning");
				return;
			}
			try {
				const sessions = await getClient(getPi()).get<Record<string, unknown>>(
					`/api/foxprox/foxctl-rooms/${path(room)}/sessions${query({ workspace_id: getWorkspace(getPi()) })}`,
					ctx.signal,
				);
				ctx.ui.notify(`Foxprox sessions: ${JSON.stringify(sessions).slice(0, 300)}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxprox sessions failed: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("foxctl-tasks", {
		description: "List foxctl tasks",
		handler: async (_args, ctx) => {
			const client = getClient(getPi());
			try {
				const tasks = await client.get<{ tasks?: Array<{ id: string; title?: string }> }>("/api/tasks", ctx.signal);
				const list = tasks.tasks?.map((t) => t.title || t.id).join(", ") || "none";
				ctx.ui.notify(`Tasks: ${truncate(list, 200)}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxctl unreachable: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("foxctl-board", {
		description: "Show foxctl orchestration board",
		handler: async (_args, ctx) => {
			const client = getClient(getPi());
			try {
				const board = await client.get<Record<string, unknown>>("/api/orchestration/board-get", ctx.signal);
				ctx.ui.notify(`Board: ${JSON.stringify(board).slice(0, 200)}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxctl unreachable: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("foxctl-stats", {
		description: "Show foxctl stats",
		handler: async (_args, ctx) => {
			const client = getClient(getPi());
			try {
				const stats = await client.get<Record<string, unknown>>("/api/stats", ctx.signal);
				ctx.ui.notify(`Stats: ${JSON.stringify(stats).slice(0, 200)}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxctl unreachable: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("foxctl-context", {
		description: "Show foxctl context plane",
		handler: async (_args, ctx) => {
			await notifyGatherContext(ctx);
		},
	});

	// --- ContextWiki slash commands ---

	pi.registerCommand("context-show", {
		description: "Show the current ContextWiki top-of-mind bundle",
		handler: async (_args, ctx) => {
			try {
				const client = getClient(pi);
				const result = await client.get<Record<string, unknown>>(`/api/context/show${query({ workspace: getWorkspace(pi) })}`);
				ctx.ui.notify(JSON.stringify(result, null, 2), "info");
			} catch (e) {
				ctx.ui.notify(`context-show failed: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("context-report", {
		description: "Show synthesized ContextWiki workspace health report",
		handler: async (_args, ctx) => {
			try {
				const client = getClient(pi);
				const result = await client.get<Record<string, unknown>>(`/api/context/report${query({ workspace: getWorkspace(pi) })}`);
				ctx.ui.notify(JSON.stringify(result, null, 2), "info");
			} catch (e) {
				ctx.ui.notify(`context-report failed: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("context-next", {
		description: "Show the recommended next ContextWiki task",
		handler: async (_args, ctx) => {
			try {
				const client = getClient(pi);
				const result = await client.get<Record<string, unknown>>(`/api/context/next${query({ workspace: getWorkspace(pi) })}`);
				ctx.ui.notify(JSON.stringify(result, null, 2), "info");
			} catch (e) {
				ctx.ui.notify(`context-next failed: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("context-handoffs", {
		description: "Show recent ContextWiki handoffs",
		handler: async (_args, ctx) => {
			try {
				const client = getClient(pi);
				const result = await client.get<Record<string, unknown>>(`/api/context/handoffs${query({ workspace: getWorkspace(pi), limit: 10 })}`);
				ctx.ui.notify(JSON.stringify(result, null, 2), "info");
			} catch (e) {
				ctx.ui.notify(`context-handoffs failed: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("gather-context", {
		description: "Gather foxctl health, workspace context, room status, and inbox",
		handler: async (_args, ctx) => {
			await notifyGatherContext(ctx);
		},
	});

	pi.registerCommand("ctx", {
		description: "Alias for gather-context",
		handler: async (_args, ctx) => {
			await notifyGatherContext(ctx);
		},
	});

	pi.registerCommand("foxctl-mcp", {
		description: "Show foxctl MCP status",
		handler: async (_args, ctx) => {
			const client = getClient(getPi());
			try {
				const mcp = await client.get<Record<string, unknown>>("/api/mcp/status", ctx.signal);
				ctx.ui.notify(`MCP: ${JSON.stringify(mcp).slice(0, 200)}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxctl unreachable: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	pi.registerCommand("foxctl-workspaces", {
		description: "List foxctl workspaces",
		handler: async (_args, ctx) => {
			const client = getClient(getPi());
			try {
				const ws = await client.get<Record<string, unknown>>("/api/workspaces", ctx.signal);
				ctx.ui.notify(`Workspaces: ${JSON.stringify(ws).slice(0, 200)}`, "info");
			} catch (e) {
				ctx.ui.notify(`foxctl unreachable: ${e instanceof Error ? e.message : String(e)}`, "error");
			}
		},
	});

	// --- Event hooks ---
	pi.on("session_start", async (_event, ctx) => {
		startAutomaticMemoryDrafts(getPi(), ctx.signal);
		const room = getRoom(getPi());
		const client = getClient(getPi());
		if (room && isFlagEnabled(getPi(), "foxctl-room-bind")) {
			try {
				await bindPiToRoom(getPi(), room, ctx.signal);
			} catch (e) {
				ctx.ui.notify(`foxctl room bind failed: ${e instanceof Error ? e.message : String(e)}`, "warning");
			}
		}
		if (!isFlagEnabled(getPi(), "foxctl-ui-status")) return;
		try {
			const health = await client.get<{ data?: { version?: string; database_driver?: string } }>("/api/health", ctx.signal);
			const version = health.data?.version || "unknown";
			const db = health.data?.database_driver || "unknown";
			let statusText = room ? `foxctl ${version} ${db} room:${room}` : `foxctl ${version} ${db}`;
			const epicID = getEpicOverride(getPi());
			if (epicID) {
				try {
					const epicRes = await runRoomAgile(getPi(), { action: "epic_resume", epic_id: epicID, limit: 10 }, ctx.signal);
					const epicData = (epicRes.result as Record<string, unknown>)?.data as Record<string, unknown> | undefined;
					const epic = epicData?.epic as Record<string, unknown> | undefined;
					if (epic) {
						const short = (epic.title as string || epicID).slice(0, 30);
						statusText += ` epic:${short} (${epic.status || "?"})`;
					}
					const milestoneID = getMilestoneOverride(getPi());
					if (milestoneID && epicData?.milestones) {
						const milestones = epicData.milestones as Array<Record<string, unknown>>;
						const ms = milestones.find((m) => m.id === milestoneID);
						if (ms) statusText += ` ms:${(ms.title as string || milestoneID).slice(0, 20)}`;
					}
					const storyID = getStoryOverride(getPi());
					if (storyID && epicData?.stories) {
						const stories = epicData.stories as Array<Record<string, unknown>>;
						const st = stories.find((s) => s.id === storyID);
						if (st) statusText += ` story:${(st.title as string || storyID).slice(0, 20)} (${st.status || "?"})`;
					}
				} catch {
					statusText += ` epic:${epicID.slice(0, 12)}`;
				}
			}
			ctx.ui.setStatus("foxctl", statusText);

			// M5: Set up epic status widget above the editor
			const epicIDForWidget = getEpicOverride(getPi());
			if (epicIDForWidget) {
				try {
					const widgetData = await fetchEpicWidgetData(getPi(), ctx.signal);
					ctx.ui.setWidget("foxctl-epic", (tui, theme) => {
						const widget = new EpicStatusWidget(widgetData, theme);
						(getPi() as any).__foxctlEpicWidget = widget;
						return widget;
					});
				} catch {
					// Widget unavailable, non-fatal
				}
			}
		} catch (e) {
			ctx.ui.setStatus("foxctl", "foxctl offline");
			ctx.ui.notify(`foxctl unreachable: ${e instanceof Error ? e.message : String(e)}`, "warning");
		}
	});

	pi.on("before_agent_start", async (event, ctx) => {
		void maybeRunAutomaticMemoryDrafts(getPi(), ctx.signal, "before_agent_start");
		if (!isFlagEnabled(getPi(), "foxctl-context")) return;
		try {
			const snapshot = await getFoxctlSnapshot(getPi(), ctx.signal, event.prompt);
			return {
				message: {
					customType: "foxctl-context",
					content: formatFoxctlContext(snapshot),
					display: false,
					details: snapshot,
				},
			};
		} catch (e) {
			ctx.ui.notify(`foxctl context unavailable: ${e instanceof Error ? e.message : String(e)}`, "warning");
		}
	});
}
