"""Configuration for the foxctl plugin.

Reads from hermes config.yaml under the ``foxctl:`` key, with env var
overrides for CI/scripted usage.
"""

from __future__ import annotations

import os
from dataclasses import dataclass, field


@dataclass
class FoxctlConfig:
    url: str = "http://localhost:8090"
    workspace: str = "."
    room: str = ""
    epic_id: str = ""
    actor: str = "actor:hermes:local"
    session: str = ""
    auto_bind: bool = False
    memory_context: bool = True
    epic_context: bool = True
    vault_path: str = ""

    @classmethod
    def from_hermes_config(cls) -> "FoxctlConfig":
        """Load config from hermes config.yaml + env var overrides."""
        cfg = cls()

        # Try loading from hermes config
        try:
            from hermes_cli.config import load_config
            hermes_cfg = load_config()
            foxctl_cfg = hermes_cfg.get("foxctl", {})
            if isinstance(foxctl_cfg, dict):
                cfg.url = foxctl_cfg.get("url", cfg.url)
                cfg.workspace = foxctl_cfg.get("workspace", cfg.workspace)
                cfg.room = foxctl_cfg.get("room", cfg.room)
                cfg.epic_id = foxctl_cfg.get("epic_id", cfg.epic_id)
                cfg.actor = foxctl_cfg.get("actor", cfg.actor)
                cfg.session = foxctl_cfg.get("session", cfg.session)
                cfg.auto_bind = foxctl_cfg.get("auto_bind", cfg.auto_bind)
                cfg.memory_context = foxctl_cfg.get("memory_context", cfg.memory_context)
                cfg.epic_context = foxctl_cfg.get("epic_context", cfg.epic_context)
                cfg.vault_path = foxctl_cfg.get("vault_path", cfg.vault_path)
        except Exception:
            pass

        # Env var overrides (take precedence)
        if v := os.environ.get("FOXCTL_URL"):
            cfg.url = v
        if v := os.environ.get("FOXCTL_WORKSPACE"):
            cfg.workspace = v
        if v := os.environ.get("FOXCTL_ROOM"):
            cfg.room = v
        if v := os.environ.get("FOXCTL_EPIC_ID"):
            cfg.epic_id = v
        if v := os.environ.get("FOXCTL_ACTOR"):
            cfg.actor = v
        if v := os.environ.get("FOXCTL_SESSION"):
            cfg.session = v
        if v := os.environ.get("FOXCTL_AUTO_BIND"):
            cfg.auto_bind = v.lower() in ("1", "true", "yes")

        return cfg
