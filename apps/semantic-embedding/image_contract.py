from __future__ import annotations

import json
import math
import os
import urllib.request


base_url = os.environ.get("FRUX_EMBEDDING_CONTRACT_URL", "http://127.0.0.1:8081")
token = os.environ["FRUX_INTERNAL_TOKEN"]


def request(path: str, body: dict | None = None) -> dict:
    data = None if body is None else json.dumps(body, ensure_ascii=False).encode()
    call = urllib.request.Request(
        base_url + path,
        data=data,
        headers={"X-Internal-Token": token, "Content-Type": "application/json"},
    )
    with urllib.request.urlopen(call, timeout=20) as response:
        return json.load(response)


metadata = request("/internal/v1/model")
assert metadata["revision"] == "e8f8c211226b894fcb81acc59f3b34ba3efd5f42"
assert metadata["dimension"] == 384
response = request(
    "/internal/v1/embeddings",
    {
        "items": [
            {
                "id": "video:contract",
                "title": "城市夜景",
                "description": "雨后的街道与霓虹灯",
            }
        ]
    },
)
vector = response["items"][0]["embedding"]
assert len(vector) == 384
assert abs(math.sqrt(sum(value * value for value in vector)) - 1) < 1e-4
assert os.getuid() == 10001
assert not os.access("/opt/frux/models", os.W_OK)

equivalent = request(
    "/internal/v1/embeddings",
    {
        "items": [
            {
                "id": "video:equivalent",
                "title": "  城市夜景  ",
                "description": "  雨后的街道与霓虹灯  ",
            }
        ]
    },
)["items"][0]["embedding"]
assert all(abs(left - right) <= 1e-6 + 1e-5 * abs(right) for left, right in zip(vector, equivalent))

batch = request(
    "/internal/v1/embeddings",
    {
        "items": [
            {
                "id": f"video:{index}",
                "title": "城市夜景",
                "description": "雨后的街道与霓虹灯",
            }
            for index in range(9)
        ]
    },
)["items"]
assert [(item["id"], item["index"]) for item in batch] == [
    (f"video:{index}", index) for index in range(9)
]
assert all(
    all(abs(left - right) <= 1e-6 + 1e-5 * abs(right) for left, right in zip(vector, item["embedding"]))
    for item in batch
)
