"""Build-time hook: regenerate docs/reference/registry.md from upstream provider.yamls.

Wired in mkdocs.yml under `hooks:`. Runs once per build via on_pre_build.
"""

from __future__ import annotations

import base64
import json
import os
import re
import urllib.request
from pathlib import Path

import yaml

REPO_ROOT = Path(__file__).resolve().parent.parent.parent
REGISTRY_YAML = REPO_ROOT / "docs" / "registry.yaml"
REGISTRY_MD = REPO_ROOT / "docs" / "reference" / "registry.md"
CACHE_DIR = REPO_ROOT / ".cache" / "registry-generator"


def on_pre_build(config, **_kwargs):
    """mkdocs hook entry point."""
    raise NotImplementedError("wired in later tasks")


DEFAULTS = {"channel": "latest-tag", "skip_image": False}


def load_registry(stream) -> dict[str, dict]:
    data = yaml.safe_load(stream) or {}
    providers = data.get("providers") or {}
    out = {}
    for name, entry in providers.items():
        merged = {**DEFAULTS, **(entry or {})}
        if "url" not in merged:
            raise ValueError(f"registry entry {name!r}: url is required")
        out[name] = merged
    return out


def _github_base() -> str:
    return os.environ.get("MGTT_REGISTRY_GITHUB_BASE", "https://api.github.com")


def _parse_repo(repo_url: str) -> tuple[str, str]:
    m = re.match(r"https?://github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$", repo_url)
    if not m:
        raise ValueError(f"unsupported repo URL: {repo_url!r}")
    return m.group(1), m.group(2)


def _github_get(path: str) -> bytes:
    req = urllib.request.Request(f"{_github_base()}{path}")
    req.add_header("Accept", "application/vnd.github+json")
    if token := os.environ.get("GITHUB_TOKEN"):
        req.add_header("Authorization", f"Bearer {token}")
    with urllib.request.urlopen(req, timeout=15) as resp:
        return resp.read()


# Strict: only v<major>.<minor>.<patch> — no pre-release/build suffixes.
# Pre-releases like v1.2.3-rc1 are skipped; operators who want them should
# pin channel: v1.2.3-rc1 explicitly.
_SEMVER_RE = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")


def resolve_ref(repo_url: str, channel: str) -> str:
    """Resolve the channel spec to a concrete git ref (tag name or branch)."""
    if channel == "main":
        return "main"
    if channel != "latest-tag":
        return channel  # caller passed a specific tag
    owner, repo = _parse_repo(repo_url)
    tags: list[dict] = []
    for page in range(1, 4):  # at most 300 tags; refuse to walk further
        raw = _github_get(f"/repos/{owner}/{repo}/tags?per_page=100&page={page}")
        page_tags = json.loads(raw)
        if not page_tags:
            break
        tags.extend(page_tags)
        if len(page_tags) < 100:
            break
    best = None
    for t in tags:
        name = t["name"]
        m = _SEMVER_RE.match(name)
        if not m:
            continue
        parts = tuple(int(p) for p in m.groups())
        if best is None or parts > best[0]:
            best = (parts, name)
    if best is None:
        raise ValueError(f"{owner}/{repo}: no v* semver tags found")
    return best[1]


def fetch_provider_yaml(repo_url: str, ref: str) -> str:
    owner, repo = _parse_repo(repo_url)
    raw = _github_get(f"/repos/{owner}/{repo}/contents/provider.yaml?ref={ref}")
    obj = json.loads(raw)
    if obj.get("encoding") != "base64":
        raise ValueError(f"{owner}/{repo}@{ref}: unexpected content encoding {obj.get('encoding')!r}")
    if "content" not in obj:
        raise ValueError(f"{owner}/{repo}@{ref}: API response lacks 'content'")
    return base64.b64decode(obj["content"]).decode("utf-8")


def _ghcr_base() -> str:
    return os.environ.get("MGTT_REGISTRY_GHCR_BASE", "https://ghcr.io")


def fetch_image_digest(image_ref: str, tag: str) -> str:
    """Return the sha256 digest ghcr.io computes for <image_ref>:<tag>.

    image_ref is the registry/org/repo portion (no tag). Example:
    ghcr.io/mgt-tool/mgtt-provider-tempo → queries
    <base>/v2/mgt-tool/mgtt-provider-tempo/manifests/<tag>.
    """
    prefix = "ghcr.io/"
    if not image_ref.startswith(prefix):
        raise ValueError(f"only ghcr.io images supported for now: {image_ref!r}")
    path = image_ref[len(prefix):]
    url = f"{_ghcr_base()}/v2/{path}/manifests/{tag}"
    req = urllib.request.Request(url)
    req.add_header(
        "Accept",
        "application/vnd.docker.distribution.manifest.v2+json, "
        "application/vnd.oci.image.manifest.v1+json",
    )
    if token := os.environ.get("GITHUB_TOKEN"):
        req.add_header("Authorization", f"Bearer {token}")
    with urllib.request.urlopen(req, timeout=15) as resp:
        digest = resp.headers.get("Docker-Content-Digest")
    if not digest:
        raise ValueError(f"{image_ref}:{tag}: no Docker-Content-Digest header")
    return digest
