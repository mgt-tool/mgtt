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
import tarfile
import threading
import unittest
from pathlib import Path

from registry_generator import REGISTRY_MD, on_pre_build


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
        shutil.rmtree(Path(__file__).resolve().parent.parent.parent / ".cache" / "registry-generator",
                      ignore_errors=True)

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
            if self.path.endswith("/tags"):
                body = json.dumps([{"name": "v0.2.0"}, {"name": "v0.1.0"}]).encode()
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


if __name__ == "__main__":
    unittest.main()
