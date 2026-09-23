#!/usr/bin/env python3
"""Local OpenAI-compatible stub for model discovery E2E. No billable upstream calls."""

from __future__ import annotations

import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MODEL_HITS = 0
CHAT_HITS = 0


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt: str, *args) -> None:  # quieter compose logs
        return

    def _json(self, status: int, payload: dict) -> None:
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self) -> None:  # noqa: N802
        global MODEL_HITS
        path = self.path.split("?", 1)[0]
        if path == "/healthz":
            self._json(200, {"ok": True, "modelHits": MODEL_HITS, "chatHits": CHAT_HITS})
            return
        if path in ("/v1/models", "/models"):
            MODEL_HITS += 1
            if self.headers.get("Authorization") == "Bearer reject-models":
                self._json(401, {"error": {"message": "invalid stub credential"}})
                return
            self._json(
                200,
                {
                    "object": "list",
                    "data": [
                        {"id": "stub-chat", "object": "model", "owned_by": "stub"},
                        {"id": "stub-reasoner", "object": "model", "owned_by": "stub"},
                    ],
                },
            )
            return
        self._json(404, {"error": {"message": "not found"}})

    def do_POST(self) -> None:  # noqa: N802
        global CHAT_HITS
        path = self.path.split("?", 1)[0]
        length = int(self.headers.get("Content-Length", "0"))
        if length:
            self.rfile.read(length)
        if path.endswith("/chat/completions"):
            CHAT_HITS += 1
            self._json(
                200,
                {"choices": [{"message": {"role": "assistant", "content": "stub"}}]},
            )
            return
        self._json(404, {"error": {"message": "not found"}})


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 18080), Handler).serve_forever()
