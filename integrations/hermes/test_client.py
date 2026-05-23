from __future__ import annotations

import subprocess
import unittest
from unittest.mock import patch

from .client import FoxctlClient, FoxctlError
from .config import FoxctlConfig


class HermesMemoryPutTest(unittest.TestCase):
    def client(self) -> FoxctlClient:
        return FoxctlClient(FoxctlConfig(url="http://foxctl.test", workspace="ws"))

    def test_memory_put_returns_cli_envelope(self) -> None:
        completed = subprocess.CompletedProcess(
            args=["foxctl"],
            returncode=0,
            stdout='{"ok": true, "id": "memory-1"}',
            stderr="",
        )

        with patch("subprocess.run", return_value=completed):
            result = self.client().memory_put("n", "content", "summary")

        self.assertEqual(result["id"], "memory-1")

    def test_memory_put_uses_companion_import_when_cli_unavailable(self) -> None:
        client = self.client()

        with patch("subprocess.run", side_effect=FileNotFoundError), patch.object(
            client,
            "_post",
            return_value={"ok": True, "imported": 1},
        ) as post:
            result = client.memory_put("n", "content", "summary")

        self.assertEqual(result["method"], "companion_import")
        self.assertEqual(result["response"], {"ok": True, "imported": 1})
        post.assert_called_once()

    def test_memory_put_raises_when_cli_and_companion_import_fail(self) -> None:
        client = self.client()
        completed = subprocess.CompletedProcess(
            args=["foxctl"],
            returncode=2,
            stdout="",
            stderr="no write",
        )

        with patch("subprocess.run", side_effect=[completed, FileNotFoundError]), patch.object(
            client,
            "_post",
            side_effect=FoxctlError(503, "http down", {}),
        ):
            with self.assertRaises(FoxctlError) as raised:
                client.memory_put("n", "content", "summary")

        self.assertEqual(raised.exception.status, 502)
        attempts = raised.exception.body["attempts"]
        self.assertEqual(attempts[0]["status"], "nonzero_exit")
        self.assertEqual(attempts[-1]["method"], "companion_import")
        self.assertEqual(attempts[-1]["message"], "http down")

    def test_memory_put_rejects_cli_success_with_malformed_json(self) -> None:
        client = self.client()
        completed = subprocess.CompletedProcess(
            args=["foxctl"],
            returncode=0,
            stdout="memory stored",
            stderr="",
        )

        with patch("subprocess.run", side_effect=[completed, FileNotFoundError]), patch.object(
            client,
            "_post",
            side_effect=FoxctlError(503, "http down", {}),
        ):
            with self.assertRaises(FoxctlError) as raised:
                client.memory_put("n", "content", "summary")

        attempts = raised.exception.body["attempts"]
        self.assertEqual(attempts[0]["status"], "invalid_json")
        self.assertEqual(attempts[0]["stdout"], "memory stored")

    def test_branch_impact_uses_canonical_skill_facade(self) -> None:
        client = self.client()

        with patch.object(client, "_skill", return_value={"changed_units": [], "review_candidates": []}) as skill:
            result = client.branch_impact("main", "HEAD", limit=7, depth=3, per_file_cap=11, max_changed=50)

        self.assertEqual(result, {"changed_units": [], "review_candidates": []})
        skill.assert_called_once_with(
            "code/branch_impact",
            base_ref="main",
            head_ref="HEAD",
            limit=7,
            depth=3,
            per_file_cap=11,
            max_changed=50,
        )


if __name__ == "__main__":
    unittest.main()
