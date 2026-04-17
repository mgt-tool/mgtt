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
                try:
                    digest = fetch_image_digest(image_ref, info["version"])
                except Exception as digest_exc:  # noqa: BLE001
                    # Private GHCR packages, token-exchange failure, etc.
                    # Better to render the card without the digest than
                    # drop the whole provider from the registry page.
                    print(
                        f"registry-generator: {name}: digest fetch skipped: "
                        f"{type(digest_exc).__name__}: {digest_exc}",
                        file=sys.stderr,
                    )
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
            import traceback
            print(f"registry-generator: {name}: {type(exc).__name__}: {exc}", file=sys.stderr)
            tb = traceback.extract_tb(exc.__traceback__)
            # Surface the deepest frame inside registry_generator.py so we
            # know which fetch (resolve_ref / fetch_provider_yaml /
            # fetch_image_digest) actually raised — the urllib-internal
            # leaf frame is not useful.
            for frame in reversed(tb):
                if "registry_generator" in frame.filename:
                    print(
                        f"registry-generator: {name}:   in {frame.name} "
                        f"({frame.filename}:{frame.lineno})",
                        file=sys.stderr,
                    )
                    break
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
    # Deliberately anonymous. The workflow's GITHUB_TOKEN is scoped to the
    # repo running the workflow; sending it to the Contents API of a
    # DIFFERENT repo returns 403 Forbidden. Public-repo anonymous reads
    # don't need auth (rate limit 60/hr, masked by our disk cache). GHCR
    # digest fetches still use GITHUB_TOKEN — that's where the scope
    # actually matters.
    req = urllib.request.Request(f"{_github_base()}{path}")
    req.add_header("Accept", "application/vnd.github+json")
    with urllib.request.urlopen(req, timeout=15) as resp:
        return resp.read()


# Strict: only v<major>.<minor>.<patch> — no pre-release/build suffixes.
# Pre-releases like v1.2.3-rc1 are skipped; operators who want them should
# pin channel: v1.2.3-rc1 explicitly.
_SEMVER_RE = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")


def resolve_ref(repo_url: str, channel: str) -> str:
    """Resolve the channel spec to a concrete git ref (tag name or branch).

    Uses `git ls-remote --tags` rather than the GitHub REST API because
    GitHub aggressively 403s anonymous API traffic from its own Actions
    runners, and the workflow's GITHUB_TOKEN is scoped to a single repo.
    `git ls-remote` is auth-free for public repos and is what git itself
    uses for tag discovery.
    """
    if channel == "main":
        return "main"
    if channel != "latest-tag":
        return channel  # caller passed a specific tag
    owner, repo = _parse_repo(repo_url)

    def _fetch_tags() -> str:
        # Test mode: MGTT_REGISTRY_GITHUB_BASE indicates the e2e stub
        # server is handling traffic. Hit its Contents API /tags path
        # (JSON shape) so the stub doesn't need to speak the git wire
        # protocol.
        if os.environ.get("MGTT_REGISTRY_GITHUB_BASE"):
            pages = []
            for page in range(1, 4):  # at most 300 tags
                raw = _github_get(f"/repos/{owner}/{repo}/tags?per_page=100&page={page}").decode()
                pages.append(raw)
                page_data = json.loads(raw)
                if len(page_data) < 100:
                    break
            # Convert to git-ls-remote text shape so the parser below
            # handles stub + real git output with one code path.
            lines = []
            for page_raw in pages:
                for t in json.loads(page_raw):
                    lines.append(f"{t.get('commit',{}).get('sha','')}\trefs/tags/{t['name']}")
            return "\n".join(lines) + ("\n" if lines else "")

        # Production: git ls-remote --tags prints lines like
        #   <sha>\trefs/tags/v1.2.3
        # (+ occasionally a ^{} dereference line for annotated tags;
        # we skip those — the object they point at is the same tag name.)
        import subprocess
        out = subprocess.check_output(
            ["git", "ls-remote", "--tags", repo_url],
            timeout=30,
            text=True,
        )
        return out

    raw = cached_fetch(f"tags-{owner}-{repo}", _fetch_tags)
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
    if best is None:
        raise ValueError(f"{owner}/{repo}: no v* semver tags found")
    return best[1]


def fetch_provider_yaml(repo_url: str, ref: str) -> str:
    """Fetch manifest.yaml at <ref> via raw.githubusercontent.com.

    Raw is CDN-backed, auth-free for public repos, and isn't subject
    to the same 403 rate limits as the REST API. If the workflow runs
    against the test stub, MGTT_REGISTRY_GITHUB_BASE points the call
    at the stub's Contents API endpoint instead.
    """
    owner, repo = _parse_repo(repo_url)

    def _fetch() -> str:
        # In production: hit raw.githubusercontent.com. In tests: hit the
        # Contents API stub via MGTT_REGISTRY_GITHUB_BASE (same behavior
        # the stub already mocks).
        if os.environ.get("MGTT_REGISTRY_GITHUB_BASE"):
            raw = _github_get(f"/repos/{owner}/{repo}/contents/manifest.yaml?ref={ref}")
            obj = json.loads(raw)
            if obj.get("encoding") != "base64":
                raise ValueError(f"{owner}/{repo}@{ref}: unexpected content encoding {obj.get('encoding')!r}")
            if "content" not in obj:
                raise ValueError(f"{owner}/{repo}@{ref}: API response lacks 'content'")
            return base64.b64decode(obj["content"]).decode("utf-8")
        url = f"https://raw.githubusercontent.com/{owner}/{repo}/{ref}/manifest.yaml"
        req = urllib.request.Request(url)
        with urllib.request.urlopen(req, timeout=15) as resp:
            return resp.read().decode("utf-8")

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
        # GHCR requires a bearer token on /v2/ even for public packages.
        # Using GITHUB_TOKEN directly as Bearer only works for packages
        # owned by the workflow's own repo (cross-repo is 403). The
        # token-exchange endpoint, however, accepts GITHUB_TOKEN via
        # Basic auth (NOT Bearer) and mints a scoped, package-specific
        # read-only bearer for any publicly visible package.
        token_url = (
            f"{_ghcr_base()}/token?service=ghcr.io&scope=repository:{path}:pull"
        )
        treq = urllib.request.Request(token_url)
        if gh := os.environ.get("GITHUB_TOKEN"):
            basic = base64.b64encode(f"x-access-token:{gh}".encode()).decode()
            treq.add_header("Authorization", f"Basic {basic}")
        with urllib.request.urlopen(treq, timeout=15) as tresp:
            token = json.loads(tresp.read()).get("token", "")
        if not token:
            raise ValueError(f"{image_ref}: GHCR token endpoint returned empty token")

        url = f"{_ghcr_base()}/v2/{path}/manifests/{tag}"
        req = urllib.request.Request(url)
        req.add_header(
            "Accept",
            "application/vnd.docker.distribution.manifest.v2+json, "
            "application/vnd.oci.image.manifest.v1+json, "
            "application/vnd.oci.image.index.v1+json, "
            "application/vnd.docker.distribution.manifest.list.v2+json",
        )
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
        # When digest is unavailable (private GHCR package, etc.) leave
        # the placeholder text `<digest>` — the preamble already tells
        # operators to substitute their own `docker buildx imagetools
        # inspect` output.
        suffix = f"@{digest}" if digest else "@<digest>"
        parts.append(f"mgtt provider install --image {image_ref}:{info['version']}{suffix}")
    parts.append("```")
    parts.append("")
    parts.append("---")
    return "\n".join(parts)
