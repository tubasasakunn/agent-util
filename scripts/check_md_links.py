#!/usr/bin/env python3
"""Lightweight relative-link checker for Markdown files.

Walks every tracked ``*.md`` file under the repository root, extracts inline
links of the form ``[text](target)``, and verifies that any non-URL target
resolves to a path that exists on disk. URL fragments (``#anchor``) are
allowed and the anchor portion is stripped before existence check. External
links (``http://``, ``https://``, ``mailto:``) are skipped.

Exits with status 1 if any broken relative link is found, otherwise 0.
The CI workflow runs this with ``continue-on-error: true`` so it never
blocks merges; it is intended as a soft signal.
"""

from __future__ import annotations

import os
import re
import sys
from pathlib import Path
from urllib.parse import urlparse

REPO_ROOT = Path(__file__).resolve().parent.parent

# [text](target) — target captured non-greedily, stops at first ')'.
LINK_RE = re.compile(r"\[(?P<text>[^\]]*)\]\((?P<target>[^)\s]+)\)")

# Directories to skip entirely.
SKIP_DIRS = {".git", "node_modules", ".venv", "dist", "build", "__pycache__"}

EXTERNAL_SCHEMES = {"http", "https", "mailto", "ftp", "tel", "data"}


def iter_markdown_files(root: Path):
    for dirpath, dirnames, filenames in os.walk(root):
        # In-place prune so os.walk skips them.
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS]
        for name in filenames:
            if name.endswith(".md"):
                yield Path(dirpath) / name


def is_external(target: str) -> bool:
    parsed = urlparse(target)
    return parsed.scheme.lower() in EXTERNAL_SCHEMES


def check_file(md_path: Path) -> list[str]:
    """Return a list of error messages for broken relative links in ``md_path``."""
    errors: list[str] = []
    try:
        text = md_path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as exc:
        return [f"{md_path}: cannot read ({exc})"]

    for match in LINK_RE.finditer(text):
        target = match.group("target").strip()
        if not target or target.startswith("#"):
            continue  # in-page anchor
        if is_external(target):
            continue
        # Strip ?query and #fragment before resolving on disk.
        path_part = target.split("#", 1)[0].split("?", 1)[0]
        if not path_part:
            continue
        # Resolve relative to the markdown file's directory.
        resolved = (md_path.parent / path_part).resolve()
        if not resolved.exists():
            rel = md_path.relative_to(REPO_ROOT)
            errors.append(f"{rel}: broken link -> {target}")
    return errors


def main() -> int:
    all_errors: list[str] = []
    for md in iter_markdown_files(REPO_ROOT):
        all_errors.extend(check_file(md))

    if all_errors:
        print(f"Found {len(all_errors)} broken Markdown link(s):", file=sys.stderr)
        for err in all_errors:
            print(f"  - {err}", file=sys.stderr)
        return 1

    print("All Markdown relative links resolve.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
