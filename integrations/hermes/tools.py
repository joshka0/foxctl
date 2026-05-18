"""Tool registration for the foxctl plugin.

Registers all foxctl tools with the hermes tool registry so the agent
can call them during conversation.
"""

from __future__ import annotations

import json
import logging
from typing import Any, Callable, Dict, List, Optional

from .client import FoxctlClient, FoxctlError
from .config import FoxctlConfig

logger = logging.getLogger(__name__)

TOOLSET = "foxctl"


def _tool_error(msg: str) -> str:
    return json.dumps({"ok": False, "error": msg})


def _tool_ok(data: Any = None) -> str:
    return json.dumps({"ok": True, "data": data})


def _wrap(fn: Callable) -> Callable:
    """Wrap a tool handler to catch FoxctlError and return error JSON."""
    def wrapper(args: Dict, **kwargs) -> str:
        try:
            result = fn(args, **kwargs)
            if isinstance(result, str):
                return result
            return _tool_ok(result)
        except FoxctlError as e:
            return _tool_error(f"foxctl error {e.status}: {e.message}")
        except Exception as e:
            return _tool_error(f"foxctl: {e}")
    wrapper.__name__ = fn.__name__
    wrapper.__doc__ = fn.__doc__
    return wrapper


def register_tools(ctx, client: FoxctlClient, cfg: FoxctlConfig) -> None:
    """Register all foxctl tools with the hermes plugin context."""

    def check_foxctl_available() -> bool:
        try:
            client.health()
            return True
        except Exception:
            return False

    # -- Health -------------------------------------------------------------

    ctx.register_tool(
        name="foxctl_health",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_health",
            "description": "Check foxctl daemon health and version.",
            "parameters": {"type": "object", "properties": {}, "required": []},
        },
        handler=_wrap(lambda args, **kw: client.health()),
        check_fn=check_foxctl_available,
    )

    # -- Memory search ------------------------------------------------------

    ctx.register_tool(
        name="foxctl_memory_search",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_memory_search",
            "description": (
                "Search foxctl's memory store for relevant context. "
                "Returns ranked results from foxctl's vector-indexed knowledge base."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "query": {"type": "string", "description": "Search query"},
                    "limit": {"type": "integer", "description": "Max results (default 5)", "default": 5},
                    "include_content": {"type": "boolean", "description": "Include full content (default true)", "default": True},
                },
                "required": ["query"],
            },
        },
        handler=_wrap(lambda args, **kw: client.memory_search(
            query=args["query"],
            limit=args.get("limit", 5),
            include_content=args.get("include_content", True),
        )),
        check_fn=check_foxctl_available,
    )

    # -- Session recall -----------------------------------------------------

    ctx.register_tool(
        name="foxctl_session_recall",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_session_recall",
            "description": (
                "Recall relevant context from foxctl's session history. "
                "Searches past conversation sessions for related information."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "query": {"type": "string", "description": "Recall query"},
                    "limit": {"type": "integer", "description": "Max results (default 3)", "default": 3},
                },
                "required": ["query"],
            },
        },
        handler=_wrap(lambda args, **kw: client.session_recall(
            query=args["query"],
            limit=args.get("limit", 3),
        )),
        check_fn=check_foxctl_available,
    )

    # -- Context ------------------------------------------------------------

    ctx.register_tool(
        name="foxctl_context_overview",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_context_overview",
            "description": (
                "Get the ContextWiki overview: proposals, evidence imports, promotion jobs, "
                "and next merge tasks. Higher-level than context_show — focuses on "
                "the knowledge promotion pipeline rather than current work state."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "limit": {"type": "integer", "description": "Max items per category (default 8)", "default": 8},
                },
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.context_overview(limit=args.get("limit", 8))),
        check_fn=check_foxctl_available,
    )

    # -- Room messaging -----------------------------------------------------

    ctx.register_tool(
        name="foxctl_room_send",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_room_send",
            "description": "Send a message to the foxctl room.",
            "parameters": {
                "type": "object",
                "properties": {
                    "message": {"type": "string", "description": "Message text"},
                    "room_id": {"type": "string", "description": "Room ID (defaults to config room)"},
                },
                "required": ["message"],
            },
        },
        handler=_wrap(lambda args, **kw: client.room_send(
            message=args["message"],
            room_id=args.get("room_id"),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_room_inbox",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_room_inbox",
            "description": "Read the room inbox for this agent.",
            "parameters": {
                "type": "object",
                "properties": {
                    "room_id": {"type": "string", "description": "Room ID (defaults to config room)"},
                    "only": {"type": "string", "description": "Filter: pending, unread, acked, resolved, all", "default": "pending"},
                    "limit": {"type": "integer", "description": "Max messages (default 50)", "default": 50},
                },
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.room_inbox(
            room_id=args.get("room_id"),
            only=args.get("only", "pending"),
            limit=args.get("limit", 50),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_room_messages",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_room_messages",
            "description": "Read recent messages from the foxctl room.",
            "parameters": {
                "type": "object",
                "properties": {
                    "room_id": {"type": "string", "description": "Room ID (defaults to config room)"},
                    "limit": {"type": "integer", "description": "Max messages (default 50)", "default": 50},
                },
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.room_messages(
            room_id=args.get("room_id"),
            limit=args.get("limit", 50),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_room_message_ack",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_room_message_ack",
            "description": "Acknowledge a room message.",
            "parameters": {
                "type": "object",
                "properties": {
                    "message_id": {"type": "string", "description": "Message ID to acknowledge"},
                    "room_id": {"type": "string", "description": "Room ID (defaults to config room)"},
                },
                "required": ["message_id"],
            },
        },
        handler=_wrap(lambda args, **kw: client.room_message_ack(
            message_id=args["message_id"],
            room_id=args.get("room_id"),
        )),
        check_fn=check_foxctl_available,
    )

    # -- Epic reads ---------------------------------------------------------

    for action_name, desc in [
        ("epic_show", "Show epic details including milestones and stories."),
        ("epic_resume", "Get a summary of the epic state: status, milestones, stories, coverage."),
        ("epic_health", "Check epic health: warnings, blockers, attention needed."),
        ("epic_next", "Get the recommended next action for the epic."),
    ]:
        ctx.register_tool(
            name=f"foxctl_{action_name}",
            toolset=TOOLSET,
            schema={
                "name": f"foxctl_{action_name}",
                "description": desc,
                "parameters": {
                    "type": "object",
                    "properties": {
                        "epic_id": {"type": "string", "description": "Epic ID (defaults to config epic_id)"},
                    },
                    "required": [],
                },
            },
            handler=_wrap(lambda args, _an=action_name, **kw: getattr(client, _an)(epic_id=args.get("epic_id"))),
            check_fn=check_foxctl_available,
        )

    # -- Milestone reads ----------------------------------------------------

    ctx.register_tool(
        name="foxctl_milestone_show",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_milestone_show",
            "description": "Show milestone details including stories and coverage.",
            "parameters": {
                "type": "object",
                "properties": {
                    "milestone_id": {"type": "string", "description": "Milestone ID"},
                    "limit": {"type": "integer", "description": "Max items (default 100)", "default": 100},
                },
                "required": ["milestone_id"],
            },
        },
        handler=_wrap(lambda args, **kw: client.milestone_show(
            milestone_id=args["milestone_id"],
            limit=args.get("limit", 100),
        )),
        check_fn=check_foxctl_available,
    )

    # -- Story reads --------------------------------------------------------

    ctx.register_tool(
        name="foxctl_story_show",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_story_show",
            "description": "Show story details.",
            "parameters": {
                "type": "object",
                "properties": {
                    "story_id": {"type": "string", "description": "Story ID (empty for all stories)"},
                    "limit": {"type": "integer", "description": "Max items (default 100)", "default": 100},
                },
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.story_show(
            story_id=args.get("story_id", ""),
            limit=args.get("limit", 100),
        )),
        check_fn=check_foxctl_available,
    )

    # -- Story mutations ----------------------------------------------------

    ctx.register_tool(
        name="foxctl_story_start",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_story_start",
            "description": "Move a story to in_progress state.",
            "parameters": {
                "type": "object",
                "properties": {
                    "story_id": {"type": "string", "description": "Story ID to start"},
                },
                "required": ["story_id"],
            },
        },
        handler=_wrap(lambda args, **kw: client.story_start(story_id=args["story_id"])),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_story_review",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_story_review",
            "description": "Move a story to in_review state.",
            "parameters": {
                "type": "object",
                "properties": {
                    "story_id": {"type": "string", "description": "Story ID to review"},
                },
                "required": ["story_id"],
            },
        },
        handler=_wrap(lambda args, **kw: client.story_review(story_id=args["story_id"])),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_story_validate",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_story_validate",
            "description": "Validate a story with a verdict (pass/fail/waived).",
            "parameters": {
                "type": "object",
                "properties": {
                    "story_id": {"type": "string", "description": "Story ID to validate"},
                    "verdict": {"type": "string", "description": "Validation verdict: pass, fail, or waived", "default": "pass"},
                    "validator_type": {"type": "string", "description": "Validator type: agent, human, or harness", "default": "agent"},
                    "notes": {"type": "string", "description": "Validation notes", "default": ""},
                },
                "required": ["story_id"],
            },
        },
        handler=_wrap(lambda args, **kw: client.story_validate(
            story_id=args["story_id"],
            verdict=args.get("verdict", "pass"),
            validator_type=args.get("validator_type", "agent"),
            notes=args.get("notes", ""),
        )),
        check_fn=check_foxctl_available,
    )

    # ===================================================================
    # Deep Intelligence Layer — repo index, code search, text, filesystem
    # ===================================================================

    # -- Repo index --------------------------------------------------------

    ctx.register_tool(
        name="foxctl_repo_search",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_repo_search",
            "description": (
                "Search the foxctl repo index for symbols, files, packages. "
                "Returns matching nodes with signatures, docs, file paths. "
                "Best for finding functions, types, methods by natural-language query."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "query": {"type": "string", "description": "Natural-language or symbol name query"},
                    "limit": {"type": "integer", "description": "Max results (default 10)", "default": 10},
                },
                "required": ["query"],
            },
        },
        handler=_wrap(lambda args, **kw: client.repo_index_search(
            query=args["query"],
            limit=args.get("limit", 10),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_repo_dag",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_repo_dag",
            "description": (
                "Search repo index and return an explanation dependency DAG. "
                "Useful for understanding how components relate, call chains, "
                "and architectural dependencies. Returns nodes + edges."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "query": {"type": "string", "description": "Natural-language query to seed the DAG"},
                    "depth": {"type": "integer", "description": "Graph traversal depth (default 2)", "default": 2},
                    "budget": {"type": "integer", "description": "Max nodes in graph (default 50)", "default": 50},
                },
                "required": ["query"],
            },
        },
        handler=_wrap(lambda args, **kw: client.repo_index_dag(
            query=args["query"],
            depth=args.get("depth", 2),
            budget=args.get("budget", 50),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_repo_expand",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_repo_expand",
            "description": (
                "Expand the repo index graph from seed node IDs to discover "
                "neighbors, dependencies, and callers. Use after foxctl_repo_search "
                "to explore the graph around interesting nodes."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "seeds": {
                        "type": "array",
                        "items": {"type": "string"},
                        "description": "Seed node IDs to expand from",
                    },
                    "depth": {"type": "integer", "description": "Traversal depth (default 1)", "default": 1},
                    "budget": {"type": "integer", "description": "Max nodes (default 30)", "default": 30},
                },
                "required": ["seeds"],
            },
        },
        handler=_wrap(lambda args, **kw: client.repo_index_expand(
            seeds=args["seeds"],
            depth=args.get("depth", 1),
            budget=args.get("budget", 30),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_repo_open",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_repo_open",
            "description": "Open a repo index node by ID to get full metadata.",
            "parameters": {
                "type": "object",
                "properties": {
                    "node_id": {"type": "string", "description": "Node ID from repo_search or repo_expand"},
                },
                "required": ["node_id"],
            },
        },
        handler=_wrap(lambda args, **kw: client.repo_index_open(node_id=args["node_id"])),
        check_fn=check_foxctl_available,
    )

    # -- Code search --------------------------------------------------------

    ctx.register_tool(
        name="foxctl_code_grep",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_code_grep",
            "description": (
                "Search code patterns and return surrounding function/class blocks. "
                "Supports ripgrep (regex), ast (structural), and line modes. "
                "Returns expanded blocks with file context, not just matching lines."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "pattern": {"type": "string", "description": "Regex or literal pattern"},
                    "mode": {"type": "string", "description": "Search mode: ripgrep, ast, or line", "default": "ripgrep"},
                    "language": {"type": "string", "description": "Language filter (go, python, javascript, typescript)"},
                    "path": {"type": "string", "description": "File or directory to search"},
                    "max_blocks": {"type": "integer", "description": "Max code blocks to return (default 10)", "default": 10},
                },
                "required": ["pattern"],
            },
        },
        handler=_wrap(lambda args, **kw: client.code_context_grep(
            pattern=args["pattern"],
            mode=args.get("mode", "ripgrep"),
            language=args.get("language"),
            path=args.get("path"),
            max_blocks=args.get("max_blocks", 10),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_semantic_search",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_semantic_search",
            "description": (
                "Unified semantic search across code symbols, sessions, memory, and codemaps. "
                "Uses vector embeddings + BM25 hybrid search. "
                "Best for: 'how does X work', 'find code related to Y', 'where is Z implemented'."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "query": {"type": "string", "description": "Natural-language code query"},
                    "limit": {"type": "integer", "description": "Max results (default 10)", "default": 10},
                },
                "required": ["query"],
            },
        },
        handler=_wrap(lambda args, **kw: client.code_semantic_search(
            query=args["query"],
            limit=args.get("limit", 10),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_code_symbols",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_code_symbols",
            "description": (
                "Extract code symbols (functions, types, interfaces, methods) from a file. "
                "Use to understand the API surface of a file before reading it."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string", "description": "File path relative to workspace root"},
                },
                "required": ["path"],
            },
        },
        handler=_wrap(lambda args, **kw: client.code_symbols(path=args["path"])),
        check_fn=check_foxctl_available,
    )

    # -- Text search --------------------------------------------------------

    ctx.register_tool(
        name="foxctl_text_grep",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_text_grep",
            "description": (
                "Fast regex search across the workspace. "
                "Returns matching lines with file and line number context. "
                "Supports include/exclude globs and case-insensitive mode."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "pattern": {"type": "string", "description": "Go RE2 regex pattern"},
                    "include": {
                        "type": "array",
                        "items": {"type": "string"},
                        "description": "Include globs (e.g. [\"*.go\"])",
                    },
                    "exclude": {
                        "type": "array",
                        "items": {"type": "string"},
                        "description": "Exclude globs",
                    },
                    "path": {"type": "string", "description": "Directory or file to search"},
                    "max_matches": {"type": "integer", "description": "Max matches (default 50)", "default": 50},
                    "ci": {"type": "boolean", "description": "Case-insensitive (default false)", "default": False},
                },
                "required": ["pattern"],
            },
        },
        handler=_wrap(lambda args, **kw: client.text_grep(
            pattern=args["pattern"],
            include=args.get("include"),
            exclude=args.get("exclude"),
            path=args.get("path"),
            max_matches=args.get("max_matches", 50),
            ci=args.get("ci", False),
        )),
        check_fn=check_foxctl_available,
    )

    # -- Filesystem ---------------------------------------------------------

    ctx.register_tool(
        name="foxctl_fs_read",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_fs_read",
            "description": (
                "Read file contents from the workspace through foxctl's CAS-backed "
                "storage. Returns content with metadata. Use for reading source files."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string", "description": "File path relative to workspace root"},
                    "max_bytes": {"type": "integer", "description": "Max bytes to read (default 50000)", "default": 50000},
                },
                "required": ["path"],
            },
        },
        handler=_wrap(lambda args, **kw: client.fs_read(
            path=args["path"],
            max_bytes=args.get("max_bytes", 50000),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_fs_find",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_fs_find",
            "description": (
                "Find files by name, path, or glob pattern. "
                "Supports fuzzy matching and ranked results."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "query": {"type": "string", "description": "Fuzzy filename/path query"},
                    "pattern": {"type": "string", "description": "Glob pattern (e.g. *.go)"},
                    "path": {"type": "string", "description": "Starting directory (default .)", "default": "."},
                    "max_results": {"type": "integer", "description": "Max results (default 20)", "default": 20},
                },
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.fs_find(
            query=args.get("query"),
            pattern=args.get("pattern"),
            path=args.get("path", "."),
            max_results=args.get("max_results", 20),
        )),
        check_fn=check_foxctl_available,
    )

    # -- Codemaps -----------------------------------------------------------

    ctx.register_tool(
        name="foxctl_codemap_list",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_codemap_list",
            "description": "List available codemaps (semantic code maps) for the workspace.",
            "parameters": {
                "type": "object",
                "properties": {
                    "limit": {"type": "integer", "description": "Max results (default 10)", "default": 10},
                },
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.codemap_list(limit=args.get("limit", 10))),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_codemap_get",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_codemap_get",
            "description": "Get a codemap by ID with full content (traces, files, description).",
            "parameters": {
                "type": "object",
                "properties": {
                    "codemap_id": {"type": "string", "description": "Codemap ID"},
                },
                "required": ["codemap_id"],
            },
        },
        handler=_wrap(lambda args, **kw: client.codemap_get(codemap_id=args["codemap_id"])),
        check_fn=check_foxctl_available,
    )

    # ===================================================================
    # Memory Write Layer — store, promote, curate
    # ===================================================================

    ctx.register_tool(
        name="foxctl_memory_put",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_memory_put",
            "description": (
                "Store a knowledge record in foxctl's memory store. "
                "Use this to persist learnings, decisions, architectural notes, "
                "gotchas, or any cross-agent knowledge that should be searchable "
                "by other agents (Pi, future Hermes sessions, etc.)."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "name": {
                        "type": "string",
                        "description": "Unique memory name (e.g. 'auth-architecture', 'gotcha-turso-conn-pool')",
                    },
                    "content": {
                        "type": "string",
                        "description": "Full content of the memory record",
                    },
                    "summary": {
                        "type": "string",
                        "description": "Short summary (used for search/BM25 ranking)",
                    },
                    "kind": {
                        "type": "string",
                        "description": "Memory kind: knowledge, decision, gotcha, preference, pattern",
                        "default": "knowledge",
                    },
                    "tags": {
                        "type": "array",
                        "items": {"type": "string"},
                        "description": "Tags for categorization",
                    },
                    "file_refs": {
                        "type": "array",
                        "items": {"type": "string"},
                        "description": "Related file paths",
                    },
                },
                "required": ["name", "content", "summary"],
            },
        },
        handler=_wrap(lambda args, **kw: client.memory_put(
            name=args["name"],
            content=args["content"],
            summary=args["summary"],
            kind=args.get("kind", "knowledge"),
            tags=args.get("tags", []),
            file_refs=args.get("file_refs", []),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_memory_curator",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_memory_curator",
            "description": (
                "Run a deterministic curator report on the memory store. "
                "Identifies stale, duplicate, or low-quality records. "
                "Use dry_run mode to preview changes before applying."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "mode": {
                        "type": "string",
                        "description": "dry_run (preview) or apply (mutate)",
                        "default": "dry_run",
                        "enum": ["dry_run", "apply"],
                    },
                    "limit": {"type": "integer", "description": "Max records to examine (default 100)", "default": 100},
                },
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.memory_curator(
            mode=args.get("mode", "dry_run"),
            limit=args.get("limit", 100),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_session_extract_learnings",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_session_extract_learnings",
            "description": (
                "Extract actionable learnings (gotchas, decisions, preferences, "
                "anti-patterns) from a session and store them as memory records. "
                "Call this at the end of a session to persist knowledge."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "session_id": {"type": "string", "description": "Session ID to extract from"},
                    "dry_run": {"type": "boolean", "description": "Preview only, don't persist (default false)", "default": False},
                },
                "required": ["session_id"],
            },
        },
        handler=_wrap(lambda args, **kw: client.session_extract_learnings(
            session_id=args["session_id"],
            dry_run=args.get("dry_run", False),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_embedding_flush",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_embedding_flush",
            "description": (
                "Process queued embedding jobs. Call this periodically to "
                "ensure newly indexed symbols and memories get their "
                "vector embeddings generated for semantic search."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "batch_size": {"type": "integer", "description": "Jobs per batch (default 50)", "default": 50},
                    "max_duration": {"type": "integer", "description": "Max seconds (default 120)", "default": 120},
                },
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.embedding_flush(
            batch_size=args.get("batch_size", 50),
            max_duration=args.get("max_duration", 120),
        )),
        check_fn=check_foxctl_available,
    )

    # ===================================================================
    # ContextWiki Integration — observations, tensions, handoffs, retrieve
    # ===================================================================

    ctx.register_tool(
        name="foxctl_context_show",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_context_show",
            "description": (
                "Show the current ContextWiki top-of-mind bundle for the workspace. "
                "Returns the workspace orientation: objective, phase, active tasks, "
                "hard constraints, next actions, observations, tensions, and handoffs. "
                "Use this at the START of a session to understand current workspace state."
            ),
            "parameters": {
                "type": "object",
                "properties": {},
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.context_show()),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_context_report",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_context_report",
            "description": (
                "Build a synthesized ContextWiki current-state report. "
                "Combines top-of-mind state with recommended actions. "
                "Use for a broader workspace health check."
            ),
            "parameters": {
                "type": "object",
                "properties": {},
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.context_report()),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_context_observe",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_context_observe",
            "description": (
                "Record an observation into the ContextWiki control plane. "
                "Observations capture repeatable system learnings — things that "
                "are true about the codebase, architecture, or process. "
                "They accumulate over time with confidence scores. "
                "Use when you discover something worth remembering."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "statement": {
                        "type": "string",
                        "description": "The observation statement (factual, repeatable)",
                    },
                    "confidence": {
                        "type": "number",
                        "description": "Confidence from 0.0 to 1.0 (default 0.7)",
                        "default": 0.7,
                    },
                    "project": {
                        "type": "string",
                        "description": "Project name",
                    },
                    "area": {
                        "type": "string",
                        "description": "Subsystem or area (e.g. 'memory', 'auth', 'api')",
                    },
                    "evidence_refs": {
                        "type": "array",
                        "items": {"type": "string"},
                        "description": "Evidence references (file paths, URLs, task IDs)",
                    },
                },
                "required": ["statement"],
            },
        },
        handler=_wrap(lambda args, **kw: client.context_observe(
            statement=args["statement"],
            confidence=args.get("confidence", 0.7),
            project=args.get("project"),
            area=args.get("area"),
            evidence_refs=args.get("evidence_refs"),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_context_tension",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_context_tension",
            "description": (
                "Record a tension into the ContextWiki control plane. "
                "Tensions capture contradictions, drag sources, and areas of "
                "friction that slow down work. They track kind (performance, "
                "contradiction, complexity, etc.) and impact level. "
                "Use when you identify something that should be fixed."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "statement": {
                        "type": "string",
                        "description": "The tension statement",
                    },
                    "kind": {
                        "type": "string",
                        "description": "Tension kind: contradiction, performance, complexity, dependency, usability",
                        "default": "contradiction",
                    },
                    "impact": {
                        "type": "string",
                        "description": "Impact level: low, medium, high",
                        "default": "medium",
                    },
                    "status": {
                        "type": "string",
                        "description": "Status: open, investigating, resolved",
                        "default": "open",
                    },
                    "related_refs": {
                        "type": "array",
                        "items": {"type": "string"},
                        "description": "Related references",
                    },
                },
                "required": ["statement"],
            },
        },
        handler=_wrap(lambda args, **kw: client.context_tension(
            statement=args["statement"],
            kind=args.get("kind", "contradiction"),
            impact=args.get("impact", "medium"),
            status=args.get("status", "open"),
            related_refs=args.get("related_refs"),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_context_capture",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_context_capture",
            "description": (
                "Capture a structured handoff into the ContextWiki control plane. "
                "Handoffs are the primary way agents communicate work continuity "
                "across sessions. Record: what was done (summary), what was "
                "observed, what tensions exist, and what should happen next. "
                "Call this when finishing a work phase or ending a session."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "summary": {
                        "type": "string",
                        "description": "Compact handoff summary of what was accomplished",
                    },
                    "task_id": {
                        "type": "string",
                        "description": "Task identifier",
                    },
                    "phase": {
                        "type": "string",
                        "description": "Phase name (research, implementation, review, etc.)",
                        "default": "work",
                    },
                    "outcome": {
                        "type": "string",
                        "description": "Outcome: partial, complete, blocked",
                        "default": "partial",
                    },
                    "observations": {
                        "type": "array",
                        "items": {"type": "string"},
                        "description": "Observations made during this work phase",
                    },
                    "tensions": {
                        "type": "array",
                        "items": {"type": "string"},
                        "description": "Tensions encountered during this work phase",
                    },
                    "next_actions": {
                        "type": "array",
                        "items": {"type": "string"},
                        "description": "Recommended next actions for whoever picks up this work",
                    },
                    "file_touched": {
                        "type": "array",
                        "items": {"type": "string"},
                        "description": "Files touched during this work phase",
                    },
                    "evidence_refs": {
                        "type": "array",
                        "items": {"type": "string"},
                        "description": "Evidence references",
                    },
                },
                "required": ["summary"],
            },
        },
        handler=_wrap(lambda args, **kw: client.context_capture(
            summary=args["summary"],
            task_id=args.get("task_id"),
            phase=args.get("phase", "work"),
            outcome=args.get("outcome", "partial"),
            observations=args.get("observations"),
            tensions=args.get("tensions"),
            next_actions=args.get("next_actions"),
            file_touched=args.get("file_touched"),
            evidence_refs=args.get("evidence_refs"),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_context_infer",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_context_infer",
            "description": (
                "Infer ContextWiki observations and tensions from a summary text. "
                "Analyzes the summary and extracts structured observations and "
                "tensions automatically. Use to batch-extract knowledge from "
                "session summaries or research notes."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "summary": {
                        "type": "string",
                        "description": "Compact summary text to analyze for observations/tensions",
                    },
                    "apply": {
                        "type": "boolean",
                        "description": "Persist the inferred items (default false = dry run)",
                        "default": False,
                    },
                    "project": {
                        "type": "string",
                        "description": "Project name override",
                    },
                    "area": {
                        "type": "string",
                        "description": "Area name override",
                    },
                },
                "required": ["summary"],
            },
        },
        handler=_wrap(lambda args, **kw: client.context_infer(
            summary=args["summary"],
            apply=args.get("apply", False),
            project=args.get("project"),
            area=args.get("area"),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_context_handoffs",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_context_handoffs",
            "description": (
                "List recorded ContextWiki handoffs — structured work continuity "
                "records from previous sessions. Each handoff captures what was "
                "done, what was observed, and what should happen next."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "limit": {
                        "type": "integer",
                        "description": "Max handoffs to return (default 10)",
                        "default": 10,
                    },
                },
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.context_handoffs(
            limit=args.get("limit", 10),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_context_dispatch",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_context_dispatch",
            "description": (
                "Build a bounded ContextWiki task packet for the next task. "
                "Returns a work packet with task details, context, and ready-to-go "
                "instructions. Use to find the next thing to work on."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "task_id": {
                        "type": "string",
                        "description": "Explicit task ID (defaults to context next)",
                    },
                },
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.context_dispatch(
            task_id=args.get("task_id"),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_context_next",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_context_next",
            "description": (
                "Select the next ContextWiki task candidate from the workspace. "
                "Returns the recommended next task based on current workspace state, "
                "priorities, and open loops."
            ),
            "parameters": {
                "type": "object",
                "properties": {},
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.context_next()),
        check_fn=check_foxctl_available,
    )

    # ===================================================================
    # Obsidian Vault / Knowledge Plane
    # ===================================================================

    ctx.register_tool(
        name="foxctl_vault_search",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_vault_search",
            "description": (
                "Search the Obsidian vault index for matching notes. "
                "Returns ranked hits with title, path, type, status, trust level, and snippet. "
                "Use to find existing knowledge about architecture, patterns, decisions."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "query": {"type": "string", "description": "Search query"},
                    "limit": {"type": "integer", "description": "Max results (default 20)", "default": 20},
                },
                "required": ["query"],
            },
        },
        handler=_wrap(lambda args, **kw: client.vault_search(
            query=args["query"],
            limit=args.get("limit", 20),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_vault_stats",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_vault_stats",
            "description": (
                "Get Obsidian vault index statistics: notes, headings, links, "
                "chunks, semantic embeddings. Use to check vault health."
            ),
            "parameters": {
                "type": "object",
                "properties": {},
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.vault_stats()),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_vault_promote",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_vault_promote",
            "description": (
                "Create an evergreen promotion draft in the vault inbox. "
                "This is the primary way to write to the knowledge plane — "
                "draft notes that capture architectural decisions, patterns, "
                "investigation findings, or methodology notes. "
                "Drafts land in inbox/drafted-from-foxctl/ for review."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "slug": {
                        "type": "string",
                        "description": "URL-safe slug for the note (e.g. 'memory-search-architecture')",
                    },
                    "content": {
                        "type": "string",
                        "description": "Full markdown content of the note",
                    },
                },
                "required": ["slug", "content"],
            },
        },
        handler=_wrap(lambda args, **kw: client.vault_promote(
            slug=args["slug"],
            content=args["content"],
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_vault_append",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_vault_append",
            "description": (
                "Append content under a specific heading in an existing vault note. "
                "Use to add findings, patterns, or evidence to an established note."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {
                        "type": "string",
                        "description": "Vault note path (e.g. 'inbox/drafted-from-foxctl/my-note.md')",
                    },
                    "heading": {
                        "type": "string",
                        "description": "Heading to append under",
                    },
                    "content": {
                        "type": "string",
                        "description": "Markdown content to append",
                    },
                },
                "required": ["path", "heading", "content"],
            },
        },
        handler=_wrap(lambda args, **kw: client.vault_append(
            path=args["path"],
            heading=args["heading"],
            content=args["content"],
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_vault_bridge",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_vault_bridge",
            "description": (
                "Reconcile repo docs with vault notes. Scans repo markdown docs/ "
                "and compares with vault notes that have repo_docs backlinks. "
                "Generates bridge drafts suggesting links between the two."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "docs_root": {
                        "type": "string",
                        "description": "Repo docs root (default: <workspace>/docs)",
                    },
                },
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.vault_bridge(
            docs_root=args.get("docs_root"),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_vault_graph",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_vault_graph",
            "description": (
                "Generate a repo graph draft bundle in the vault inbox. "
                "Creates structured vault notes from the repo index graph, "
                "mapping packages, symbols, and relationships into the knowledge plane."
            ),
            "parameters": {
                "type": "object",
                "properties": {},
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.vault_graph_build()),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_vault_index_build",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_vault_index_build",
            "description": (
                "Rebuild the local Obsidian vault index. Call this after adding "
                "new notes or when search results seem stale."
            ),
            "parameters": {
                "type": "object",
                "properties": {},
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.vault_index_build()),
        check_fn=check_foxctl_available,
    )

    # ===================================================================
    # Multi-Agent Coordination — tasks, agents, context broadcasting
    # ===================================================================

    ctx.register_tool(
        name="foxctl_agent_list",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_agent_list",
            "description": (
                "List all registered foxctl agents. Returns agent IDs, names, roles, "
                "states, and execution modes. Use to discover other agents in the workspace."
            ),
            "parameters": {
                "type": "object",
                "properties": {},
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.agent_list()),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_room_task_list",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_room_task_list",
            "description": (
                "List tasks associated with the room. Shows task IDs, titles, statuses, "
                "assignees, and dependencies. Use to find available work or track progress."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "status": {
                        "type": "string",
                        "description": "Filter by status: pending, in_progress, blocked, completed, failed",
                    },
                },
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.room_task_list(status=args.get("status", ""))),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_room_task_add",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_room_task_add",
            "description": (
                "Create a task in the room and announce it to all participants. "
                "Use to break work into trackable units that agents can claim."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "title": {"type": "string", "description": "Task title"},
                    "description": {"type": "string", "description": "Task description"},
                    "scope": {"type": "string", "description": "Scope path (e.g. 'src/memory/')"},
                    "depends_on": {
                        "type": "array",
                        "items": {"type": "string"},
                        "description": "Task IDs this task depends on",
                    },
                },
                "required": ["title"],
            },
        },
        handler=_wrap(lambda args, **kw: client.room_task_add(
            title=args["title"],
            description=args.get("description", ""),
            scope=args.get("scope", ""),
            depends_on=args.get("depends_on"),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_room_task_claim",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_room_task_claim",
            "description": (
                "Claim a pending room task for yourself. Moves the task to in-progress "
                "and signals other agents that it's taken."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "task_id": {"type": "string", "description": "Task ID to claim"},
                },
                "required": ["task_id"],
            },
        },
        handler=_wrap(lambda args, **kw: client.room_task_claim(task_id=args["task_id"])),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_room_task_complete",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_room_task_complete",
            "description": (
                "Complete a room task and broadcast the completion to all participants. "
                "Use when you've finished work on a claimed task."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "task_id": {"type": "string", "description": "Task ID to complete"},
                    "notes": {"type": "string", "description": "Completion notes (what was done, gotchas)"},
                },
                "required": ["task_id"],
            },
        },
        handler=_wrap(lambda args, **kw: client.room_task_complete(
            task_id=args["task_id"],
            notes=args.get("notes", ""),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_room_task_block",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_room_task_block",
            "description": (
                "Mark a claimed task as blocked. Signals that you can't proceed "
                "and need help or a dependency resolved."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "task_id": {"type": "string", "description": "Task ID to block"},
                    "reason": {"type": "string", "description": "Why the task is blocked"},
                },
                "required": ["task_id"],
            },
        },
        handler=_wrap(lambda args, **kw: client.room_task_block(
            task_id=args["task_id"],
            reason=args.get("reason", ""),
        )),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_room_task_abandon",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_room_task_abandon",
            "description": (
                "Release a claimed task back to pending. Other agents can then claim it. "
                "Use when you can't complete a task and want someone else to pick it up."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "task_id": {"type": "string", "description": "Task ID to abandon"},
                },
                "required": ["task_id"],
            },
        },
        handler=_wrap(lambda args, **kw: client.room_task_abandon(task_id=args["task_id"])),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_room_status",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_room_status",
            "description": (
                "Get the room status: participants, their roles, task pulse, "
                "backlog summary, and action-required counts. Use to understand "
                "the current coordination state of the room."
            ),
            "parameters": {
                "type": "object",
                "properties": {},
                "required": [],
            },
        },
        handler=_wrap(lambda args, **kw: client.room_status()),
        check_fn=check_foxctl_available,
    )

    ctx.register_tool(
        name="foxctl_publish_context",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_publish_context",
            "description": (
                "Publish your current context to the room for other agents to read. "
                "Include what you're working on, what you've learned, and what you need. "
                "Other agents can read this through room messages."
            ),
            "parameters": {
                "type": "object",
                "properties": {
                    "context": {
                        "type": "string",
                        "description": "Structured context to broadcast (markdown): current task, findings, blockers, needs",
                    },
                },
                "required": ["context"],
            },
        },
        handler=_wrap(lambda args, **kw: client.publish_agent_context(context=args["context"])),
        check_fn=check_foxctl_available,
    )

    logger.info("Registered %d foxctl tools", 58)
