"""End-to-end test for the registry generator.

Spins up an http.server on a random port that mimics the GitHub REST API
and the GHCR Docker Registry v2 API, points the generator at it via env
vars, runs on_pre_build(), and asserts the rendered markdown.
"""

from __future__ import annotations

import base64
import http.server
import io
import json
import os
import shutil
import socketserver
import tempfile
import threading
import unittest
from pathlib import Path

import registry_generator as rg
from registry_generator import (
    cached_fetch,
    fetch_image_digest,
    fetch_provider_yaml,
    load_registry,
    on_pre_build,
    parse_provider,
    render_card,
    resolve_ref,
)


# ---- Stub HTTP server + test fixtures --------------------------------------

_TEMPO_YAML = (
    "meta:\n"
    "  name: tempo\n"
    "  version: 0.2.1\n"
    "  description: Per-span SLO checks against Grafana Tempo\n"
    "  tags: [tracing, otel]\n"
    "  requires:\n"
    "    mgtt: \">=0.2.0\"\n"
    "runtime:\n"
    "  network_mode: host\n"
    "install:\n"
    "  source:\n"
    "    build: hooks/install.sh\n"
    "    clean: hooks/uninstall.sh\n"
    "  image:\n"
    "    repository: ghcr.io/mgt-tool/mgtt-provider-tempo\n"
)

_DOCKER_YAML = (
    "meta:\n"
    "  name: docker\n"
    "  version: 1.4.0\n"
    "  description: Container health via the Docker Engine API\n"
    "  tags: [containers]\n"
    "  requires:\n"
    "    mgtt: \">=0.3.0\"\n"
    "runtime:\n"
    "  needs: [docker]\n"
    "install:\n"
    "  source:\n"
    "    build: hooks/install.sh\n"
)

# repo name -> manifest served by the stub. Distinct per repo so a bug that
# renders one provider's data under another's heading can't hide.
_MANIFESTS = {
    "mgtt-provider-tempo": _TEMPO_YAML,
    "mgtt-provider-docker": _DOCKER_YAML,
}

_TEST_REGISTRY_YAML = (
    "providers:\n"
    "  tempo:  {url: https://github.com/mgt-tool/mgtt-provider-tempo}\n"
    "  docker: {url: https://github.com/mgt-tool/mgtt-provider-docker, skip_image: true}\n"
)


# ---- Isolation -------------------------------------------------------------
#
# on_pre_build() writes REGISTRY_MD and cached_fetch() writes CACHE_DIR. Both
# default to real repo paths, so without this redirect a test run overwrote
# the committed docs/reference/registry.md with stub data and seeded the real
# build cache with it.

_TMP: tempfile.TemporaryDirectory | None = None
_SAVED_PATHS: dict[str, Path] = {}


def setUpModule():
    global _TMP
    _TMP = tempfile.TemporaryDirectory(prefix="registry-generator-test-")
    tmp = Path(_TMP.name)
    for attr in ("REGISTRY_YAML", "REGISTRY_MD", "CACHE_DIR"):
        _SAVED_PATHS[attr] = getattr(rg, attr)
    (tmp / "registry.yaml").write_text(_TEST_REGISTRY_YAML)
    rg.REGISTRY_YAML = tmp / "registry.yaml"
    rg.REGISTRY_MD = tmp / "registry.md"
    rg.CACHE_DIR = tmp / "cache"


def tearDownModule():
    for attr, value in _SAVED_PATHS.items():
        setattr(rg, attr, value)
    _TMP.cleanup()


def start_stub_server():
    """Boots a local HTTP server returning hardcoded GitHub + GHCR responses."""
    server = socketserver.TCPServer(("127.0.0.1", 0), _StubHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, thread, f"http://127.0.0.1:{server.server_address[1]}"


class _StubHandler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):  # silence test output
        pass

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        query = self.path.split("?", 1)[1] if "?" in self.path else ""
        if path.endswith("/tags"):
            # Paginated: return tags on page=1, empty on higher pages.
            if "page=1" in query or "page=" not in query:
                body = json.dumps([{"name": "v0.2.0"}, {"name": "v0.1.0"}]).encode()
            else:
                body = b"[]"
            self._respond(200, body, "application/json")
        elif "/contents/manifest.yaml" in self.path:
            # /github/repos/<owner>/<repo>/contents/manifest.yaml
            repo = path.split("/repos/", 1)[1].split("/")[1]
            manifest = _MANIFESTS.get(repo)
            if manifest is None:
                self._respond(404, b"", "text/plain")
                return
            body = json.dumps({
                "content": base64.b64encode(manifest.encode()).decode(),
                "encoding": "base64",
            }).encode()
            self._respond(200, body, "application/json")
        elif path.endswith("/token"):
            self._respond(200, json.dumps({"token": "stub-ghcr-token"}).encode(), "application/json")
        elif "/manifests/" in self.path:
            self._respond(
                200, b"{}",
                "application/vnd.docker.distribution.manifest.v2+json",
                extra_headers={"Docker-Content-Digest": "sha256:deadbeef"},
            )
        else:
            self._respond(404, b"", "text/plain")

    def _respond(self, status, body, content_type, extra_headers=None):
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        for k, v in (extra_headers or {}).items():
            self.send_header(k, v)
        self.end_headers()
        self.wfile.write(body)


class _StubServerTestCase(unittest.TestCase):
    """Base for tests that need the local stub server and one or more env
    vars pointing at it. Subclasses override `ENV_VARS` with the suffix
    (e.g. "/github") to attach to the stub base URL."""

    ENV_VARS: dict[str, str] = {}

    def setUp(self):
        self._server, self._thread, base = start_stub_server()
        self.addCleanup(self._thread.join)
        self.addCleanup(self._server.server_close)
        self.addCleanup(self._server.shutdown)
        for var, suffix in self.ENV_VARS.items():
            os.environ[var] = base + suffix
            self.addCleanup(os.environ.pop, var, None)


def _clear_cache():
    shutil.rmtree(rg.CACHE_DIR, ignore_errors=True)


# ---- Tests -----------------------------------------------------------------

class RegistryGeneratorE2E(_StubServerTestCase):
    ENV_VARS = {"MGTT_REGISTRY_GITHUB_BASE": "/github", "MGTT_REGISTRY_GHCR_BASE": "/ghcr"}

    def setUp(self):
        super().setUp()
        self.addCleanup(_clear_cache)

    def test_renders_one_provider_card(self):
        on_pre_build(config=None)
        rendered = rg.REGISTRY_MD.read_text()
        self.assertIn("## tempo", rendered)
        self.assertIn("0.2.1", rendered)
        self.assertIn("sha256:deadbeef", rendered)
        self.assertIn("mgt-tool/tempo@0.2.1", rendered)

    def test_each_provider_renders_its_own_manifest(self):
        """Regression: every card in registry.md once showed tempo's
        description and install line, because the test stub served tempo's
        manifest for every repo and on_pre_build wrote into the committed
        file. Two providers with different manifests must render different
        cards, each under its own heading."""
        on_pre_build(config=None)
        cards = _split_cards(rg.REGISTRY_MD.read_text())
        self.assertEqual(set(cards), {"tempo", "docker"})

        self.assertIn("Per-span SLO checks against Grafana Tempo", cards["tempo"])
        self.assertIn("mgtt provider install mgt-tool/tempo@0.2.1", cards["tempo"])
        self.assertNotIn("docker", cards["tempo"].replace("## tempo", ""))

        self.assertIn("Container health via the Docker Engine API", cards["docker"])
        self.assertIn("mgtt provider install mgt-tool/docker@1.4.0", cards["docker"])
        self.assertIn("`>=0.3.0`", cards["docker"])
        self.assertNotIn("Tempo", cards["docker"])
        self.assertNotIn("tempo", cards["docker"])
        self.assertNotIn("--image", cards["docker"])  # skip_image honoured per entry

        self.assertNotEqual(cards["tempo"].replace("tempo", ""), cards["docker"].replace("docker", ""))

    def test_does_not_touch_committed_files(self):
        self.assertNotEqual(rg.REGISTRY_MD, _SAVED_PATHS["REGISTRY_MD"])
        self.assertNotEqual(rg.CACHE_DIR, _SAVED_PATHS["CACHE_DIR"])
        before = _SAVED_PATHS["REGISTRY_MD"].read_bytes()
        on_pre_build(config=None)
        self.assertEqual(_SAVED_PATHS["REGISTRY_MD"].read_bytes(), before)


def _split_cards(markdown: str) -> dict[str, str]:
    """Map `## <name>` heading -> that card's text (up to the next heading)."""
    cards: dict[str, str] = {}
    current = None
    for line in markdown.splitlines():
        if line.startswith("## "):
            current = line[3:].strip()
            cards[current] = ""
        if current is not None:
            cards[current] += line + "\n"
    return cards


class LoadRegistryTest(unittest.TestCase):
    def test_parses_minimal_entries(self):
        entries = load_registry(io.StringIO(
            "providers:\n"
            "  tempo: {url: https://github.com/mgt-tool/mgtt-provider-tempo}\n"
            "  docker: {url: https://github.com/mgt-tool/mgtt-provider-docker, channel: main}\n"
        ))
        self.assertEqual(entries["tempo"]["url"], "https://github.com/mgt-tool/mgtt-provider-tempo")
        self.assertEqual(entries["tempo"]["channel"], "latest-tag")  # default
        self.assertEqual(entries["docker"]["channel"], "main")

    def test_missing_url_raises(self):
        with self.assertRaisesRegex(ValueError, r"url is required"):
            load_registry(io.StringIO("providers:\n  bad: {channel: main}\n"))


class GitHubFetchTest(_StubServerTestCase):
    ENV_VARS = {"MGTT_REGISTRY_GITHUB_BASE": "/github"}

    def test_latest_tag_resolves_highest_semver(self):
        ref = resolve_ref("https://github.com/mgt-tool/mgtt-provider-tempo", "latest-tag")
        self.assertEqual(ref, "v0.2.0")

    def test_explicit_tag_passthrough(self):
        self.assertEqual(resolve_ref("https://github.com/x/y", "v1.2.3"), "v1.2.3")

    def test_main_channel_resolves_to_branch_name(self):
        self.assertEqual(resolve_ref("https://github.com/x/y", "main"), "main")

    def test_fetch_provider_yaml(self):
        text = fetch_provider_yaml("https://github.com/mgt-tool/mgtt-provider-tempo", "v0.2.0")
        self.assertIn("name: tempo", text)
        self.assertIn("version: 0.2.1", text)


class GHCRDigestTest(_StubServerTestCase):
    ENV_VARS = {"MGTT_REGISTRY_GHCR_BASE": "/ghcr"}

    def test_digest_from_manifest_header(self):
        digest = fetch_image_digest("ghcr.io/mgt-tool/mgtt-provider-tempo", "0.2.0")
        self.assertEqual(digest, "sha256:deadbeef")


class ParseProviderTest(unittest.TestCase):
    def test_null_install_subblock_not_advertised(self):
        """A manifest with `install: {source: null, image: {...}}` must
        not list 'source' as an offered method — the subblock being
        explicitly null means the author opted out."""
        yml = (
            "meta:\n"
            "  name: x\n"
            "  version: 0.1.0\n"
            "  description: d\n"
            "install:\n"
            "  source: ~\n"
            "  image:\n"
            "    repository: ghcr.io/x/y\n"
        )
        info = parse_provider(yml)
        self.assertEqual(info["methods"], ["image"])

    def test_extracts_meta_and_runtime_fields(self):
        yml = (
            "meta:\n"
            "  name: tempo\n"
            "  version: 0.2.1\n"
            "  description: Per-span SLO checks\n"
            "  tags: [tracing, otel]\n"
            "  requires:\n"
            "    mgtt: \">=0.2.0\"\n"
            "runtime:\n"
            "  needs:\n"
            "    kubectl: \">=1.25\"\n"
            "  network_mode: host\n"
            "install:\n"
            "  source:\n"
            "    build: hooks/install.sh\n"
            "    clean: hooks/uninstall.sh\n"
            "read_only: false\n"
            "writes_note: writes state\n"
        )
        info = parse_provider(yml)
        self.assertEqual(info["name"], "tempo")
        self.assertEqual(info["version"], "0.2.1")
        self.assertEqual(info["needs"], {"kubectl": ">=1.25"})
        self.assertEqual(info["network_mode"], "host")
        self.assertEqual(info["methods"], ["source"])
        self.assertFalse(info["read_only"])
        self.assertIn("state", info["writes_note"])


class RenderCardTest(unittest.TestCase):
    def _info(self, **overrides):
        base = {
            "name": "x", "version": "0.1.0", "description": "x",
            "tags": [], "requires_mgtt": ">=0.1.0", "needs": {},
            "backends": {}, "network_mode": "", "read_only": True,
            "writes_note": "", "methods": [], "image_repository": "",
        }
        base.update(overrides)
        return base

    def test_renders_full_card(self):
        info = self._info(
            name="tempo", version="0.2.1", description="Per-span SLO checks",
            tags=["tracing", "otel"], network_mode="host",
            methods=["source", "image"], backends={"tempo": ">=2.4"},
        )
        text = render_card(
            entry_name="tempo",
            repo_url="https://github.com/mgt-tool/mgtt-provider-tempo",
            image_ref="ghcr.io/mgt-tool/mgtt-provider-tempo",
            digest="sha256:deadbeef", info=info, skip_image=False,
        )
        self.assertIn("## tempo", text)
        self.assertIn("mgt-tool/tempo@0.2.1", text)
        self.assertIn("**Network**: `host`", text)
        self.assertIn("**Install methods**: `source`, `image`", text)
        self.assertIn("**Backends**: `tempo` `>=2.4`", text)
        self.assertIn("sha256:deadbeef", text)
        self.assertIn("mgtt provider install tempo", text)
        self.assertIn("https://github.com/mgt-tool/mgtt-provider-tempo", text)

    def test_render_card_handles_read_only_false(self):
        info = self._info(
            name="terraform", description="Terraform state",
            needs={"terraform": ""}, network_mode="host", methods=["source"],
            read_only=False, writes_note="refreshes state on plan",
        )
        text = render_card(
            entry_name="terraform",
            repo_url="https://github.com/mgt-tool/mgtt-provider-terraform",
            image_ref="ghcr.io/mgt-tool/mgtt-provider-terraform",
            digest="sha256:abc", info=info, skip_image=False,
        )
        self.assertIn("refreshes state on plan", text)
        self.assertIn("**Posture**: writes", text)

    def test_render_card_skip_image(self):
        text = render_card(
            entry_name="x", repo_url="https://github.com/x/x",
            image_ref="", digest="", info=self._info(), skip_image=True,
        )
        self.assertNotIn("--image", text)
        self.assertNotIn("sha256:", text)


class CacheTest(unittest.TestCase):
    def setUp(self):
        _clear_cache()

    def test_cached_fetch_hits_once(self):
        """Second call for the same key must be served from disk cache."""
        calls = []
        def fake_fetch():
            calls.append(1)
            return "payload"
        a = cached_fetch("k-123", fake_fetch)
        b = cached_fetch("k-123", fake_fetch)
        self.assertEqual(a, b)
        self.assertEqual(len(calls), 1)

    def test_cache_expires(self):
        key = "k-expire"
        cached_fetch(key, lambda: "old")
        # Backdate mtime so TTL check fails.
        os.utime(rg.CACHE_DIR / key, (0, 0))  # epoch — >> 1 hour ago
        got = cached_fetch(key, lambda: "new")
        self.assertEqual(got, "new")


    def test_stub_mode_does_not_share_cache_with_production(self):
        """Responses fetched from a stub upstream must never be served to a
        real build (and vice versa), even under the same logical key."""
        cached_fetch("yaml-o-r-v1", lambda: "real")
        os.environ["MGTT_REGISTRY_GITHUB_BASE"] = "http://127.0.0.1:9"
        self.addCleanup(os.environ.pop, "MGTT_REGISTRY_GITHUB_BASE", None)
        self.assertEqual(cached_fetch("yaml-o-r-v1", lambda: "stub"), "stub")
        os.environ.pop("MGTT_REGISTRY_GITHUB_BASE")
        self.assertEqual(cached_fetch("yaml-o-r-v1", lambda: "refetched"), "real")


class OfflineModeTest(unittest.TestCase):
    def setUp(self):
        os.environ["MGTT_REGISTRY_GENERATOR"] = "offline"
        self.addCleanup(os.environ.pop, "MGTT_REGISTRY_GENERATOR", None)
        self.addCleanup(_clear_cache)

    def test_offline_renders_placeholder(self):
        on_pre_build(config=None)
        self.assertIn("[unavailable — registry sync offline]", rg.REGISTRY_MD.read_text())


class FailSoftTest(unittest.TestCase):
    def setUp(self):
        # No stub server. Point the GitHub base at a port that's not
        # listening so real fetches fail with ConnectionRefused.
        os.environ["MGTT_REGISTRY_GITHUB_BASE"] = "http://127.0.0.1:1"
        self.addCleanup(os.environ.pop, "MGTT_REGISTRY_GITHUB_BASE", None)
        _clear_cache()
        self.addCleanup(_clear_cache)

    def test_unreachable_url_yields_error_card(self):
        on_pre_build(config=None)
        self.assertIn("registry sync failed", rg.REGISTRY_MD.read_text())


if __name__ == "__main__":
    unittest.main()
