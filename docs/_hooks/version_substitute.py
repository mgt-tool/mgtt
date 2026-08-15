"""Build-time hook: substitute {{ MGTT_VERSION }} from the repo's VERSION file.

Wired in mkdocs.yml under `hooks:`. Runs once per page during `mkdocs build`
and `mkdocs serve` — operators editing docs locally see the substitution
immediately.

Why a hook and not a plugin: zero new dependencies, ~15 lines, and the
substitution scope is intentionally tiny (one placeholder).
"""

from pathlib import Path


def _read_version() -> str:
    # mkdocs runs from the repo root; VERSION is one level above docs/.
    repo_root = Path(__file__).resolve().parent.parent.parent
    version_file = repo_root / "VERSION"
    return version_file.read_text().strip()


_VERSION = _read_version()


def on_page_markdown(markdown, **_kwargs):
    return markdown.replace("{{ MGTT_VERSION }}", _VERSION)
