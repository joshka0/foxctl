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

    # -- memory search ------------------------------------------------------

    def memory_search(self, query: str, limit: int = 5, include_content: bool = True) -> Dict:
        body = {
            "workspace": self.cfg.workspace,
            "query": query,
            "limit": limit,
            "include_content": include_content,
        }
        return self._post("/api/memory/query", body)

    # -- session recall -----------------------------------------------------

    def session_recall(self, query: str, limit: int = 3) -> Dict:
        body = {
            "workspace": self.cfg.workspace,
            "query": query,
            "limit": limit,
        }
        return self._post("/api/session/recall", body)

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
