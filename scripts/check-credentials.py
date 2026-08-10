#!/usr/bin/env python3
"""Credential-shaped value scanner for Cattery documentation.

Reports literal credential-shaped values in Markdown or text files: age secret
identities, PEM private-key blocks, and high-entropy values assigned to obvious
secret names. Placeholder text (angle brackets, "example", "your-key", runs of
the same character) is intentionally ignored so documented examples remain safe.

Uses only the Python standard library. It never imports project code, never
accesses the network, and never rewrites files. Exits non-zero when any
credential-shaped value is found.

Usage:
    python3 scripts/check-credentials.py [PATH ...]

With no PATH it scans README.md and every *.md file beneath docs/.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path
from typing import Iterable

REPOSITORY_ROOT = Path(__file__).resolve().parents[1]

# A real age secret identity is "AGE-SECRET-KEY-1" plus 58 bech32-alphabet chars.
# age PUBLIC recipients start with "age1" and are NOT secrets; they are ignored.
AGE_SECRET_PATTERN = re.compile(rb"AGE-SECRET-KEY-1[0-9A-Z]{58}")
PEM_PRIVATE_PATTERN = re.compile(rb"-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----")
ASSIGNMENT_PATTERN = re.compile(
    rb"(?P<name>password|passwd|secret|api[_-]?key|access[_-]?key|"
    rb"private[_-]?key|token)\s*[:=]\s*[\"']?(?P<value>[A-Za-z0-9+/=_-]{16,})"
)

PLACEHOLDER_MARKERS = (
    b"<",
    b">",
    b"example",
    b"placeholder",
    b"your",
    b"xxxx",
    b"...",
    b"replace",
    b"sample",
    b"redacted",
)


class Finding(dict):
    __slots__ = ("path", "line", "rule", "message")

    def __init__(self, path: str, line: int, rule: str, message: str) -> None:
        super().__init__()
        self.path = path
        self.line = line
        self.rule = rule
        self.message = message

    def sort_key(self) -> tuple[str, int, str, str]:
        return (self.path, self.line, self.rule, self.message)


def collect_targets(paths: list[str]) -> list[Path]:
    if paths:
        resolved: list[Path] = []
        for raw in paths:
            target = Path(raw)
            if target.is_dir():
                resolved.extend(sorted(target.rglob("*.md")))
            elif target.is_file():
                resolved.append(target)
        return sorted({p.resolve() for p in resolved})
    defaults = [REPOSITORY_ROOT / "README.md"]
    defaults.extend(sorted((REPOSITORY_ROOT / "docs").rglob("*.md")))
    return sorted({p.resolve() for p in defaults if p.exists()})


def looks_placeholder(value: bytes, line: bytes) -> bool:
    """A credential-shaped value is a placeholder if context marks it so."""
    if value.count(value[:1]) >= len(value):
        return True
    lower = line.lower()
    return any(marker in lower for marker in PLACEHOLDER_MARKERS)


def relative_path(path: Path, root: str) -> str:
    try:
        return str(path.relative_to(root))
    except ValueError:
        return str(path)


def scan_file(path: Path, root: str) -> list[Finding]:
    relative = relative_path(path, root)
    data = path.read_bytes()
    findings: list[Finding] = []
    for line_number, line in enumerate(data.splitlines(), start=1):
        for match in AGE_SECRET_PATTERN.finditer(line):
            findings.append(
                Finding(relative, line_number, "age-secret-key", "age secret identity literal")
            )
        for match in PEM_PRIVATE_PATTERN.finditer(line):
            findings.append(
                Finding(relative, line_number, "private-key", "PEM private key block")
            )
        for match in ASSIGNMENT_PATTERN.finditer(line):
            if looks_placeholder(match.group("value"), line):
                continue
            findings.append(
                Finding(
                    relative,
                    line_number,
                    "assigned-secret",
                    f"credential-shaped value assigned to {match.group('name').decode()}",
                )
            )
    findings.sort(key=Finding.sort_key)
    return findings


def scan(targets: Iterable[Path]) -> list[Finding]:
    findings: list[Finding] = []
    for path in targets:
        try:
            findings.extend(scan_file(path, str(REPOSITORY_ROOT)))
        except OSError as error:
            findings.append(
                Finding(str(path), 1, "unreadable", str(error))
            )
    findings.sort(key=Finding.sort_key)
    return findings


def render(findings: list[Finding]) -> str:
    lines = [f"{finding.path}:{finding.line}: {finding.rule}: {finding.message}" for finding in findings]
    return "\n".join(lines).encode("utf-8", "replace").decode("utf-8")


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Scan for credential-shaped values in Cattery documentation.",
        epilog="With no PATH, scans README.md and docs/**/*.md.",
    )
    parser.add_argument("paths", nargs="*", help="files or directories to scan")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(sys.argv[1:] if argv is None else argv)
    findings = scan(collect_targets(args.paths))
    output = render(findings)
    if output:
        print(output)
        print(f"\n{len(findings)} credential-shaped value(s) found.", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
