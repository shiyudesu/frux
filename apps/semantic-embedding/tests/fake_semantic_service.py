from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import math
import os
from pathlib import Path

TOKEN = "Compose-Semantic-Outage-Token-123!"
READY = Path(os.environ.get("FRUX_FAKE_SEMANTIC_READY_FILE", "/state/ready"))
MODEL = "sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2"
REVISION = "e8f8c211226b894fcb81acc59f3b34ba3efd5f42"
DIMENSION = 384


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def response(self, status, body):
        content = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(content)))
        self.end_headers()
        self.wfile.write(content)

    def authenticated(self):
        if self.headers.get("X-Internal-Token") != TOKEN:
            self.response(401, {"code": "AUTH_FAILED", "error": "unauthorized"})
            return False
        return True

    def do_GET(self):
        if self.path == "/health/live":
            self.response(200, {"status": "live"})
            return
        if self.path == "/health/ready":
            self.response(
                200 if READY.exists() else 503,
                {"status": "ready" if READY.exists() else "not_ready"},
            )
            return
        if self.path != "/internal/v1/model":
            self.response(404, {"code": "NOT_FOUND", "error": "not found"})
            return
        if not self.authenticated():
            return
        if not READY.exists():
            self.response(503, {"code": "NOT_READY", "error": "not ready"})
            return
        self.response(
            200,
            {
                "model": MODEL,
                "revision": REVISION,
                "dimension": DIMENSION,
                "max_sequence_tokens": 128,
                "dtype": "float32",
                "normalized": True,
                "device": "cpu",
                "limits": {
                    "max_batch_size": 32,
                    "max_title_codepoints": 200,
                    "max_description_codepoints": 2000,
                    "max_total_codepoints": 16384,
                    "max_request_bytes": 131072,
                },
            },
        )

    def do_POST(self):
        if self.path != "/internal/v1/embeddings":
            self.response(404, {"code": "NOT_FOUND", "error": "not found"})
            return
        if not self.authenticated():
            return
        if not READY.exists():
            self.response(503, {"code": "NOT_READY", "error": "not ready"})
            return
        length = int(self.headers.get("Content-Length", "0"))
        request = json.loads(self.rfile.read(length))
        value = 1 / math.sqrt(DIMENSION)
        self.response(
            200,
            {
                "model": MODEL,
                "revision": REVISION,
                "dimension": DIMENSION,
                "items": [
                    {
                        "id": item["id"],
                        "index": index,
                        "embedding": [value] * DIMENSION,
                    }
                    for index, item in enumerate(request["items"])
                ],
            },
        )


ThreadingHTTPServer(("0.0.0.0", 8081), Handler).serve_forever()
