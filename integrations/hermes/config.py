"""Configuration for the foxctl plugin.

Reads from hermes config.yaml under the ``foxctl:`` key, with env var
overrides for CI/scripted usage.
"""

from __future__ import annotations

import os
from dataclasses import dataclass


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
    memory_drafts_auto: bool = False
    memory_drafts_apply: bool = True
    memory_drafts_dry_run: bool = False
    memory_drafts_interval_seconds: int = 900
    memory_drafts_lookback: str = "24h"
    memory_drafts_limit: int = 20
    memory_drafts_blur_agent: bool = False
    memory_drafts_blur_backend: str = "hermes"
    memory_drafts_blur_agent_bin: str = ""
    memory_drafts_blur_agent_provider: str = ""
    memory_drafts_blur_agent_model: str = ""
    memory_drafts_blur_foxctl_agent_id: str = ""

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
                cfg.memory_drafts_auto = foxctl_cfg.get("memory_drafts_auto", cfg.memory_drafts_auto)
                cfg.memory_drafts_apply = foxctl_cfg.get("memory_drafts_apply", cfg.memory_drafts_apply)
                cfg.memory_drafts_dry_run = foxctl_cfg.get("memory_drafts_dry_run", cfg.memory_drafts_dry_run)
                cfg.memory_drafts_interval_seconds = int(
                    foxctl_cfg.get("memory_drafts_interval_seconds", cfg.memory_drafts_interval_seconds)
                )
                cfg.memory_drafts_lookback = foxctl_cfg.get("memory_drafts_lookback", cfg.memory_drafts_lookback)
                cfg.memory_drafts_limit = int(foxctl_cfg.get("memory_drafts_limit", cfg.memory_drafts_limit))
                cfg.memory_drafts_blur_agent = foxctl_cfg.get(
                    "memory_drafts_blur_agent", cfg.memory_drafts_blur_agent
                )
                cfg.memory_drafts_blur_backend = foxctl_cfg.get(
                    "memory_drafts_blur_backend", cfg.memory_drafts_blur_backend
                )
                cfg.memory_drafts_blur_agent_bin = foxctl_cfg.get(
                    "memory_drafts_blur_agent_bin", cfg.memory_drafts_blur_agent_bin
                )
                cfg.memory_drafts_blur_agent_provider = foxctl_cfg.get(
                    "memory_drafts_blur_agent_provider", cfg.memory_drafts_blur_agent_provider
                )
                cfg.memory_drafts_blur_agent_model = foxctl_cfg.get(
                    "memory_drafts_blur_agent_model", cfg.memory_drafts_blur_agent_model
                )
                cfg.memory_drafts_blur_foxctl_agent_id = foxctl_cfg.get(
                    "memory_drafts_blur_foxctl_agent_id", cfg.memory_drafts_blur_foxctl_agent_id
                )
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
        if v := os.environ.get("FOXCTL_VAULT_PATH"):
            cfg.vault_path = v
        if v := os.environ.get("FOXCTL_MEMORY_DRAFTS_AUTO"):
            cfg.memory_drafts_auto = v.lower() in ("1", "true", "yes")
        if v := os.environ.get("FOXCTL_MEMORY_DRAFTS_APPLY"):
            cfg.memory_drafts_apply = v.lower() in ("1", "true", "yes")
        if v := os.environ.get("FOXCTL_MEMORY_DRAFTS_DRY_RUN"):
            cfg.memory_drafts_dry_run = v.lower() in ("1", "true", "yes")
        if v := os.environ.get("FOXCTL_MEMORY_DRAFTS_INTERVAL_SECONDS"):
            try:
                cfg.memory_drafts_interval_seconds = int(v)
            except ValueError:
                pass
        if v := os.environ.get("FOXCTL_MEMORY_DRAFTS_LOOKBACK"):
            cfg.memory_drafts_lookback = v
        if v := os.environ.get("FOXCTL_MEMORY_DRAFTS_LIMIT"):
            try:
                cfg.memory_drafts_limit = int(v)
            except ValueError:
                pass
        if v := os.environ.get("FOXCTL_MEMORY_DRAFTS_BLUR_AGENT"):
            cfg.memory_drafts_blur_agent = v.lower() in ("1", "true", "yes")
        if v := os.environ.get("FOXCTL_MEMORY_DRAFTS_BLUR_BACKEND"):
            cfg.memory_drafts_blur_backend = v
        if v := os.environ.get("FOXCTL_MEMORY_DRAFTS_BLUR_AGENT_BIN"):
            cfg.memory_drafts_blur_agent_bin = v
        if v := os.environ.get("FOXCTL_MEMORY_DRAFTS_BLUR_AGENT_PROVIDER"):
            cfg.memory_drafts_blur_agent_provider = v
        if v := os.environ.get("FOXCTL_MEMORY_DRAFTS_BLUR_AGENT_MODEL"):
            cfg.memory_drafts_blur_agent_model = v
        if v := os.environ.get("FOXCTL_MEMORY_DRAFTS_BLUR_FOXCTL_AGENT_ID"):
            cfg.memory_drafts_blur_foxctl_agent_id = v

        return cfg
