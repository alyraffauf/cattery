#!/usr/bin/env python3
"""Tests for the documentation and credential checkers.

Run directly:

    python3 scripts/tests/check_docs_test.py

The credential fixtures are assembled at runtime from fragments so this tracked
source file never contains a literal credential-shaped value (PLAN.md Section 16
Task 119 acceptance).
"""

from __future__ import annotations

import subprocess
import sys
import unittest
from pathlib import Path

REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
CHECK_DOCS = REPOSITORY_ROOT / "scripts" / "check-docs.py"
CHECK_CREDENTIALS = REPOSITORY_ROOT / "scripts" / "check-credentials.py"

VALID_MARKDOWN = """\
# Valid

Cattery does not support templates; configuration is literal.

Run `cattery apply --dry-run` to preview, then see [layout](repository-layout.md).
"""

BROKEN_LINK_MARKDOWN = "See [missing](does-not-exist.md).\n"
UNKNOWN_COMMAND_MARKDOWN = "Run `cattery frobnicate` now.\n"
UNKNOWN_FLAG_MARKDOWN = "Run `cattery apply --yes` to proceed.\n"
FORBIDDEN_FEATURE_MARKDOWN = "Cattery supports templates for every config file.\n"
NEGATED_FEATURE_MARKDOWN = "Cattery never provides a plugin system.\n"
MULTI_COMMAND_MARKDOWN = "Use `cattery status --verbose` and `cattery version`.\n"


def run(script: Path, *args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [sys.executable, str(script), *args],
        capture_output=True,
        text=True,
    )


class CheckDocsTests(unittest.TestCase):
    def test_valid_markdown_passes(self) -> None:
        with _scratch() as directory:
            target = directory / "valid.md"
            linked = directory / "repository-layout.md"
            target.write_text(VALID_MARKDOWN, encoding="utf-8")
            linked.write_text("# Layout\n", encoding="utf-8")
            result = run(CHECK_DOCS, str(target))
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_broken_link_is_reported(self) -> None:
        with _scratch() as directory:
            target = directory / "broken.md"
            target.write_text(BROKEN_LINK_MARKDOWN, encoding="utf-8")
            result = run(CHECK_DOCS, str(target))
            self.assertIn("broken-link", result.stdout)
            self.assertEqual(result.returncode, 1)

    def test_unknown_command_is_reported(self) -> None:
        with _scratch() as directory:
            target = directory / "cmd.md"
            target.write_text(UNKNOWN_COMMAND_MARKDOWN, encoding="utf-8")
            result = run(CHECK_DOCS, str(target))
            self.assertIn("command-vocabulary", result.stdout)
            self.assertIn("frobnicate", result.stdout)
            self.assertEqual(result.returncode, 1)

    def test_unknown_flag_is_reported(self) -> None:
        with _scratch() as directory:
            target = directory / "flag.md"
            target.write_text(UNKNOWN_FLAG_MARKDOWN, encoding="utf-8")
            result = run(CHECK_DOCS, str(target))
            self.assertIn("unknown flag --yes", result.stdout)
            self.assertEqual(result.returncode, 1)

    def test_forbidden_feature_claim_is_reported(self) -> None:
        with _scratch() as directory:
            target = directory / "feature.md"
            target.write_text(FORBIDDEN_FEATURE_MARKDOWN, encoding="utf-8")
            result = run(CHECK_DOCS, str(target))
            self.assertIn("forbidden-feature", result.stdout)
            self.assertIn("template", result.stdout)
            self.assertEqual(result.returncode, 1)

    def test_negated_feature_is_not_reported(self) -> None:
        with _scratch() as directory:
            target = directory / "negated.md"
            target.write_text(NEGATED_FEATURE_MARKDOWN, encoding="utf-8")
            result = run(CHECK_DOCS, str(target))
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_multiple_valid_commands_pass(self) -> None:
        with _scratch() as directory:
            target = directory / "multi.md"
            target.write_text(MULTI_COMMAND_MARKDOWN, encoding="utf-8")
            result = run(CHECK_DOCS, str(target))
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_invalid_utf8_is_reported_without_crashing(self) -> None:
        with _scratch() as directory:
            target = directory / "binary.md"
            target.write_bytes(b"# Bad\n\xff\xfe garbage\n")
            result = run(CHECK_DOCS, str(target))
            self.assertIn("invalid-utf8", result.stdout)
            self.assertEqual(result.returncode, 1)

    def test_multiple_input_paths(self) -> None:
        with _scratch() as directory:
            good = directory / "good.md"
            bad = directory / "bad.md"
            good.write_text(VALID_MARKDOWN, encoding="utf-8")
            (directory / "repository-layout.md").write_text("# Layout\n", encoding="utf-8")
            bad.write_text(BROKEN_LINK_MARKDOWN, encoding="utf-8")
            result = run(CHECK_DOCS, str(good), str(bad))
            self.assertEqual(result.returncode, 1)
            self.assertIn("bad.md", result.stdout)
            self.assertNotIn("good.md", result.stdout)


class CheckCredentialsTests(unittest.TestCase):
    def test_age_secret_is_detected(self) -> None:
        prefix = "AGE-" + "SECRET-" + "KEY-1"
        secret = prefix + "Q" * 58
        with _scratch() as directory:
            target = directory / "secrets.md"
            target.write_text(f"identity: {secret}\n", encoding="utf-8")
            result = run(CHECK_CREDENTIALS, str(target))
            self.assertIn("age-secret-key", result.stdout)
            self.assertEqual(result.returncode, 1)

    def test_pem_private_key_is_detected(self) -> None:
        header = "-----BEGIN " + "OPENSSH " + "PRIVATE KEY-----"
        with _scratch() as directory:
            target = directory / "key.md"
            target.write_text(header + "\nbody\n", encoding="utf-8")
            result = run(CHECK_CREDENTIALS, str(target))
            self.assertIn("private-key", result.stdout)
            self.assertEqual(result.returncode, 1)

    def test_assigned_secret_value_is_detected(self) -> None:
        value = "d7J9H2k4" * 4  # 32 alphanumeric characters, assembled at runtime
        with _scratch() as directory:
            target = directory / "env.md"
            target.write_text(f"token: {value}\n", encoding="utf-8")
            result = run(CHECK_CREDENTIALS, str(target))
            self.assertIn("assigned-secret", result.stdout)
            self.assertEqual(result.returncode, 1)

    def test_placeholder_values_are_not_flagged(self) -> None:
        with _scratch() as directory:
            target = directory / "example.md"
            target.write_text(
                "token: <your-token-here>\napi_key: EXAMPLEEXAMPLEEXAMPLE\n",
                encoding="utf-8",
            )
            result = run(CHECK_CREDENTIALS, str(target))
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_age_public_recipient_is_not_flagged(self) -> None:
        with _scratch() as directory:
            target = directory / "recipient.md"
            target.write_text("recipient: age1qzvxyexampleexampleexampleexampleexample\n", encoding="utf-8")
            result = run(CHECK_CREDENTIALS, str(target))
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


class _Scratch:
    """Context manager yielding an isolated temp directory."""

    def __init__(self) -> None:
        import tempfile

        self._tempfile = tempfile.TemporaryDirectory(prefix="cattery-docs-test-")

    def __enter__(self) -> Path:
        return Path(self._tempfile.name)

    def __exit__(self, *exc: object) -> None:
        self._tempfile.cleanup()


def _scratch() -> _Scratch:
    return _Scratch()


if __name__ == "__main__":
    unittest.main(verbosity=2)
