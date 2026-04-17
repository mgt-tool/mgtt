"""Build-time hook: regenerate docs/reference/registry.md from upstream provider.yamls.

Wired in mkdocs.yml under `hooks:`. Runs once per build via on_pre_build.
"""

from __future__ import annotations

from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent.parent
REGISTRY_YAML = REPO_ROOT / "docs" / "registry.yaml"
REGISTRY_MD = REPO_ROOT / "docs" / "reference" / "registry.md"
CACHE_DIR = REPO_ROOT / ".cache" / "registry-generator"


def on_pre_build(config, **_kwargs):
    """mkdocs hook entry point."""
    raise NotImplementedError("wired in later tasks")
