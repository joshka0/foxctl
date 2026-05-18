"""HTTP client for the foxctl daemon API.

Thin wrapper around ``requests`` (or ``urllib`` as fallback) that maps
foxctl REST endpoints to Python methods.
"""

from __future__ import annotations

import json
import logging
import os
from typing import Any, Dict, List, Optional
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode
from urllib.request import Request, urlopen

from .config import FoxctlConfig

logger = logging.getLogger(__name__)


class FoxctlClient:
    """Synchronous HTTP client for the foxctl daemon."""

    def __init__(self, cfg: FoxctlConfig):
        self.cfg = cfg
        self.base_url = cfg.url.rstrip("/")

    # -- internal helpers ---------------------------------------------------

    @staticmethod
    def _unwrap_skill(resp: Dict) -> Dict:
        """Unwrap a foxctl skill envelope (ok/output/data) into the inner data."""
        if "output" in resp and isinstance(resp["output"], dict):
            inner = resp["output"]
            if "data" in inner and isinstance(inner["data"], dict):
                return inner["data"]
            return inner
        return resp

    def _request(self, method: str, path: str, body: Optional[Dict] = None) -> Dict:
        url = f"{self.base_url}{path}"
        data = json.dumps(body).encode() if body else None
        headers = {"Content-Type": "application/json"} if body else {}
        req = Request(url, data=data, headers=headers, method=method)

        try:
            with urlopen(req, timeout=30) as resp:
                text = resp.read().decode()
                ct = resp.headers.get("content-type", "")
                if "application/json" in ct and text:
                    return json.loads(text)
                return {"raw": text}
        except HTTPError as e:
            text = e.read().decode() if e.fp else ""
            try:
                err = json.loads(text) if text else {}
            except json.JSONDecodeError:
                err = {"error": text}
            raise FoxctlError(
                e.code,
                err.get("error", {}).get("message", text or e.reason),
                err,
            ) from e
        except URLError as e:
            raise FoxctlError(0, f"Cannot reach foxctl at {self.base_url}: {e.reason}", {}) from e

    def _get(self, path: str, params: Optional[Dict] = None) -> Dict:
        if params:
            path = f"{path}?{urlencode({k: v for k, v in params.items() if v is not None})}"
        return self._request("GET", path)

    def _post(self, path: str, body: Optional[Dict] = None) -> Dict:
        return self._request("POST", path, body)

    def _skill(self, skill_name: str, **kwargs) -> Dict:
        """Call a foxctl skill by name with workspace auto-injected.

        Uses /api/skills/run which works for all skills (not just
        those with openapi enabled in their manifest).
        """
        body = {
            "skill": skill_name,
            "input": {"workspace": self.cfg.workspace, **{k: v for k, v in kwargs.items() if v is not None}},
        }
        resp = self._post("/api/skills/run", body)
        # /api/skills/run wraps differently: {ok, output: {data, status, ...}}
        if resp.get("ok"):
            output = resp.get("output", {})
            if isinstance(output, dict) and "data" in output:
                return output["data"]
            return output
        return resp

    def _put(self, path: str, body: Optional[Dict] = None) -> Dict:
        return self._request("PUT", path, body)

    def _agile(self, action: str, **kwargs) -> Dict:
        """Call the room-agile unified endpoint."""
        room = kwargs.pop("room_id", None) or self.cfg.room
        if not room:
            raise FoxctlError(400, "room_id required or set FOXCTL_ROOM", {})
        body = {
            "workspace": self.cfg.workspace,
            "action": action,
            "actor": self.cfg.actor,
            **kwargs,
        }
        return self._post(f"/api/rooms/{room}/agile", body)

    def _query(self, **extra) -> Dict:
        """Common query params for workspace-scoped endpoints."""
        return {"workspace_id": self.cfg.workspace, **extra}

    # -- health -------------------------------------------------------------

    def health(self) -> Dict:
        return self._get("/api/health")

    # -- context / overview -------------------------------------------------

    def context_overview(self, limit: int = 8) -> Dict:
        return self._get("/api/context/overview", self._query(limit=limit))

    # -- ContextWiki CLI-backed commands -----------------------------------

    def _cli(self, *args: str, timeout: int = 15) -> Dict:
        """Run a foxctl CLI command and parse the JSON envelope."""
        import subprocess as _sp

        cli_paths = ["/home/dev/repos/foxctl/bin/foxctl", "foxctl"]
        last_err = None
        for cli in cli_paths:
            try:
                result = _sp.run(
                    [cli, *args],
                    capture_output=True, text=True, timeout=timeout,
                )
                # Parse last JSON line from stdout (even on non-zero exit)
                for line in reversed(result.stdout.strip().splitlines()):
                    try:
                        d = json.loads(line)
                        if isinstance(d, dict):
                            return d
                    except json.JSONDecodeError:
                        continue
                # If no JSON on stdout but we have stderr, check for known benign errors
                stderr_text = (result.stderr or "").strip()
                if result.returncode != 0 and stderr_text:
                    # Some commands return useful info on stderr (e.g. "no eligible task found")
                    if "no eligible task" in stderr_text or "no such file" in stderr_text.lower():
                        return {"ok": True, "data": {"message": stderr_text, "found": False}}
                    raise FoxctlError(result.returncode, stderr_text[:200], {})
                if result.stdout.strip():
                    return {"ok": True, "raw": result.stdout}
                return {"ok": True, "data": {}}
            except FileNotFoundError:
                last_err = FileNotFoundError
                continue
            except _sp.TimeoutExpired:
                raise FoxctlError(408, f"CLI timed out after {timeout}s", {})
        raise FoxctlError(404, "foxctl binary not found", {})

    def context_show(self) -> Dict:
        """Show the current ContextWiki top-of-mind bundle."""
        resp = self._cli("context", "show", "--workspace", self.cfg.workspace)
        return resp.get("data", resp)

    def context_report(self) -> Dict:
        """Build a synthesized ContextWiki current-state report."""
        resp = self._cli("context", "report", "--workspace", self.cfg.workspace)
        return resp.get("data", resp)

    def context_observations(self, limit: int = 50) -> Dict:
        """List recorded ContextWiki observations."""
        resp = self._cli("context", "observations", "--workspace", self.cfg.workspace)
        return resp.get("data", resp)

    def context_tensions(self, limit: int = 50) -> Dict:
        """List recorded ContextWiki tensions."""
        resp = self._cli("context", "tensions", "--workspace", self.cfg.workspace)
        return resp.get("data", resp)

    def context_handoffs(self, limit: int = 20) -> Dict:
        """List recorded ContextWiki handoffs."""
        resp = self._cli("context", "handoffs", "--workspace", self.cfg.workspace)
        return resp.get("data", resp)

    def context_observe(
        self,
        statement: str,
        confidence: float = 0.7,
        project: Optional[str] = None,
        area: Optional[str] = None,
        evidence_refs: Optional[List[str]] = None,
    ) -> Dict:
        """Record an observation into the ContextWiki control plane."""
        args = [
            "observe", "--workspace", self.cfg.workspace,
            "--statement", statement,
            "--confidence", str(confidence),
        ]
        if project:
            args.extend(["--project", project])
        if area:
            args.extend(["--area", area])
        for ref in (evidence_refs or []):
            args.extend(["--evidence-ref", ref])
        resp = self._cli(*args)
        return resp.get("data", resp)

    def context_tension(
        self,
        statement: str,
        kind: str = "contradiction",
        impact: str = "medium",
        status: str = "open",
        related_refs: Optional[List[str]] = None,
    ) -> Dict:
        """Record a tension into the ContextWiki control plane."""
        args = [
            "tension", "--workspace", self.cfg.workspace,
            "--statement", statement,
            "--kind", kind,
            "--impact", impact,
            "--status", status,
        ]
        for ref in (related_refs or []):
            args.extend(["--related-ref", ref])
        resp = self._cli(*args)
        return resp.get("data", resp)

    def context_capture(
        self,
        summary: str,
        task_id: Optional[str] = None,
        phase: str = "work",
        outcome: str = "partial",
        observations: Optional[List[str]] = None,
        tensions: Optional[List[str]] = None,
        next_actions: Optional[List[str]] = None,
        file_touched: Optional[List[str]] = None,
        evidence_refs: Optional[List[str]] = None,
    ) -> Dict:
        """Capture a structured handoff into the ContextWiki control plane.

        Handoffs are the primary way agents communicate work continuity.
        They record: what was done, what was observed, what's next.
        """
        args = [
            "capture", "--workspace", self.cfg.workspace,
            "--summary", summary,
            "--phase", phase,
            "--outcome", outcome,
        ]
        if task_id:
            args.extend(["--task-id", task_id])
        for o in (observations or []):
            args.extend(["--observation", o])
        for t in (tensions or []):
            args.extend(["--tension", t])
        for n in (next_actions or []):
            args.extend(["--next-action", n])
        for f in (file_touched or []):
            args.extend(["--file-touched", f])
        for r in (evidence_refs or []):
            args.extend(["--evidence-ref", r])
        resp = self._cli(*args)
        return resp.get("data", resp)

    def context_infer(
        self,
        summary: str,
        apply: bool = False,
        project: Optional[str] = None,
        area: Optional[str] = None,
        evidence_refs: Optional[List[str]] = None,
    ) -> Dict:
        """Infer ContextWiki observations and tensions from a summary.

        Analyzes the summary text and extracts structured observations
        and tensions. Use dry-run (apply=False) to preview, then apply.
        """
        args = [
            "context", "infer", "--workspace", self.cfg.workspace,
            "--summary", summary,
        ]
        if apply:
            args.append("--apply")
        if project:
            args.extend(["--project", project])
        if area:
            args.extend(["--area", area])
        for r in (evidence_refs or []):
            args.extend(["--evidence-ref", r])
        resp = self._cli(*args)
        return resp.get("data", resp)

    def context_dispatch(self, task_id: Optional[str] = None) -> Dict:
        """Build a bounded task packet for the next or selected task."""
        args = ["context", "dispatch", "--workspace", self.cfg.workspace]
        if task_id:
            args.extend(["--task-id", task_id])
        resp = self._cli(*args)
        return resp.get("data", resp)

    def context_next(self) -> Dict:
        """Select the next ContextWiki task candidate."""
        resp = self._cli("context", "next", "--workspace", self.cfg.workspace)
        return resp.get("data", resp)

    # -- Obsidian Vault / Knowledge Plane ----------------------------

    def vault_search(self, query: str, vault_path: Optional[str] = None, limit: int = 20) -> Dict:
        """Search the Obsidian vault index for matching notes."""
        args = [
            "obsidian", "index", "search",
            "--query", query,
            "--limit", str(limit),
        ]
        vp = vault_path or self.cfg.vault_path
        if vp:
            args.extend(["--vault-path", vp])
        resp = self._cli(*args)
        return resp.get("data", resp)

    def vault_stats(self, vault_path: Optional[str] = None) -> Dict:
        """Get vault index stats (notes, headings, links, chunks)."""
        args = ["obsidian", "index", "stats"]
        vp = vault_path or self.cfg.vault_path
        if vp:
            args.extend(["--vault-path", vp])
        resp = self._cli(*args)
        return resp.get("data", resp)

    def vault_index_build(self, vault_path: Optional[str] = None) -> Dict:
        """Rebuild the local Obsidian vault index."""
        args = ["obsidian", "index", "build"]
        vp = vault_path or self.cfg.vault_path
        if vp:
            args.extend(["--vault-path", vp])
        resp = self._cli(*args)
        return resp.get("data", resp)

    def vault_promote(
        self,
        slug: str,
        content: str,
        vault_path: Optional[str] = None,
    ) -> Dict:
        """Create an inbox-first evergreen promotion draft in the vault.

        This is the primary way Hermes writes to the knowledge plane.
        Drafts land in inbox/drafted-from-foxctl/ for review.
        """
        args = [
            "obsidian", "promote-evergreen",
            "--slug", slug,
            "--content", content,
        ]
        vp = vault_path or self.cfg.vault_path
        if vp:
            args.extend(["--vault-path", vp])
        resp = self._cli(*args)
        return resp.get("data", resp)

    def vault_append(
        self,
        path: str,
        heading: str,
        content: str,
        vault_path: Optional[str] = None,
    ) -> Dict:
        """Append content under a specific heading in a vault note."""
        args = [
            "obsidian", "append-under-heading",
            "--path", path,
            "--heading", heading,
            "--content", content,
        ]
        vp = vault_path or self.cfg.vault_path
        if vp:
            args.extend(["--vault-path", vp])
        resp = self._cli(*args)
        return resp.get("data", resp)

    def vault_bridge(
        self,
        vault_path: Optional[str] = None,
        docs_root: Optional[str] = None,
    ) -> Dict:
        """Reconcile repo docs with vault notes (bridge)."""
        args = [
            "obsidian", "bridge", "reconcile",
            "--workspace", self.cfg.workspace,
        ]
        vp = vault_path or self.cfg.vault_path
        if vp:
            args.extend(["--vault-path", vp])
        if docs_root:
            args.extend(["--docs-root", docs_root])
        resp = self._cli(*args, timeout=30)
        return resp.get("data", resp)

    def vault_graph_build(self, vault_path: Optional[str] = None) -> Dict:
        """Generate a repo graph draft bundle in the vault inbox."""
        args = [
            "obsidian", "graph", "build",
            "--workspace", self.cfg.workspace,
        ]
        vp = vault_path or self.cfg.vault_path
        if vp:
            args.extend(["--vault-path", vp])
        resp = self._cli(*args, timeout=30)
        return resp.get("data", resp)

    # -- Multi-Agent Coordination -----------------------------------------

    def agent_list(self) -> Dict:
        """List all registered agents."""
        return self._get("/api/agents")

    def agent_info(self, agent_id: str) -> Dict:
        """Get info about a specific agent."""
        return self._get(f"/api/agents/{agent_id}")

    def room_task_list(self, room_id: Optional[str] = None, status: str = "") -> Dict:
        """List tasks associated with a room."""
        rid = room_id or self.cfg.room
        params = {"workspace_id": self.cfg.workspace}
        if status:
            params["status"] = status
        return self._get(f"/api/rooms/{rid}/tasks", params)

    def room_task_add(
        self,
        title: str,
        description: str = "",
        room_id: Optional[str] = None,
        scope: str = "",
        depends_on: Optional[List[str]] = None,
    ) -> Dict:
        """Create a task associated with a room."""
        rid = room_id or self.cfg.room
        args = [
            "room", "task", "add", rid,
            "--title", title,
            "--workspace", self.cfg.workspace,
            "--sender", self.cfg.actor,
        ]
        if description:
            args.extend(["--description", description])
        if scope:
            args.extend(["--scope", scope])
        for dep in (depends_on or []):
            args.extend(["--depends-on", dep])
        resp = self._cli(*args)
        return resp.get("data", resp)

    def room_task_claim(self, task_id: str, room_id: Optional[str] = None) -> Dict:
        """Claim a room task for this agent."""
        rid = room_id or self.cfg.room
        resp = self._cli(
            "room", "task", "claim", rid,
            "--id", task_id,
            "--workspace", self.cfg.workspace,
        )
        return resp.get("data", resp)

    def room_task_complete(self, task_id: str, room_id: Optional[str] = None, notes: str = "") -> Dict:
        """Complete a room task and broadcast to room."""
        rid = room_id or self.cfg.room
        args = [
            "room", "task", "complete", rid,
            "--id", task_id,
            "--workspace", self.cfg.workspace,
        ]
        if notes:
            args.extend(["--notes", notes])
        resp = self._cli(*args)
        return resp.get("data", resp)

    def room_task_block(self, task_id: str, reason: str = "", room_id: Optional[str] = None) -> Dict:
        """Mark a claimed task as blocked."""
        rid = room_id or self.cfg.room
        args = [
            "room", "task", "block", rid,
            "--id", task_id,
            "--workspace", self.cfg.workspace,
        ]
        if reason:
            args.extend(["--reason", reason])
        resp = self._cli(*args)
        return resp.get("data", resp)

    def room_task_abandon(self, task_id: str, room_id: Optional[str] = None) -> Dict:
        """Release a claimed task back to pending."""
        rid = room_id or self.cfg.room
        resp = self._cli(
            "room", "task", "abandon", rid,
            "--id", task_id,
            "--workspace", self.cfg.workspace,
        )
        return resp.get("data", resp)

    def room_status(self, room_id: Optional[str] = None) -> Dict:
        """Get room status including participants, backlog, and task pulse."""
        rid = room_id or self.cfg.room
        return self._get(f"/api/rooms/{rid}/status", {"workspace_id": self.cfg.workspace})

    def room_task_assign(self, task_id: str, recipient: str, room_id: Optional[str] = None) -> Dict:
        """Assign a room task to a participant (coordinator only)."""
        rid = room_id or self.cfg.room
        resp = self._cli(
            "room", "task", "assign", rid,
            "--id", task_id,
            "--workspace", self.cfg.workspace,
        )
        return resp.get("data", resp)

    def publish_agent_context(self, context: str, room_id: Optional[str] = None) -> Dict:
        """Publish agent context to the room for other agents to read.

        Uses room message with a structured subject so other agents
        can discover and consume the context.
        """
        rid = room_id or self.cfg.room
        return self._post(f"/api/rooms/{rid}/messages", {
            "workspace_id": self.cfg.workspace,
            "sender": self.cfg.actor,
            "subject": "agent-context-broadcast",
            "body": context,
        })

    # -- Pipe Protocol ----------------------------------------------------

    def pipe_emit(
        self,
        pipe_id: str,
        payload: str,
        target_agents: Optional[List[str]] = None,
        room_id: Optional[str] = None,
    ) -> Dict:
        """Emit a structured pipe message to the room.

        Writes a pipe-formatted room message that other agents can
        consume via pipe_receive or talkback rules. The message has
        subject 'pipe:<pipe_id>' and structured JSON body.
        """
        rid = room_id or self.cfg.room
        body = {
            "pipe_id": pipe_id,
            "source": self.cfg.actor,
            "targets": target_agents or ["*"],
            "payload": payload,
        }
        return self._post(f"/api/rooms/{rid}/messages", {
            "workspace_id": self.cfg.workspace,
            "sender": self.cfg.actor,
            "subject": f"pipe:{pipe_id}",
            "body": json.dumps(body),
        })

    def pipe_receive(
        self,
        pipe_id: str = "",
        room_id: Optional[str] = None,
        limit: int = 10,
    ) -> Dict:
        """Receive pipe messages from the room inbox.

        Reads pending room messages matching 'pipe:<pipe_id>' subject.
        If pipe_id is empty, returns all pipe messages.
        """
        rid = room_id or self.cfg.room
        resp = self.room_inbox(room_id=rid, only="pending", limit=limit)
        # Filter for pipe messages
        messages = resp.get("messages", resp.get("data", {}).get("messages", []))
        pipe_msgs = []
        for msg in messages:
            subject = msg.get("subject", "")
            if subject.startswith("pipe:"):
                if not pipe_id or subject == f"pipe:{pipe_id}":
                    pipe_msgs.append(msg)
        return {"pipe_messages": pipe_msgs, "count": len(pipe_msgs)}

    # -- memory search ------------------------------------------------------

    def memory_search(self, query: str, limit: int = 5, include_content: bool = True) -> Dict:
        body = {
            "workspace": self.cfg.workspace,
            "query": query,
            "limit": limit,
            "include_content": include_content,
        }
        resp = self._post("/api/skills/memory/query", body)
        return self._unwrap_skill(resp)

    # -- session recall -----------------------------------------------------

    def session_recall(self, query: str, limit: int = 3) -> Dict:
        body = {
            "workspace": self.cfg.workspace,
            "query": query,
            "limit": limit,
        }
        resp = self._post("/api/skills/session/recall", body)
        return self._unwrap_skill(resp)

    # -- repo index ---------------------------------------------------------

    def repo_index_search(self, query: str, limit: int = 10) -> Dict:
        """Search the repo index for matching symbol/file nodes."""
        body = {"workspace": self.cfg.workspace, "query": query, "limit": limit}
        resp = self._post("/api/skills/repo/index_search", body)
        return self._unwrap_skill(resp)

    def repo_index_dag(self, query: str, depth: int = 2, budget: int = 50) -> Dict:
        """Search repo index and return an explanation DAG graph."""
        body = {
            "workspace": self.cfg.workspace,
            "query": query,
            "depth": depth,
            "budget": budget,
        }
        resp = self._post("/api/skills/repo/index_dag_grep", body)
        return self._unwrap_skill(resp)

    def repo_index_expand(self, seeds: List[str], depth: int = 1, budget: int = 30) -> Dict:
        """Expand repo index graph edges from seed node IDs."""
        body = {
            "workspace": self.cfg.workspace,
            "seeds": seeds,
            "depth": depth,
            "budget": budget,
        }
        resp = self._post("/api/skills/repo/index_expand", body)
        return self._unwrap_skill(resp)

    def repo_index_open(self, node_id: str) -> Dict:
        """Open one repo-index node by ID with full metadata."""
        body = {"workspace": self.cfg.workspace, "id": node_id}
        resp = self._post("/api/skills/repo/index_open", body)
        return self._unwrap_skill(resp)

    # -- code search --------------------------------------------------------

    def code_context_grep(
        self,
        pattern: str,
        mode: str = "ripgrep",
        language: Optional[str] = None,
        path: Optional[str] = None,
        max_blocks: int = 10,
    ) -> Dict:
        """Search code patterns and return surrounding function/class blocks."""
        body: Dict[str, Any] = {
            "workspace": self.cfg.workspace,
            "pattern": pattern,
            "mode": mode,
            "max_blocks": max_blocks,
        }
        if language:
            body["language"] = language
        if path:
            body["path"] = path
        resp = self._post("/api/skills/code/context_grep", body)
        return self._unwrap_skill(resp)

    def code_semantic_search(self, query: str, limit: int = 10) -> Dict:
        """Unified semantic search across symbols, sessions, memory, codemaps."""
        body = {
            "workspace": self.cfg.workspace,
            "query": query,
            "limit": limit,
        }
        resp = self._post("/api/skills/code/semantic_search", body)
        return self._unwrap_skill(resp)

    def code_smart_search(self, query: str, limit: int = 10, max_snippets: int = 5) -> Dict:
        """Smart code search with auto-generated candidates from indexes."""
        body = {
            "workspace": self.cfg.workspace,
            "query": query,
            "limit": limit,
            "max_snippets": max_snippets,
        }
        resp = self._post("/api/skills/code/smart_search", body)
        return self._unwrap_skill(resp)

    def code_symbols(self, path: str) -> Dict:
        """Extract code symbols from a file."""
        body = {"workspace": self.cfg.workspace, "path": path}
        resp = self._post("/api/skills/code/symbols", body)
        return self._unwrap_skill(resp)

    # -- text search --------------------------------------------------------

    def text_grep(
        self,
        pattern: str,
        include: Optional[List[str]] = None,
        exclude: Optional[List[str]] = None,
        path: Optional[str] = None,
        max_matches: int = 50,
        ci: bool = False,
    ) -> Dict:
        """Regex search across the workspace."""
        body: Dict[str, Any] = {
            "workspace": self.cfg.workspace,
            "pattern": pattern,
            "max_matches": max_matches,
            "ci": ci,
        }
        if include:
            body["include"] = include
        if exclude:
            body["exclude"] = exclude
        if path:
            body["path"] = path
        resp = self._post("/api/skills/text/grep", body)
        return self._unwrap_skill(resp)

    # -- filesystem ---------------------------------------------------------

    def fs_read(self, path: str, max_bytes: int = 50000) -> Dict:
        """Read file contents through CAS-backed preview."""
        body = {
            "workspace": self.cfg.workspace,
            "path": path,
            "max_bytes": max_bytes,
        }
        resp = self._post("/api/skills/fs/read", body)
        return self._unwrap_skill(resp)

    def fs_find(
        self,
        query: Optional[str] = None,
        pattern: Optional[str] = None,
        path: str = ".",
        max_results: int = 20,
    ) -> Dict:
        """Find files by name/path query or glob pattern."""
        body: Dict[str, Any] = {
            "workspace": self.cfg.workspace,
            "path": path,
            "max_results": max_results,
        }
        if query:
            body["query"] = query
        if pattern:
            body["pattern"] = pattern
        resp = self._post("/api/skills/fs/find", body)
        return self._unwrap_skill(resp)

    # -- codemaps -----------------------------------------------------------

    def codemap_list(self, limit: int = 10) -> Dict:
        """List available codemaps."""
        body = {"workspace": self.cfg.workspace, "limit": limit}
        resp = self._post("/api/skills/codemap/list", body)
        return self._unwrap_skill(resp)

    def codemap_get(self, codemap_id: str) -> Dict:
        """Get codemap details by ID."""
        body = {"workspace": self.cfg.workspace, "id": codemap_id}
        resp = self._post("/api/skills/codemap/get", body)
        return self._unwrap_skill(resp)

    # -- Memory write -------------------------------------------------------

    def memory_put(
        self,
        name: str,
        content: str,
        summary: str,
        kind: str = "knowledge",
        tags: Optional[List[str]] = None,
        file_refs: Optional[List[str]] = None,
    ) -> Dict:
        """Store a knowledge record in the foxctl named memory store.

        Uses the foxctl CLI binary to call ``foxctl memory put``, which
        persists the record to the Turso store with optional embedding.
        Falls back to the skill API if the CLI is unavailable.
        """
        import json as _json
        import subprocess

        result_data = {
            "title": name,
            "content": content,
            "tags": tags or [],
        }
        if file_refs:
            result_data["file_refs"] = file_refs

        # Try foxctl CLI first (most reliable write path)
        cli_paths = [
            "/home/dev/repos/foxctl/bin/foxctl",
            "foxctl",  # from PATH
        ]
        for cli in cli_paths:
            try:
                result = subprocess.run(
                    [cli, "memory", "put",
                     "--name", name,
                     "--type", kind,
                     "--summary", summary,
                     "--workspace", self.cfg.workspace,
                     "--data", _json.dumps(result_data)],
                    capture_output=True, text=True, timeout=10,
                )
                if result.returncode == 0:
                    # Parse the CLI envelope output
                    try:
                        return _json.loads(result.stdout)
                    except _json.JSONDecodeError:
                        return {"ok": True, "name": name, "method": "cli"}
            except (FileNotFoundError, subprocess.TimeoutExpired):
                continue

        # Fallback: use the companion memory import API
        # This stores in the companion's per-conversation store
        body = {
            "entries": [{
                "role": "system",
                "content": f"[{kind}] {name}: {summary}",
            }]
        }
        try:
            self._post(f"/api/companion/memory/hermes/import", body)
        except Exception:
            pass

        return {"ok": True, "name": name, "method": "companion_fallback"}

    def memory_curator(self, mode: str = "dry_run", limit: int = 100) -> Dict:
        """Run a deterministic curator report on the memory store."""
        return self._skill(
            "memory/curator_report",
            mode=mode,
            limit=limit,
            persist_report=(mode == "apply"),
        )

    def session_extract_learnings(self, session_id: str, dry_run: bool = False) -> Dict:
        """Extract learnings from a session and store as memory records."""
        return self._skill(
            "session/extract_learnings",
            session_id=session_id,
            dry_run=dry_run,
        )

    def embedding_flush(self, batch_size: int = 50, max_duration: int = 120) -> Dict:
        """Process queued embedding jobs."""
        return self._skill(
            "embedding/worker",
            batch_size=batch_size,
            max_duration=max_duration,
            process_all=True,
        )

    # -- rooms --------------------------------------------------------------

    def list_rooms(self, limit: int = 12) -> Dict:
        return self._get("/api/rooms", self._query(actor_id=self.cfg.actor, limit=limit))

    def room_status(self, room_id: Optional[str] = None) -> Dict:
        rid = room_id or self.cfg.room
        if not rid:
            raise FoxctlError(400, "room_id required or set FOXCTL_ROOM", {})
        return self._get(f"/api/rooms/{rid}", self._query())

    def bind_to_room(self, room_id: Optional[str] = None) -> Dict:
        """Register this agent as a room member."""
        rid = room_id or self.cfg.room
        if not rid:
            raise FoxctlError(400, "room_id required", {})
        body = {
            "workspace_id": self.cfg.workspace,
            "id": rid,
            "title": rid,
            "members": [{
                "actor_id": self.cfg.actor,
                "role": "participant",
                "backend": "herdr",
                "session": self.cfg.session,
                "transport_kind": "pi-extension",
                "transport_endpoint": os.environ.get("HERDR_PANE_ID", ""),
                "delivery_binding": {
                    "transport_kind": "pi-extension",
                    "transport_endpoint": os.environ.get("HERDR_PANE_ID", ""),
                    "health": "unknown",
                    "fallback_policy": "room-inbox",
                },
            }],
        }
        try:
            return self._post("/api/rooms", body)
        except FoxctlError as e:
            if e.status != 409:
                raise
            # Room exists — update binding
            return self._put(
                f"/api/rooms/{rid}/members/{self.cfg.actor}/binding",
                {**body["members"][0], "actor_id": self.cfg.actor, **self._query()},
            )

    # -- room messages ------------------------------------------------------

    def room_send(self, message: str, room_id: Optional[str] = None) -> Dict:
        rid = room_id or self.cfg.room
        if not rid:
            raise FoxctlError(400, "room_id required or set FOXCTL_ROOM", {})
        return self._post(f"/api/rooms/{rid}/messages", {
            "workspace_id": self.cfg.workspace,
            "sender": self.cfg.actor,
            "body": message,
        })

    def room_inbox(self, room_id: Optional[str] = None, only: str = "pending", limit: int = 50) -> Dict:
        rid = room_id or self.cfg.room
        if not rid:
            raise FoxctlError(400, "room_id required or set FOXCTL_ROOM", {})
        return self._get(
            f"/api/rooms/{rid}/inbox",
            self._query(actor_id=self.cfg.actor, only=only, limit=limit),
        )

    def room_messages(self, room_id: Optional[str] = None, limit: int = 50) -> Dict:
        rid = room_id or self.cfg.room
        if not rid:
            raise FoxctlError(400, "room_id required or set FOXCTL_ROOM", {})
        return self._get(f"/api/rooms/{rid}/messages", self._query(limit=limit))

    def room_message_ack(self, message_id: str, room_id: Optional[str] = None) -> Dict:
        rid = room_id or self.cfg.room
        if not rid:
            raise FoxctlError(400, "room_id required or set FOXCTL_ROOM", {})
        return self._post(
            f"/api/rooms/{rid}/messages/{message_id}/ack",
            {"workspace_id": self.cfg.workspace, "actor_id": self.cfg.actor},
        )

    # -- agile: epics -------------------------------------------------------

    def epic_show(self, epic_id: Optional[str] = None) -> Dict:
        return self._agile("epic_show", epic_id=epic_id or self.cfg.epic_id or "")

    def epic_resume(self, epic_id: Optional[str] = None, limit: int = 100) -> Dict:
        return self._agile("epic_resume", epic_id=epic_id or self.cfg.epic_id or "", limit=limit)

    def epic_health(self, epic_id: Optional[str] = None) -> Dict:
        return self._agile("epic_health", epic_id=epic_id or self.cfg.epic_id or "")

    def epic_next(self, epic_id: Optional[str] = None) -> Dict:
        return self._agile("epic_next", epic_id=epic_id or self.cfg.epic_id or "")

    # -- agile: milestones --------------------------------------------------

    def milestone_show(self, milestone_id: str, limit: int = 100) -> Dict:
        return self._agile("milestone_show", milestone_id=milestone_id, limit=limit)

    # -- agile: stories -----------------------------------------------------

    def story_show(self, story_id: Optional[str] = None, limit: int = 100) -> Dict:
        return self._agile("story_show", story_id=story_id or "", limit=limit)

    def story_start(self, story_id: str) -> Dict:
        return self._agile("story_state", story_id=story_id, state="in_progress")

    def story_review(self, story_id: str) -> Dict:
        return self._agile("story_state", story_id=story_id, state="in_review")

    def story_validate(self, story_id: str, verdict: str = "pass", validator_type: str = "agent", notes: str = "") -> Dict:
        return self._agile(
            "story_validate",
            story_id=story_id,
            verdict=verdict,
            validator_type=validator_type,
            notes=notes,
        )


class FoxctlError(Exception):
    """Error from the foxctl API."""

    def __init__(self, status: int, message: str, body: Dict):
        super().__init__(message)
        self.status = status
        self.message = message
        self.body = body
