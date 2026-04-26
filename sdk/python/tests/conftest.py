"""Shared pytest fixtures for the ai-agent SDK tests.

Configures pytest-asyncio in auto mode (so plain ``async def`` tests work
without explicit markers) and adds the repository ``sdk/python`` directory to
``sys.path`` for in-tree imports.
"""

from __future__ import annotations

import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))
