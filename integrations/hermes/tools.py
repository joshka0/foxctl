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
        name="foxctl_context",
        toolset=TOOLSET,
        schema={
            "name": "foxctl_context",
            "description": (
                "Gather foxctl workspace context: health, rooms, tasks, and active jobs. "
                "Use this to understand the current foxctl state before acting."
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

    logger.info("Registered %d foxctl tools", 32)
