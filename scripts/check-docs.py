#!/usr/bin/env python3
"""Deterministic documentation checker for Cattery.

Validates Markdown documentation against the frozen Cattery contract
(PLAN.md Section 11):

  * local links resolve to an existing file in the repository;
  * documented commands and flags match the Section 11 vocabulary;
  * no unsupported feature is presented as supported;
  * output is deterministic and UTF-8.

The checker uses only the Python standard library. It never imports project
code, never accesses the network, and never rewrites documentation. It exits
non-zero when any violation is found and zero otherwise.

Usage:
    python3 scripts/check-docs.py [PATH ...]

With no PATH it scans README.md and every *.md file beneath docs/. With one or
more PATH values it scans those files or directories recursively.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path
from typing import Iterable

# Repository root is the parent of the scripts/ directory holding this file.
REPOSITORY_ROOT = Path(__file__).resolve().parents[1]

# Section 11 command surface. --version is intentionally absent: the root
# command leaves Cobra's Version field empty so --version is an unknown flag and
# the version subcommand is the only version interface.
ALLOWED_COMMANDS = frozenset(
    {"init", "validate", "status", "diff", "apply", "add", "version", "help"}
)
ALLOWED_FLAGS = frozenset(
    {
        "--repo",
        "--verbose",
        "--dry-run",
        "--non-interactive",
        "--no-hooks",
        "--group",
        "--platform",
        "--secret",
        "--help",
    }
)

# Features that are explicit Section 1.1 non-goals. They may appear only when
# the surrounding prose makes clear they are unsupported. Terms match as a
# case-insensitive word prefix so plurals and derivations are caught.
BANNED_FEATURES = (
    "template",
    "interpolation",
    "patch language",
    "symlink farm",
    "gnu stow",
    "plugin system",
    "plugin runtime",
    "daemon",
    "file watcher",
    "background sync",
    "automatic sync",
    "bidirectional sync",
    "rollback",
    "prune",
    "package manager",
    "homebrew integration",
    "apt-get",
    "graphical interface",
    "windows support",
)
NEGATION_WORDS = (
    "not",
    "no ",
    "never",
    "without",
    "non-goal",
    "non-goals",
    "unsupported",
    "does not",
    "do not",
    "don't",
    "cannot",
    "can't",
    "rather than",
    "instead of",
    "out of scope",
)

LINK_PATTERN = re.compile(r"\[(?P<label>[^\]]*)\]\((?P<url>[^)]+)\)")
INLINE_CODE_PATTERN = re.compile(r"`([^`\n]+)`")
FLAG_PATTERN = re.compile(r"(--?[A-Za-z][\w-]*)")
PLACEHOLDER_PATTERN = re.compile(r"[<>\[\]|]")
EXTERNAL_URL_PATTERN = re.compile(r"^(?:[a-z][a-z0-9+.-]*://|mailto:)", re.IGNORECASE)


class Finding(dict):
    """A sortable violation record."""

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
    """Resolve the Markdown files to scan from the CLI paths or the default."""
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


def read_lines(path: Path) -> tuple[list[str], bool]:
    """Return the file's lines and whether it decoded as UTF-8 without errors."""
    data = path.read_bytes()
    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError:
        return [], False
    return text.splitlines(), True


def strip_inline_code(line: str) -> str:
    """Replace inline `code` spans with spaces so prose checks ignore them."""
    return INLINE_CODE_PATTERN.sub(lambda match: " " * len(match.group(0)), line)


def extract_code_samples(lines: list[str]) -> list[tuple[int, str]]:
    """Return (starting_line, source) for fenced blocks and each inline span.

    Fenced code blocks are collected whole so a command split across lines is
    checked as one unit; inline `code` spans are returned individually.
    """
    samples: list[tuple[int, str]] = []
    block_start = 0
    block_body: list[str] = []
    in_fenced = False
    for index, line in enumerate(lines, start=1):
        fenced_delimiter = line.lstrip().startswith("```") or line.lstrip().startswith("~~~")
        if in_fenced:
            if fenced_delimiter:
                samples.append((block_start, "\n".join(block_body)))
                block_body = []
                in_fenced = False
            else:
                block_body.append(line)
            continue
        if fenced_delimiter:
            in_fenced = True
            block_start = index
            block_body = []
            continue
        for span in INLINE_CODE_PATTERN.findall(line):
            samples.append((index, span))
    if in_fenced and block_body:
        samples.append((block_start, "\n".join(block_body)))
    return samples


def is_placeholder_token(token: str) -> bool:
    """A bare value placeholder like <name>, [GROUP], or linux|darwin."""
    return bool(PLACEHOLDER_PATTERN.search(token)) or token in {"PATH", "NAME", "FILE", "GROUP", "GROUP ..."}


def check_command_vocabulary(sample: str) -> list[str]:
    """Validate cattery command and flag tokens inside one code sample."""
    problems: list[str] = []
    if "cattery" not in sample.split():
        return problems
    tokens = sample.replace("=", " ").split()
    for token in tokens:
        if token.startswith("--"):
            flag = FLAG_PATTERN.match(token)
            name = flag.group(1) if flag else token
            if name not in ALLOWED_FLAGS:
                problems.append(f"unknown flag {name}")
    for position, token in enumerate(tokens):
        if token != "cattery":
            continue
        following = tokens[position + 1 :]
        subcommand = next((value for value in following if not value.startswith("-")), None)
        if subcommand is None or is_placeholder_token(subcommand):
            continue
        if subcommand not in ALLOWED_COMMANDS:
            problems.append(f"unknown command {subcommand!r}")
    return problems


def check_banned_features(prose_line: str) -> list[str]:
    """Flag a banned feature term used without nearby negation."""
    lowered = prose_line.lower()
    hits: list[str] = []
    if any(negation in lowered for negation in NEGATION_WORDS):
        return hits
    for term in BANNED_FEATURES:
        for match in re.finditer(r"\b" + re.escape(term), lowered):
            window_start = max(0, match.start() - 40)
            window = lowered[window_start : match.end() + 20]
            if any(negation in window for negation in NEGATION_WORDS):
                continue
            hits.append(term)
    return hits


def resolve_link_target(link_source: Path, url: str) -> Path | None:
    """Resolve a local link URL to a filesystem path, or None if external."""
    if not url or url.startswith("#") or EXTERNAL_URL_PATTERN.match(url):
        return None
    target = url.split("#", 1)[0].split("?", 1)[0].strip()
    if not target:
        return None
    if target.startswith("/"):
        return (REPOSITORY_ROOT / target.lstrip("/")).resolve()
    return (link_source.parent / target).resolve()


def relative_path(path: Path, root: str) -> str:
    try:
        return str(path.relative_to(root))
    except ValueError:
        return str(path)


def scan_markdown(path: Path, root: str) -> list[Finding]:
    """Scan one Markdown file and return every finding it produces."""
    findings: list[Finding] = []
    relative = relative_path(path, root)
    lines, valid = read_lines(path)
    if not valid:
        findings.append(
            Finding(relative, 1, "invalid-utf8", "file is not valid UTF-8")
        )
        return findings
    samples = extract_code_samples(lines)
    for start_line, sample in samples:
        for problem in check_command_vocabulary(sample):
            findings.append(Finding(relative, start_line, "command-vocabulary", problem))
    in_fenced = False
    for line_number, line in enumerate(lines, start=1):
        if line.lstrip().startswith("```") or line.lstrip().startswith("~~~"):
            in_fenced = not in_fenced
            continue
        if in_fenced:
            continue
        for match in LINK_PATTERN.finditer(line):
            url = match.group("url").strip()
            target = resolve_link_target(path, url)
            if target is None:
                continue
            if not target.exists():
                findings.append(
                    Finding(
                        relative,
                        line_number,
                        "broken-link",
                        f"link target {url!r} does not exist",
                    )
                )
        prose = strip_inline_code(line)
        for term in check_banned_features(prose):
            findings.append(
                Finding(relative, line_number, "forbidden-feature", f"unsupported feature presented as supported: {term!r}")
            )
    return findings


def scan(targets: Iterable[Path]) -> list[Finding]:
    findings: list[Finding] = []
    for path in targets:
        findings.extend(scan_markdown(path, str(REPOSITORY_ROOT)))
    findings.sort(key=Finding.sort_key)
    return findings


def render(findings: list[Finding]) -> str:
    lines = [f"{finding.path}:{finding.line}: {finding.rule}: {finding.message}" for finding in findings]
    return "\n".join(lines).encode("utf-8", "replace").decode("utf-8")


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Validate Cattery Markdown documentation deterministically.",
        epilog="With no PATH, scans README.md and docs/**/*.md.",
    )
    parser.add_argument("paths", nargs="*", help="Markdown files or directories to scan")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(sys.argv[1:] if argv is None else argv)
    targets = collect_targets(args.paths)
    findings = scan(targets)
    output = render(findings)
    if output:
        print(output)
        print(f"\n{len(findings)} documentation problem(s) found.", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
