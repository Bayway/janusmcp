#!/usr/bin/env python3
"""Validate the small set of documentation contracts used by JanusMCP CI."""

from pathlib import Path
import re
import sys


ROOT = Path(__file__).resolve().parent.parent
REQUIRED = [
    "CHANGELOG.md",
    "CONTRIBUTING.md",
    "SECURITY.md",
    ".github/pull_request_template.md",
    "docs/README.md",
    "docs/architecture.md",
    "docs/compatibility.md",
    "docs/testing.md",
    "docs/adr/README.md",
    "docs/adr/0000-template.md",
]
ADR_SECTIONS = [
    "## Context",
    "## Decision",
    "## Alternatives considered",
    "## Consequences",
    "## Compatibility and migration",
    "## Security and privacy",
    "## Validation",
]


def fail(message: str) -> None:
    print(f"docs check: {message}", file=sys.stderr)
    raise SystemExit(1)


for relative in REQUIRED:
    if not (ROOT / relative).is_file():
        fail(f"missing required file: {relative}")

adrs = sorted((ROOT / "docs/adr").glob("[0-9][0-9][0-9][0-9]-*.md"))
accepted = [path for path in adrs if path.name != "0000-template.md"]
if not accepted:
    fail("no accepted ADRs found")

for path in accepted:
    text = path.read_text(encoding="utf-8")
    if not re.search(r"^# ADR-[0-9]{4}: .+", text, re.MULTILINE):
        fail(f"invalid ADR title: {path.relative_to(ROOT)}")
    if not re.search(r"^- Status: (Proposed|Accepted|Deprecated|Superseded by ADR-[0-9]{4})$", text, re.MULTILINE):
        fail(f"invalid ADR status: {path.relative_to(ROOT)}")
    if not re.search(r"^- Date: [0-9]{4}-[0-9]{2}-[0-9]{2}$", text, re.MULTILINE):
        fail(f"invalid ADR date: {path.relative_to(ROOT)}")
    for section in ADR_SECTIONS:
        if section not in text:
            fail(f"{path.relative_to(ROOT)} is missing {section}")

pr_template = (ROOT / ".github/pull_request_template.md").read_text(encoding="utf-8")
for section in (
    "## Summary and motivation",
    "## Compatibility and release impact",
    "## Security and privacy",
    "## Validation",
):
    if section not in pr_template:
        fail(f"pull request template is missing {section}")

print(f"docs check: ok ({len(accepted)} ADRs)")
