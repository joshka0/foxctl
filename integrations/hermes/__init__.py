"""Foxctl room-agile integration plugin for Hermes Agent.

Bridges foxctl's daemon HTTP API into hermes tools so the agent can:
  - Search foxctl's memory and session store
  - Send/receive room messages via herdr relay
  - Query epic/milestone/story state
  - Mutate story lifecycle (start, review, validate)
  - Inject foxctl workspace context into conversations

Install:
  Symlink this directory to ``~/.hermes/plugins/foxctl/`` and enable in config:

  plugins:
    enabled:
      - foxctl

  foxctl:
    url: "http://localhost:8090"
    workspace: "."
    room: ""
    actor: "actor:hermes:local"
    auto_bind: false
    memory_context: true
    epic_context: true
"""

from __future__ import annotations

import logging

logger = logging.getLogger(__name__)


def register(ctx) -> None:
    """Plugin entry point — register all foxctl tools and hooks."""
    from .client import FoxctlClient
    from .config import FoxctlConfig
    from .tools import register_tools

    cfg = FoxctlConfig.from_hermes_config()
    client = FoxctlClient(cfg)

    register_tools(ctx, client, cfg)

    # Register lifecycle hooks
    ctx.register_hook("on_session_start", _make_session_start_hook(client, cfg))
    ctx.register_hook("on_session_end", _make_session_end_hook(client, cfg))

    logger.info("foxctl plugin registered (url=%s room=%s actor=%s)", cfg.url, cfg.room or "none", cfg.actor)


def _make_session_start_hook(client, cfg):
    """Return an on_session_start callback that binds to the room if configured."""
    def on_session_start(**kwargs):
        if not cfg.room or not cfg.auto_bind:
            return
        try:
            result = client.bind_to_room(cfg.room)
            logger.info("foxctl room bind: %s", result.get("status", "ok"))
        except Exception as e:
            logger.warning("foxctl room bind failed: %s", e)
    return on_session_start


def _make_session_end_hook(client, cfg):
    """Return an on_session_end callback."""
    def on_session_end(messages=None, **kwargs):
        logger.debug("foxctl session end")
    return on_session_end
