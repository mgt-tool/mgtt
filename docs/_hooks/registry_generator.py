"""Build-time hook: regenerate docs/reference/registry.md from upstream manifest.yamls.

Wired in mkdocs.yml under `hooks:`. Runs once per build via on_pre_build.
"""

from __future__ import annotations

import base64
import json
import os
import re
import subprocess
import sys
import time
import traceback
import urllib.request
from pathlib import Path
from typing import Callable

import yaml

REPO_ROOT = Path(__file__).resolve().parent.parent.parent
REGISTRY_YAML = REPO_ROOT / "docs" / "registry.yaml"
REGISTRY_MD = REPO_ROOT / "docs" / "reference" / "registry.md"
CACHE_DIR = REPO_ROOT / ".cache" / "registry-generator"
CACHE_TTL_SECONDS = 60 * 60  # 1 hour

DEFAULTS = {"channel": "latest-tag", "skip_image": False}

# Strict: only v<major>.<minor>.<patch>. Pre-releases like v1.2.3-rc1 are
# skipped; operators who want them should pin channel: v1.2.3-rc1 explicitly.
_SEMVER_RE = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")

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


# ---- HTTP + cache primitives ------------------------------------------------

def _http_get(url: str, *, headers: dict | None = None, timeout: int = 15) -> bytes:
    req = urllib.request.Request(url, headers=headers or {})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return resp.read()


def _github_base() -> str:
    return os.environ.get("MGTT_REGISTRY_GITHUB_BASE", "https://api.github.com")


def _ghcr_base() -> str:
    return os.environ.get("MGTT_REGISTRY_GHCR_BASE", "https://ghcr.io")


def _github_api(path: str) -> bytes:
    # Deliberately anonymous. The workflow's GITHUB_TOKEN is scoped to the
    # repo running the workflow; sending it to the Contents API of a
    # DIFFERENT repo returns 403 Forbidden. Public-repo anonymous reads
    # don't need auth (rate limit 60/hr, masked by our disk cache). GHCR
    # digest fetches still use GITHUB_TOKEN — that's where scope matters.
    return _http_get(f"{_github_base()}{path}", headers={"Accept": "application/vnd.github+json"})


def cached_fetch(key: str, loader: Callable[[], str]) -> str:
    CACHE_DIR.mkdir(parents=True, exist_ok=True)
    path = CACHE_DIR / key
    if path.exists() and time.time() - path.stat().st_mtime < CACHE_TTL_SECONDS:
        return path.read_text()
    data = loader()
    path.write_text(data)
    return data


# ---- Repo URL parsing -------------------------------------------------------

def _parse_repo(repo_url: str) -> tuple[str, str]:
    m = re.match(r"https?://github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$", repo_url)
    if not m:
        raise ValueError(f"unsupported repo URL: {repo_url!r}")
    return m.group(1), m.group(2)


def _default_image_ref(repo_url: str) -> str:
    owner, repo = _parse_repo(repo_url)
    return f"ghcr.io/{owner}/{repo}"


# ---- Registry YAML loader ---------------------------------------------------

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


# ---- Tag resolution + manifest fetch ---------------------------------------

def resolve_ref(repo_url: str, channel: str) -> str:
    """Resolve the channel spec to a concrete git ref (tag name or branch).

    Uses `git ls-remote --tags` rather than the GitHub REST API because
    GitHub aggressively 403s anonymous API traffic from its own Actions
    runners, and the workflow's GITHUB_TOKEN is scoped to a single repo.
    `git ls-remote` is auth-free for public repos.
    """
    if channel == "main":
        return "main"
    if channel != "latest-tag":
        return channel  # caller passed a specific tag
    owner, repo = _parse_repo(repo_url)

    def _fetch_tags() -> str:
        if os.environ.get("MGTT_REGISTRY_GITHUB_BASE"):
            return _tags_via_github_stub(owner, repo)
        return _tags_via_git_lsremote(repo_url)

    raw = cached_fetch(f"tags-{owner}-{repo}", _fetch_tags)
    best = _highest_semver_tag(raw)
    if best is None:
        raise ValueError(f"{owner}/{repo}: no v* semver tags found")
    return best


def _tags_via_github_stub(owner: str, repo: str) -> str:
    """Test-mode only: pull tags via the stub's Contents API and synthesise
    git-ls-remote-style lines so the caller has one parsing path."""
    lines = []
    for page in range(1, 4):  # at most 300 tags
        data = json.loads(_github_api(f"/repos/{owner}/{repo}/tags?per_page=100&page={page}"))
        for t in data:
            sha = (t.get("commit") or {}).get("sha", "")
            lines.append(f"{sha}\trefs/tags/{t['name']}")
        if len(data) < 100:
            break
    return "\n".join(lines) + ("\n" if lines else "")


def _tags_via_git_lsremote(repo_url: str) -> str:
    """Production: `git ls-remote --tags` prints `<sha>\\trefs/tags/v1.2.3`
    (+ occasional `^{}` dereference lines which _highest_semver_tag drops)."""
    return subprocess.check_output(
        ["git", "ls-remote", "--tags", repo_url],
        timeout=30,
        text=True,
    )


def _highest_semver_tag(raw: str) -> str | None:
    best = None
    for line in raw.splitlines():
        if "\t" not in line:
            continue
        _sha, ref = line.split("\t", 1)
        if not ref.startswith("refs/tags/"):
            continue
        name = ref[len("refs/tags/"):]
        if name.endswith("^{}"):
            continue
        m = _SEMVER_RE.match(name)
        if not m:
            continue
        parts = tuple(int(p) for p in m.groups())
        if best is None or parts > best[0]:
            best = (parts, name)
    return best[1] if best else None


def fetch_provider_yaml(repo_url: str, ref: str) -> str:
    """Fetch manifest.yaml at <ref>.

    Production: raw.githubusercontent.com (CDN, auth-free, not subject
    to REST API 403 rate limits). Test mode (MGTT_REGISTRY_GITHUB_BASE
    set): the stub's Contents API.
    """
    owner, repo = _parse_repo(repo_url)

    def _fetch() -> str:
        if os.environ.get("MGTT_REGISTRY_GITHUB_BASE"):
            return _manifest_via_github_stub(owner, repo, ref)
        return _manifest_via_raw(owner, repo, ref)

    return cached_fetch(f"yaml-{owner}-{repo}-{ref}", _fetch)


def _manifest_via_raw(owner: str, repo: str, ref: str) -> str:
    return _http_get(f"https://raw.githubusercontent.com/{owner}/{repo}/{ref}/manifest.yaml").decode("utf-8")


def _manifest_via_github_stub(owner: str, repo: str, ref: str) -> str:
    obj = json.loads(_github_api(f"/repos/{owner}/{repo}/contents/manifest.yaml?ref={ref}"))
    if obj.get("encoding") != "base64":
        raise ValueError(f"{owner}/{repo}@{ref}: unexpected content encoding {obj.get('encoding')!r}")
    if "content" not in obj:
        raise ValueError(f"{owner}/{repo}@{ref}: API response lacks 'content'")
    return base64.b64decode(obj["content"]).decode("utf-8")


# ---- GHCR digest fetch ------------------------------------------------------

def fetch_image_digest(image_ref: str, tag: str) -> str:
    """Return the sha256 digest ghcr.io computes for <image_ref>:<tag>.

    GHCR requires a bearer token on /v2/ even for public packages. Using
    GITHUB_TOKEN directly as Bearer only works for packages owned by the
    workflow's own repo (cross-repo is 403). The token-exchange endpoint
    accepts GITHUB_TOKEN via Basic auth (NOT Bearer) and mints a scoped,
    package-specific read-only bearer for any publicly visible package.
    """
    prefix = "ghcr.io/"
    if not image_ref.startswith(prefix):
        raise ValueError(f"only ghcr.io images supported for now: {image_ref!r}")
    path = image_ref[len(prefix):]
    cache_key = f"digest-{path.replace('/', '-')}-{tag}"

    def _fetch() -> str:
        headers = {
            "Accept": (
                "application/vnd.docker.distribution.manifest.v2+json, "
                "application/vnd.oci.image.manifest.v1+json, "
                "application/vnd.oci.image.index.v1+json, "
                "application/vnd.docker.distribution.manifest.list.v2+json"
            ),
            "Authorization": f"Bearer {_ghcr_scoped_token(path)}",
        }
        req = urllib.request.Request(f"{_ghcr_base()}/v2/{path}/manifests/{tag}", headers=headers)
        with urllib.request.urlopen(req, timeout=15) as resp:
            digest = resp.headers.get("Docker-Content-Digest")
        if not digest:
            raise ValueError(f"{image_ref}:{tag}: no Docker-Content-Digest header")
        return digest

    return cached_fetch(cache_key, _fetch)


def _ghcr_scoped_token(path: str) -> str:
    headers = {}
    if gh := os.environ.get("GITHUB_TOKEN"):
        basic = base64.b64encode(f"x-access-token:{gh}".encode()).decode()
        headers["Authorization"] = f"Basic {basic}"
    url = f"{_ghcr_base()}/token?service=ghcr.io&scope=repository:{path}:pull"
    token = json.loads(_http_get(url, headers=headers or None)).get("token", "")
    if not token:
        raise ValueError(f"{path}: GHCR token endpoint returned empty token")
    return token


# ---- Provider manifest parsing ---------------------------------------------

def parse_provider(yaml_text: str) -> dict:
    doc = yaml.safe_load(yaml_text) or {}
    meta = doc.get("meta") or {}
    runtime = doc.get("runtime") or {}
    install = doc.get("install") or {}

    needs = runtime.get("needs") or {}
    if isinstance(needs, list):
        needs = {k: "" for k in needs}
    backends = runtime.get("backends") or {}
    if isinstance(backends, list):
        backends = {k: "" for k in backends}

    # `.get()` returns None for missing keys AND explicit null; the
    # truthiness check drops both so `install: {source: null, image: {...}}`
    # doesn't wrongly advertise source.
    methods = []
    if install.get("source"):
        methods.append("source")
    if install.get("image"):
        methods.append("image")

    return {
        "name": meta.get("name", ""),
        "version": str(meta.get("version", "")),
        "description": meta.get("description", ""),
        "tags": list(meta.get("tags") or []),
        "requires_mgtt": (meta.get("requires") or {}).get("mgtt", ""),
        "needs": needs,                                     # dict name→constraint
        "backends": backends,                               # dict name→constraint
        "network_mode": runtime.get("network_mode", ""),
        "read_only": doc.get("read_only", True),
        "writes_note": doc.get("writes_note", ""),
        "methods": methods,                                 # ["source"], ["image"], or both
        "image_repository": (install.get("image") or {}).get("repository", ""),
    }


# ---- Rendering --------------------------------------------------------------

def _format_entry(name: str, constraint: str) -> str:
    if constraint:
        return f"`{name}` `{constraint}`"
    return f"`{name}`"


def render_card(*, entry_name: str, repo_url: str, image_ref: str,
                digest: str, info: dict, skip_image: bool) -> str:
    owner, _ = _parse_repo(repo_url)
    fqn = f"{owner}/{info['name']}@{info['version']}"
    caps_parts = [_format_entry(n, v) for n, v in sorted(info["needs"].items())]
    caps = ", ".join(caps_parts) if caps_parts else "—"

    backend_parts = [_format_entry(n, v) for n, v in sorted(info["backends"].items())]
    backends = ", ".join(backend_parts) if backend_parts else "—"

    methods = ", ".join(f"`{m}`" for m in info["methods"]) or "—"
    network = f"`{info['network_mode']}`" if info["network_mode"] and info["network_mode"] != "bridge" else "— (bridge)"
    tags = ", ".join(info["tags"]) if info["tags"] else "—"
    posture = "read-only" if info["read_only"] else "writes"

    parts = [
        f"## {entry_name}",
        "",
        info["description"] or "",
        "",
        f"- **FQN**: `{fqn}`",
        f"- **Install methods**: {methods}",
        f"- **Capabilities**: {caps} · **Network**: {network}",
        f"- **Backends**: {backends}",
        f"- **Posture**: {posture}",
    ]
    # Guard against whitespace-only writes_note: .strip() would return
    # "" and .splitlines()[0] would IndexError. The Go parser rejects
    # this for valid providers, but harden defensively.
    if not info["read_only"] and info["writes_note"].strip():
        first_line = info["writes_note"].strip().splitlines()[0]
        parts.append(f"- **Writes**: {first_line}")
    parts += [
        f"- **Tags**: {tags}",
        f"- **Requires mgtt**: `{info['requires_mgtt']}`",
        "",
        "```bash",
        f"mgtt provider install {entry_name}",
        f"mgtt provider install {owner}/{info['name']}@{info['version']}",
        f"mgtt provider install {repo_url}",
    ]
    if not skip_image:
        # Digest unavailable (private GHCR package, etc.) — leave the
        # `<digest>` placeholder; the preamble tells operators to
        # substitute their own `docker buildx imagetools inspect` output.
        suffix = f"@{digest}" if digest else "@<digest>"
        parts.append(f"mgtt provider install --image {image_ref}:{info['version']}{suffix}")
    parts += ["```", "", "---"]
    return "\n".join(parts)


def _placeholder_card(entry_name: str, entry: dict, message: str) -> str:
    return (
        f"## {entry_name}\n\n"
        f"{message}\n\n"
        f"- **Source**: `{entry['url']}`\n"
        "\n---\n"
    )


# ---- Orchestration ----------------------------------------------------------

def on_pre_build(config, **_kwargs):
    with REGISTRY_YAML.open() as f:
        entries = load_registry(f)
    offline = os.environ.get("MGTT_REGISTRY_GENERATOR") == "offline"
    sections = [REGISTRY_MD_PREAMBLE] + [_render_entry(n, e, offline) for n, e in entries.items()]
    REGISTRY_MD.write_text("\n".join(sections) + "\n")


def _render_entry(name: str, entry: dict, offline: bool) -> str:
    if offline:
        return _placeholder_card(name, entry, "[unavailable — registry sync offline]")
    try:
        ref = resolve_ref(entry["url"], entry["channel"])
        info = parse_provider(fetch_provider_yaml(entry["url"], ref))
        image_ref = info.get("image_repository") or entry.get("image") or _default_image_ref(entry["url"])
        digest = "" if entry["skip_image"] else _fetch_digest_soft(name, image_ref, info["version"])
        return render_card(
            entry_name=name, repo_url=entry["url"], image_ref=image_ref,
            digest=digest, info=info, skip_image=entry["skip_image"],
        )
    except Exception as exc:  # noqa: BLE001 — deliberate fail-soft
        _log_failure(name, exc)
        return _placeholder_card(name, entry, f":warning: **registry sync failed**: {exc}")


def _fetch_digest_soft(name: str, image_ref: str, version: str) -> str:
    """Private GHCR packages, token-exchange failures, etc. shouldn't drop
    the whole card — the preamble already tells operators to fill in their
    own digest. Swallow and log; return "" so render_card emits <digest>."""
    try:
        return fetch_image_digest(image_ref, version)
    except Exception as exc:  # noqa: BLE001
        print(
            f"registry-generator: {name}: digest fetch skipped: "
            f"{type(exc).__name__}: {exc}",
            file=sys.stderr,
        )
        return ""


def _log_failure(name: str, exc: Exception) -> None:
    print(f"registry-generator: {name}: {type(exc).__name__}: {exc}", file=sys.stderr)
    # Surface the deepest frame inside registry_generator.py so we know
    # which fetch actually raised — the urllib-internal leaf frame isn't
    # useful.
    for frame in reversed(traceback.extract_tb(exc.__traceback__)):
        if "registry_generator" in frame.filename:
            print(
                f"registry-generator: {name}:   in {frame.name} "
                f"({frame.filename}:{frame.lineno})",
                file=sys.stderr,
            )
            break
