"""End-to-end test for the registry generator.

Spins up an http.server on a random port that mimics the GitHub REST API
and the GHCR Docker Registry v2 API, points the generator at it via env
vars, runs on_pre_build(), and asserts the rendered markdown.
"""

from __future__ import annotations

import http.server
import io
import json
import os
import shutil
import socketserver
import threading
import unittest

from registry_generator import (
    CACHE_DIR,
    REGISTRY_MD,
    fetch_image_digest,
    fetch_provider_yaml,
    load_registry,
    on_pre_build,
    resolve_ref,
)


class RegistryGeneratorE2E(unittest.TestCase):
    def setUp(self):
        self._server, self._thread, self._base = start_stub_server()
        os.environ["MGTT_REGISTRY_GITHUB_BASE"] = self._base + "/github"
        os.environ["MGTT_REGISTRY_GHCR_BASE"] = self._base + "/ghcr"

    def tearDown(self):
        self._server.shutdown()
        self._thread.join()
        for var in ("MGTT_REGISTRY_GITHUB_BASE", "MGTT_REGISTRY_GHCR_BASE"):
            os.environ.pop(var, None)
        shutil.rmtree(CACHE_DIR, ignore_errors=True)

    def test_renders_one_provider_card(self):
        on_pre_build(config=None)
        rendered = REGISTRY_MD.read_text()
        self.assertIn("## tempo", rendered)
        self.assertIn("0.2.0", rendered)
        self.assertIn("sha256:deadbeef", rendered)
        self.assertIn("mgt-tool/tempo@0.2.0", rendered)


def start_stub_server():
    """Boots a local HTTP server returning hardcoded GitHub + GHCR responses."""
    handler = _make_handler()
    server = socketserver.TCPServer(("127.0.0.1", 0), handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    port = server.server_address[1]
    return server, thread, f"http://127.0.0.1:{port}"


def _make_handler():
    import base64
    tempo_yaml = (
        "meta:\n"
        "  name: tempo\n"
        "  version: 0.2.0\n"
        "  description: Per-span SLO checks against Grafana Tempo\n"
        "  tags: [tracing, otel]\n"
        "  requires:\n"
        "    mgtt: \">=0.1.0\"\n"
        "  command: /bin/provider\n"
        "network: host\n"
    )

    class Handler(http.server.BaseHTTPRequestHandler):
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
            elif "/contents/provider.yaml" in self.path:
                body = json.dumps({
                    "content": base64.b64encode(tempo_yaml.encode()).decode(),
                    "encoding": "base64",
                }).encode()
                self._respond(200, body, "application/json")
            elif "/manifests/" in self.path:
                self.send_response(200)
                self.send_header("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
                self.send_header("Docker-Content-Digest", "sha256:deadbeef")
                self.end_headers()
                self.wfile.write(b"{}")
            else:
                self.send_response(404)
                self.end_headers()

        def _respond(self, status, body, content_type):
            self.send_response(status)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    return Handler


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


class GitHubFetchTest(unittest.TestCase):
    def setUp(self):
        self._server, self._thread, self._base = start_stub_server()
        os.environ["MGTT_REGISTRY_GITHUB_BASE"] = self._base + "/github"

    def tearDown(self):
        self._server.shutdown()
        self._thread.join()
        os.environ.pop("MGTT_REGISTRY_GITHUB_BASE", None)

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
        self.assertIn("version: 0.2.0", text)


class GHCRDigestTest(unittest.TestCase):
    def setUp(self):
        self._server, self._thread, self._base = start_stub_server()
        os.environ["MGTT_REGISTRY_GHCR_BASE"] = self._base + "/ghcr"

    def tearDown(self):
        self._server.shutdown()
        self._thread.join()
        os.environ.pop("MGTT_REGISTRY_GHCR_BASE", None)

    def test_digest_from_manifest_header(self):
        from registry_generator import fetch_image_digest
        digest = fetch_image_digest("ghcr.io/mgt-tool/mgtt-provider-tempo", "0.2.0")
        self.assertEqual(digest, "sha256:deadbeef")


if __name__ == "__main__":
    unittest.main()
