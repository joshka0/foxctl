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
import threading

logger = logging.getLogger(__name__)

_memory_drafts_runner = None
_memory_drafts_runner_lock = threading.Lock()


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
            pass
        else:
            try:
                result = client.bind_to_room(cfg.room)
                logger.info("foxctl room bind: %s", result.get("status", "ok"))
            except Exception as e:
                logger.warning("foxctl room bind failed: %s", e)
        _ensure_memory_drafts_runner(client, cfg)
    return on_session_start


def _make_session_end_hook(client, cfg):
    """Return an on_session_end callback."""
    def on_session_end(messages=None, **kwargs):
        runner = _ensure_memory_drafts_runner(client, cfg)
        if runner is not None:
            runner.run_once("session_end")
            runner.stop()
        logger.debug("foxctl session end")
    return on_session_end


def _ensure_memory_drafts_runner(client, cfg):
    """Start the opt-in memory-draft background runner for this process."""
    global _memory_drafts_runner
    if not cfg.memory_drafts_auto:
        return None
    with _memory_drafts_runner_lock:
        if _memory_drafts_runner is None:
            _memory_drafts_runner = _MemoryDraftsRunner(client, cfg)
        _memory_drafts_runner.start()
        return _memory_drafts_runner


class _MemoryDraftsRunner:
    """Runs autonomous Obsidian inbox draft creation from provider hooks."""

    def __init__(self, client, cfg):
        self.client = client
        self.cfg = cfg
        self.stop_event = threading.Event()
        self.lock = threading.Lock()
        self.thread = None

    def start(self):
        if self.thread is not None and self.thread.is_alive():
            return
        self.stop_event.clear()
        self._run_background("session_start")
        self.thread = threading.Thread(target=self._loop, name="foxctl-memory-drafts", daemon=True)
        self.thread.start()
        logger.info(
            "foxctl memory drafts auto-runner started (interval=%ss apply=%s dry_run=%s)",
            self._interval_seconds(),
            self.cfg.memory_drafts_apply,
            self._dry_run(),
        )

    def stop(self):
        self.stop_event.set()

    def _loop(self):
        interval = self._interval_seconds()
        while not self.stop_event.wait(interval):
            self.run_once("interval")

    def _run_background(self, reason):
        threading.Thread(
            target=self.run_once,
            args=(reason,),
            name=f"foxctl-memory-drafts-{reason}",
            daemon=True,
        ).start()

    def run_once(self, reason):
        if not self.lock.acquire(blocking=False):
            return
        try:
            result = self.client.context_memory_drafts(
                apply_drafts=self.cfg.memory_drafts_apply,
                dry_run=self._dry_run(),
                lookback=self.cfg.memory_drafts_lookback,
                limit=max(1, int(self.cfg.memory_drafts_limit or 20)),
                vault_path=self.cfg.vault_path or None,
                blur_with_agent=self.cfg.memory_drafts_blur_agent,
                blur_agent=self.cfg.memory_drafts_blur_backend,
                blur_agent_bin=self.cfg.memory_drafts_blur_agent_bin,
                blur_agent_provider=self.cfg.memory_drafts_blur_agent_provider,
                blur_agent_model=self.cfg.memory_drafts_blur_agent_model,
                foxctl_agent_id=self.cfg.memory_drafts_blur_foxctl_agent_id,
            )
            data = result.get("data", result) if isinstance(result, dict) else {}
            report = data.get("report", data) if isinstance(data, dict) else {}
            logger.info(
                "foxctl memory drafts auto-run complete (reason=%s scanned=%s planned=%s written=%s proposals=%s)",
                reason,
                report.get("feedback_scanned", 0),
                report.get("drafts_planned", 0),
                report.get("drafts_written", 0),
                report.get("proposals_recorded", 0),
            )
        except Exception as e:
            logger.warning("foxctl memory drafts auto-run failed (reason=%s): %s", reason, e)
        finally:
            self.lock.release()

    def _dry_run(self):
        return bool(self.cfg.memory_drafts_dry_run or not self.cfg.memory_drafts_apply)

    def _interval_seconds(self):
        return max(10, int(self.cfg.memory_drafts_interval_seconds or 900))
