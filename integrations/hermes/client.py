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

    def _patch(self, path: str, body: Optional[Dict] = None) -> Dict:
        return self._request("PATCH", path, body)

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

    # -- Flow DAG ----------------------------------------------------------

    def flow_create(
        self,
        name: str,
        description: str = "",
    ) -> Dict:
        """Create a new flow graph."""
        cmd = ["flow", "create", "--name", name, "--workspace", self.cfg.workspace]
        if description:
            cmd += ["--description", description]
        return self._cli(*cmd)

    def flow_show(self, flow_id: str) -> Dict:
        """Show flow detail including nodes and edges."""
        return self._cli("flow", "show", flow_id, "--workspace", self.cfg.workspace)

    def flow_list(self) -> Dict:
        """List all flows in the workspace."""
        return self._cli("flow", "list", "--workspace", self.cfg.workspace)

    def flow_delete(self, flow_id: str) -> Dict:
        """Delete a flow."""
        return self._cli("flow", "delete", flow_id, "--workspace", self.cfg.workspace)

    def flow_add_node(
        self,
        flow_id: str,
        kind: str,
        label: str,
        config: Dict,
        position: Optional[Dict] = None,
    ) -> Dict:
        """Add a node to a flow. kind is one of: skill, pty, http, playwright, image, transform, agent."""
        cmd = [
            "flow", "add-node", flow_id,
            "--kind", kind,
            "--label", label,
            "--config", json.dumps(config),
            "--workspace", self.cfg.workspace,
        ]
        if position:
            cmd += ["--position", json.dumps(position)]
        return self._cli(*cmd)

    def flow_add_edge(
        self,
        flow_id: str,
        from_node: str,
        to_node: str,
        transform: str = "passthrough",
        transform_config: str = "",
        trigger: str = "output_ready",
        condition: str = "",
        retry: str = "",
    ) -> Dict:
        """Add an edge between two nodes in a flow."""
        cmd = [
            "flow", "add-edge", flow_id,
            "--from", from_node,
            "--to", to_node,
            "--transform", transform,
            "--trigger", trigger,
            "--workspace", self.cfg.workspace,
        ]
        if transform_config:
            cmd += ["--transform-config", transform_config]
        if condition:
            cmd += ["--condition", condition]
        if retry:
            cmd += ["--retry", retry]
        return self._cli(*cmd)

    def flow_remove_node(self, flow_id: str, node_id: str) -> Dict:
        """Remove a node from a flow."""
        return self._cli("flow", "remove-node", flow_id, node_id, "--workspace", self.cfg.workspace)

    def flow_remove_edge(self, flow_id: str, edge_id: str) -> Dict:
        """Remove an edge from a flow."""
        return self._cli("flow", "remove-edge", flow_id, edge_id, "--workspace", self.cfg.workspace)

    def flow_start(self, flow_id: str) -> Dict:
        """Start executing a flow."""
        return self._cli("flow", "start", flow_id, "--workspace", self.cfg.workspace, timeout=30)

    def flow_stop(self, flow_id: str) -> Dict:
        """Stop a running flow."""
        return self._cli("flow", "stop", flow_id, "--workspace", self.cfg.workspace)

    def flow_pause(self, flow_id: str) -> Dict:
        """Pause a running flow."""
        return self._cli("flow", "pause", flow_id, "--workspace", self.cfg.workspace)

    def flow_status(self, flow_id: str) -> Dict:
        """Get runtime status of a flow."""
        return self._cli("flow", "status", flow_id, "--workspace", self.cfg.workspace)

    def flow_logs(
        self,
        flow_id: str,
        run_id: str = "",
        node: str = "",
    ) -> Dict:
        """Get execution logs for a flow run."""
        cmd = ["flow", "logs"]
        if run_id:
            cmd.append(run_id)
        else:
            # Get latest run from status
            status = self.flow_status(flow_id)
            run_data = status.get("data", {}).get("run", {})
            rid = run_data.get("id", "")
            if rid:
                cmd.append(rid)
            else:
                cmd.append(flow_id)  # fallback
        cmd += ["--workspace", self.cfg.workspace]
        if node:
            cmd += ["--node", node]
        return self._cli(*cmd, timeout=30)

    def flow_output(
        self,
        flow_id: str,
        node: str,
        data: Dict,
    ) -> Dict:
        """Push structured output to a running flow node."""
        return self._cli(
            "flow", "output",
            "--node", node,
            "--data", json.dumps(data),
            "--workspace", self.cfg.workspace,
            flow_id,
            timeout=15,
        )

    # -- Flow Templates -----------------------------------------------------

    def flow_build_pipeline(
        self,
        name: str,
        description: str,
        stages: List[Dict],
    ) -> Dict:
        """Build a linear agent pipeline flow from stage definitions.

        Each stage dict has: kind, label, config.
        Stages are connected sequentially with passthrough edges.
        Returns the created flow with all nodes and edges.
        """
        # Create flow
        flow = self.flow_create(name, description)
        flow_data = flow.get("data", {})
        flow_id = flow_data.get("id", flow_data.get("name", ""))
        if not flow_id:
            return flow

        nodes = []
        # Add nodes
        for i, stage in enumerate(stages):
            node = self.flow_add_node(
                flow_id,
                kind=stage["kind"],
                label=stage["label"],
                config=stage["config"],
                position={"x": float(i * 200), "y": 0},
            )
            node_data = node.get("data", {})
            node_id = node_data.get("id", node_data.get("label", ""))
            nodes.append(node_id)

        # Add sequential edges
        for i in range(len(nodes) - 1):
            self.flow_add_edge(
                flow_id,
                from_node=nodes[i],
                to_node=nodes[i + 1],
                transform=stages[i + 1].get("transform", "passthrough"),
                transform_config=stages[i + 1].get("transform_config", ""),
            )

        return self.flow_show(flow_id)

    def flow_build_fan_out(
        self,
        name: str,
        description: str,
        source: Dict,
        sinks: List[Dict],
        transform: str = "passthrough",
    ) -> Dict:
        """Build a fan-out flow: one source broadcasts to multiple parallel sinks.

        source: {kind, label, config}
        sinks: [{kind, label, config, transform?, transform_config?}]
        """
        flow = self.flow_create(name, description)
        flow_data = flow.get("data", {})
        flow_id = flow_data.get("id", flow_data.get("name", ""))
        if not flow_id:
            return flow

        # Add source node
        src_node = self.flow_add_node(
            flow_id,
            kind=source["kind"],
            label=source["label"],
            config=source["config"],
            position={"x": 0, "y": 0},
        )
        src_id = src_node.get("data", {}).get("id", source["label"])

        # Add sink nodes and edges from source
        for i, sink in enumerate(sinks):
            sink_node = self.flow_add_node(
                flow_id,
                kind=sink["kind"],
                label=sink["label"],
                config=sink["config"],
                position={"x": 300, "y": float(i * 150)},
            )
            sink_id = sink_node.get("data", {}).get("id", sink["label"])
            self.flow_add_edge(
                flow_id,
                from_node=src_id,
                to_node=sink_id,
                transform=sink.get("transform", transform),
                transform_config=sink.get("transform_config", ""),
            )

        return self.flow_show(flow_id)

    # -- Context Curator ---------------------------------------------------

    def context_curator(self, mode: str = "dry_run", stale_after_days: int = 30) -> Dict:
        """Run a unified context plane curator report.

        Collects data from all context plane stores and produces a
        deterministic report with proposals for cleanup:
        - Memory: stale/low-utility records, duplicates, supersessions
        - Observations: low-confidence, stale, or redundant entries
        - Tensions: open tensions past stale threshold
        - Handoffs: files older than stale threshold
        - Vault: orphaned drafts, stale inbox items
        """
        import subprocess as _sp
        import glob as _glob
        import os as _os

        proposals = []
        summary = {
            "memory_records": 0,
            "observations": 0,
            "tensions": 0,
            "handoffs": 0,
            "vault_drafts": 0,
            "total_proposals": 0,
        }

        # 1. Memory curator (deterministic)
        try:
            mem_report = self._skill(
                "memory/curator_report",
                mode="dry_run",
                limit=1000,
                stale_after_days=stale_after_days,
            )
            report_data = mem_report.get("report", mem_report)
            summary["memory_records"] = report_data.get("summary", {}).get("total_records", 0)
            for p in report_data.get("proposals", []):
                proposals.append({
                    "source": "memory",
                    "record_id": p.get("record_id", ""),
                    "action": p.get("action", ""),
                    "current_state": p.get("current_state", ""),
                    "proposed_state": p.get("proposed_state", ""),
                    "reasons": p.get("reasons", []),
                    "utility_score": p.get("utility_score", 0),
                })
            for cluster in report_data.get("consolidation_clusters", []):
                proposals.append({
                    "source": "memory_consolidation",
                    "kind": cluster.get("kind", ""),
                    "record_ids": cluster.get("record_ids", []),
                    "primary": cluster.get("primary_record_id", ""),
                    "signals": cluster.get("signals", []),
                    "action": "consolidate",
                    "manual_review": cluster.get("manual_review", True),
                })
        except Exception as e:
            proposals.append({"source": "memory", "action": "error", "reasons": [str(e)]})

        # 2. Observations (from context plane)
        try:
            obs_resp = self._cli("context", "observations", "--workspace", self.cfg.workspace, "--limit", "100")
            observations = obs_resp.get("data", {}).get("observations", [])
            summary["observations"] = len(observations)
            now_str = __import__("time").strftime("%Y-%m-%dT%H:%M:%S", __import__("time").gmtime())
            for obs in observations:
                reasons = []
                if obs.get("confidence", 1) < 0.5:
                    reasons.append(f"low confidence ({obs.get('confidence', 0):.2f})")
                if obs.get("count", 0) == 1:
                    last_seen = obs.get("last_seen", "")
                    if last_seen:
                        days_old = self._days_since(last_seen)
                        if days_old > stale_after_days:
                            reasons.append(f"seen only once, {days_old}d ago")
                if reasons:
                    proposals.append({
                        "source": "observation",
                        "record_id": obs.get("id", ""),
                        "action": "review",
                        "statement": obs.get("statement", "")[:100],
                        "confidence": obs.get("confidence", 0),
                        "count": obs.get("count", 0),
                        "reasons": reasons,
                    })
        except Exception:
            pass

        # 3. Tensions (from context plane)
        try:
            tens_resp = self._cli("context", "tensions", "--workspace", self.cfg.workspace, "--limit", "100")
            tensions = tens_resp.get("data", {}).get("tensions", [])
            summary["tensions"] = len(tensions)
            for t in tensions:
                if t.get("status") == "open":
                    reasons = []
                    last_seen = t.get("last_seen", "")
                    if last_seen:
                        days_old = self._days_since(last_seen)
                        if days_old > stale_after_days // 2:
                            reasons.append(f"open tension {days_old}d old")
                    if t.get("count", 0) > 3:
                        reasons.append(f"recurring ({t['count']} occurrences)")
                    if reasons:
                        proposals.append({
                            "source": "tension",
                            "record_id": t.get("id", ""),
                            "action": "address_or_close",
                            "kind": t.get("kind", ""),
                            "statement": t.get("statement", "")[:100],
                            "status": t.get("status", ""),
                            "count": t.get("count", 0),
                            "reasons": reasons,
                        })
        except Exception:
            pass

        # 4. Handoffs (filesystem scan)
        try:
            handoff_dir = _os.path.join(self.cfg.workspace, ".foxctl", "runtime", "handoffs")
            if _os.path.isdir(handoff_dir):
                handoff_files = _glob.glob(_os.path.join(handoff_dir, "*.json"))
                summary["handoffs"] = len(handoff_files)
                for hf in handoff_files:
                    mtime = _os.path.getmtime(hf)
                    days_old = (__import__("time").time() - mtime) / 86400
                    if days_old > stale_after_days:
                        proposals.append({
                            "source": "handoff",
                            "record_id": _os.path.basename(hf),
                            "action": "archive",
                            "reasons": [f"handoff file {days_old:.0f}d old"],
                        })
        except Exception:
            pass

        # 5. Vault drafts (inbox items)
        try:
            if hasattr(self.cfg, 'vault_path') and self.cfg.vault_path:
                inbox_dir = _os.path.join(self.cfg.workspace, self.cfg.vault_path, "inbox", "drafted-from-foxctl")
                if _os.path.isdir(inbox_dir):
                    draft_files = []
                    for root, dirs, files in _os.walk(inbox_dir):
                        for f in files:
                            if f.endswith('.md'):
                                draft_files.append(_os.path.join(root, f))
                    summary["vault_drafts"] = len(draft_files)
                    for df in draft_files:
                        mtime = _os.path.getmtime(df)
                        days_old = (__import__("time").time() - mtime) / 86400
                        if days_old > stale_after_days:
                            proposals.append({
                                "source": "vault_draft",
                                "record_id": _os.path.relpath(df, _os.path.join(self.cfg.workspace, self.cfg.vault_path)),
                                "action": "promote_or_archive",
                                "reasons": [f"inbox draft {days_old:.0f}d old"],
                            })
        except Exception:
            pass

        summary["total_proposals"] = len(proposals)
        return {
            "mode": mode,
            "stale_after_days": stale_after_days,
            "summary": summary,
            "proposals": proposals,
        }

    @staticmethod
    def _days_since(iso_timestamp: str) -> int:
        """Days since an ISO timestamp."""
        import time as _time
        try:
            # Parse ISO timestamp
            ts = iso_timestamp[:19]  # trim to seconds
            then = _time.mktime(_time.strptime(ts, "%Y-%m-%dT%H:%M:%S"))
            now = _time.time()
            return max(0, int((now - then) / 86400))
        except (ValueError, TypeError):
            return 0

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

    # -- direct actor mailbox -----------------------------------------------

    def mailbox_send(self, recipient: str, subject: str, body: str,
                     kind: str = "info", priority: int = 3) -> Dict:
        """Send a direct message to another agent's mailbox."""
        return self._post("/api/mailbox", {
            "workspace_id": self.cfg.workspace,
            "sender": self.cfg.actor,
            "recipient": recipient,
            "subject": subject,
            "body": body,
            "kind": kind,
            "priority": priority,
        })

    def mailbox_inbox(self, only_unread: bool = False, limit: int = 20) -> Dict:
        """Read this agent's mailbox inbox."""
        return self._get("/api/mailbox", self._query(
            workspace_id=self.cfg.workspace,
            actor_id=self.cfg.actor,
            only_unread=str(only_unread).lower(),
            limit=limit,
        ))

    def mailbox_ack(self, message_ids: list) -> Dict:
        """Acknowledge mailbox messages."""
        return self._patch("/api/mailbox", {
            "workspace_id": self.cfg.workspace,
            "actor_id": self.cfg.actor,
            "action": "ack",
            "message_ids": message_ids,
        })

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
