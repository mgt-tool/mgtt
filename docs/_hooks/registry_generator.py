"""Build-time hook: regenerate docs/reference/registry.md from upstream manifest.yamls.

Wired in mkdocs.yml under `hooks:`. Runs once per build via on_pre_build.
"""

from __future__ import annotations

import base64
import json
import os
import re
import sys
import time
import urllib.request
from pathlib import Path

import yaml

REPO_ROOT = Path(__file__).resolve().parent.parent.parent
REGISTRY_YAML = REPO_ROOT / "docs" / "registry.yaml"
REGISTRY_MD = REPO_ROOT / "docs" / "reference" / "registry.md"
CACHE_DIR = REPO_ROOT / ".cache" / "registry-generator"

CACHE_TTL_SECONDS = 60 * 60  # 1 hour


def cached_fetch(key: str, loader):
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    path = CACHE_DIR / key
    if path.exists():
        if time.time() - path.stat().st_mtime < CACHE_TTL_SECONDS:
            return path.read_text()
    data = loader()
    path.write_text(data)
    return data


REGISTRY_MD_PREAMBLE = """# Provider Registry

<!--
GENERATED FILE — do not edit by hand.
Source: docs/registry.yaml (minimal name→URL map) + each provider's upstream
manifest.yaml. Rebuilt by docs/_hooks/registry_generator.py on every
mkdocs build.
-->

Community-maintained providers for mgtt.

The single source of truth for the name→URL map is
[`docs/registry.yaml`](https://github.com/mgt-tool/mgtt/blob/main/docs/registry.yaml).
Per-provider detail below is pulled from each repo's `manifest.yaml` at
its latest `v*` tag on every docs build.

Replace `<digest>` shown in Install commands below with the current
`sha256:…` from your own `docker buildx imagetools inspect` if you need
to double-check.

---

"""


def on_pre_build(config, **_kwargs):
    with REGISTRY_YAML.open() as f:
        entries = load_registry(f)

    offline = os.environ.get("MGTT_REGISTRY_GENERATOR") == "offline"

    sections = [REGISTRY_MD_PREAMBLE]
    for name, entry in entries.items():
        if offline:
            sections.append(_offline_card(name, entry))
            continue
        try:
            ref = resolve_ref(entry["url"], entry["channel"])
            yaml_text = fetch_provider_yaml(entry["url"], ref)
            info = parse_provider(yaml_text)
            image_ref = entry.get("image") or _default_image_ref(entry["url"])
            digest = ""
            if not entry["skip_image"]:
                digest = fetch_image_digest(image_ref, info["version"])
            card = render_card(
                entry_name=name,
                repo_url=entry["url"],
                image_ref=image_ref,
                digest=digest,
                info=info,
                skip_image=entry["skip_image"],
            )
            sections.append(card)
        except Exception as exc:  # noqa: BLE001 — deliberate fail-soft
            print(f"registry-generator: {name}: {exc}", file=sys.stderr)
            sections.append(_error_card(name, entry, exc))

    REGISTRY_MD.write_text("\n".join(sections) + "\n")


def _offline_card(entry_name: str, entry: dict) -> str:
    return (
        f"## {entry_name}\n\n"
        f"[unavailable — registry sync offline]\n\n"
        f"- **Source**: `{entry['url']}`\n"
        "\n---\n"
    )


def _error_card(entry_name: str, entry: dict, err: Exception) -> str:
    return (
        f"## {entry_name}\n\n"
        f":warning: **registry sync failed**: {err}\n\n"
        f"- **Source**: `{entry['url']}`\n"
        "\n---\n"
    )


def _default_image_ref(repo_url: str) -> str:
    owner, repo = _parse_repo(repo_url)
    return f"ghcr.io/{owner}/{repo}"


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

    def _fetch_tags() -> str:
        pages = []
        for page in range(1, 4):  # at most 300 tags; refuse to walk further
            raw = _github_get(f"/repos/{owner}/{repo}/tags?per_page=100&page={page}").decode()
            pages.append(raw)
            page_data = json.loads(raw)
            if len(page_data) < 100:
                break
        merged = []
        for page_raw in pages:
            merged.extend(json.loads(page_raw))
        return json.dumps(merged)

    raw = cached_fetch(f"tags-{owner}-{repo}", _fetch_tags)
    tags = json.loads(raw)
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

    def _fetch() -> str:
        raw = _github_get(f"/repos/{owner}/{repo}/contents/manifest.yaml?ref={ref}")
        obj = json.loads(raw)
        if obj.get("encoding") != "base64":
            raise ValueError(f"{owner}/{repo}@{ref}: unexpected content encoding {obj.get('encoding')!r}")
        if "content" not in obj:
            raise ValueError(f"{owner}/{repo}@{ref}: API response lacks 'content'")
        return base64.b64decode(obj["content"]).decode("utf-8")

    return cached_fetch(f"yaml-{owner}-{repo}-{ref}", _fetch)


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
    owner_repo = path.replace("/", "-")

    def _fetch() -> str:
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

    return cached_fetch(f"digest-{owner_repo}-{tag}", _fetch)


def parse_provider(yaml_text: str) -> dict:
    doc = yaml.safe_load(yaml_text) or {}
    meta = doc.get("meta") or {}
    return {
        "name": meta.get("name", ""),
        "version": str(meta.get("version", "")),
        "description": meta.get("description", ""),
        "tags": list(meta.get("tags") or []),
        "requires_mgtt": (meta.get("requires") or {}).get("mgtt", ""),
        "needs": list(doc.get("needs") or []),
        "network": doc.get("network", ""),
        "read_only": doc.get("read_only", True),
        "writes_note": doc.get("writes_note", ""),
    }


def _namespace_from_url(repo_url: str) -> str:
    owner, _ = _parse_repo(repo_url)
    return owner


def render_card(*, entry_name: str, repo_url: str, image_ref: str,
                digest: str, info: dict, skip_image: bool) -> str:
    owner = _namespace_from_url(repo_url)
    fqn = f"{owner}/{info['name']}@{info['version']}"
    caps = ", ".join(f"`{n}`" for n in info["needs"]) if info["needs"] else "—"
    network = f"`{info['network']}`" if info["network"] and info["network"] != "bridge" else "— (bridge)"
    tags = ", ".join(info["tags"]) if info["tags"] else "—"
    posture = "read-only" if info["read_only"] else "writes"

    parts = [
        f"## {entry_name}",
        "",
        info["description"] or "",
        "",
        f"- **FQN**: `{fqn}`",
        f"- **Capabilities**: {caps} · **Network**: {network}",
        f"- **Posture**: {posture}",
    ]
    if not info["read_only"] and info["writes_note"]:
        first = info["writes_note"].strip().splitlines()[0]
        parts.append(f"- **Writes**: {first}")
    parts.append(f"- **Tags**: {tags}")
    parts.append(f"- **Requires mgtt**: `{info['requires_mgtt']}`")
    parts.append("")
    parts.append("```bash")
    parts.append(f"mgtt provider install {entry_name}")
    parts.append(f"mgtt provider install {owner}/{info['name']}@{info['version']}")
    parts.append(f"mgtt provider install {repo_url}")
    if not skip_image:
        parts.append(f"mgtt provider install --image {image_ref}:{info['version']}@{digest}")
    parts.append("```")
    parts.append("")
    parts.append("---")
    return "\n".join(parts)
