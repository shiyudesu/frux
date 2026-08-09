from __future__ import annotations

import math
import os
import asyncio
import json
from threading import Event
import time

from fastapi.testclient import TestClient

TOKEN = "Strong-Internal-Token-For-Frux-123!"
os.environ.setdefault("FRUX_INTERNAL_TOKEN", TOKEN)

from frux_embedding.app import Capacity, InferenceTimeout, OverCapacity, create_app
from frux_embedding.constants import MODEL_DIMENSION, MODEL_NAME, MODEL_REVISION
from frux_embedding.normalization import canonical_text
from frux_embedding.settings import Settings, load_settings

class FakeRuntime:
    ready = True
    calls = 0

    def load(self):
        self.ready = True

    def encode(self, texts):
        self.calls += 1
        value = 1 / math.sqrt(MODEL_DIMENSION)
        return [[value] * MODEL_DIMENSION for _ in texts]


def client():
    return TestClient(create_app(Settings(token=TOKEN), FakeRuntime()))


def test_settings_reject_unknown_and_weak_values():
    load_settings({"FRUX_INTERNAL_TOKEN": TOKEN})
    for env in (
        {"FRUX_INTERNAL_TOKEN": "weak"},
        {"FRUX_INTERNAL_TOKEN": TOKEN, "FRUX_EMBEDDING_UNKNOWN": "1"},
        {"FRUX_INTERNAL_TOKEN": TOKEN, "FRUX_EMBEDDING_MAX_CONCURRENCY": "3"},
    ):
        try:
            load_settings(env)
        except ValueError:
            pass
        else:
            raise AssertionError("invalid settings accepted")
    assert load_settings(
        {
            "FRUX_INTERNAL_TOKEN": TOKEN,
            "FRUX_EMBEDDING_PORT": "1",
            "FRUX_EMBEDDING_MAX_CONCURRENCY": "1",
            "FRUX_EMBEDDING_MAX_QUEUE": "0",
            "FRUX_EMBEDDING_QUEUE_TIMEOUT_MS": "100",
            "FRUX_EMBEDDING_REQUEST_TIMEOUT_MS": "1000",
            "FRUX_EMBEDDING_LOG_LEVEL": "warning",
        }
    ).port == 1
    for name, value in (
        ("FRUX_EMBEDDING_PORT", "65536"),
        ("FRUX_EMBEDDING_MAX_QUEUE", "9"),
        ("FRUX_EMBEDDING_QUEUE_TIMEOUT_MS", "99"),
        ("FRUX_EMBEDDING_REQUEST_TIMEOUT_MS", "2000"),
        ("FRUX_EMBEDDING_BIND_HOST", "localhost"),
        ("FRUX_EMBEDDING_LOG_LEVEL", "TRACE"),
    ):
        env = {"FRUX_INTERNAL_TOKEN": TOKEN, name: value}
        if name == "FRUX_EMBEDDING_REQUEST_TIMEOUT_MS":
            env["FRUX_EMBEDDING_QUEUE_TIMEOUT_MS"] = "2000"
        try:
            load_settings(env)
        except ValueError:
            pass
        else:
            raise AssertionError(f"invalid {name} accepted")


def test_authentication_precedes_body_validation():
    runtime = FakeRuntime()
    with TestClient(create_app(Settings(token=TOKEN), runtime)) as api:
        response = api.post("/internal/v1/embeddings", content=b"{")
        assert response.status_code == 401
        assert response.json()["code"] == "AUTH_INTERNAL_TOKEN_REQUIRED"
        assert runtime.calls == 0
        wrong = api.post(
            "/internal/v1/embeddings",
            headers={"X-Internal-Token": "wrong"},
            json={"items": []},
        )
        assert wrong.status_code == 401
        query = api.post(
            f"/internal/v1/embeddings?token={TOKEN}",
            json={"items": []},
        )
        assert query.status_code == 401
        assert runtime.calls == 0


def test_metadata_and_ordered_embedding_contract():
    with client() as api:
        metadata = api.get(
            "/internal/v1/model", headers={"X-Internal-Token": f" {TOKEN} "}
        )
        assert metadata.status_code == 200
        assert metadata.json()["model"] == MODEL_NAME
        assert metadata.json()["revision"] == MODEL_REVISION
        response = api.post(
            "/internal/v1/embeddings",
            headers={"X-Internal-Token": TOKEN},
            json={
                "items": [
                    {"id": "video:1", "title": "  Ｆｒｕｘ  ", "description": "语义\t内容"},
                    {"id": "video:2", "title": "城市", "description": ""},
                ]
            },
        )
        assert response.status_code == 200
        items = response.json()["items"]
        assert [(item["id"], item["index"]) for item in items] == [
            ("video:1", 0),
            ("video:2", 1),
        ]
        assert all(len(item["embedding"]) == MODEL_DIMENSION for item in items)


def test_normalization_and_safe_errors():
    assert canonical_text("  Ｆｒｕｘ  ", "语义\t 内容") == (
        "Frux",
        "语义 内容",
        "Frux\n语义 内容",
    )
    with client() as api:
        response = api.post(
            "/internal/v1/embeddings",
            headers={"X-Internal-Token": TOKEN},
            json={"items": [{"id": "bad id", "title": "x", "description": ""}]},
        )
        assert response.status_code == 400
        assert response.json() == {"code": "INVALID_REQUEST", "error": "invalid request"}


def test_strict_schema_malformed_json_and_body_limit():
    headers = {"X-Internal-Token": TOKEN, "Content-Type": "application/json"}
    with client() as api:
        cases = [
            b"{",
            b'{"items":[]} trailing',
            b'{"items":[{"id":"video:1","title":"x","description":"","extra":1}]}',
            b'{"items":[{"id":"video:1","title":"x"}]}',
        ]
        for body in cases:
            response = api.post("/internal/v1/embeddings", headers=headers, content=body)
            assert response.status_code == 400
            assert set(response.json()) == {"code", "error"}
        response = api.post(
            "/internal/v1/embeddings",
            headers=headers,
            content=b" " * 131_073,
        )
        assert response.status_code == 413
        assert response.json()["code"] == "REQUEST_TOO_LARGE"
        valid = json.dumps(
            {"items": [{"id": "video:1", "title": "x", "description": ""}]}
        ).encode()
        exact = valid + b" " * (131_072 - len(valid))
        assert api.post(
            "/internal/v1/embeddings", headers=headers, content=exact
        ).status_code == 200


def test_duplicate_controls_and_boundaries_are_rejected_before_inference():
    runtime = FakeRuntime()
    with TestClient(create_app(Settings(token=TOKEN), runtime)) as api:
        for items in (
            [
                {"id": "video:1", "title": "x", "description": ""},
                {"id": "video:1", "title": "y", "description": ""},
            ],
            [{"id": "video:1", "title": "x\x00", "description": ""}],
            [{"id": "video:1", "title": "x" * 201, "description": ""}],
            [{"id": "video:1", "title": "x", "description": "y" * 2001}],
        ):
            response = api.post(
                "/internal/v1/embeddings",
                headers={"X-Internal-Token": TOKEN},
                json={"items": items},
            )
            assert response.status_code == 400
        assert runtime.calls == 0


def test_exact_text_and_batch_boundaries():
    with client() as api:
        exact_items = [
            {
                "id": f"video:{index}",
                "title": "x" * 200,
                "description": "y" * 2000,
            }
            for index in range(32)
        ]
        response = api.post(
            "/internal/v1/embeddings",
            headers={"X-Internal-Token": TOKEN},
            json={"items": exact_items},
        )
        assert response.status_code == 400
        bounded_items = [
            {"id": f"video:{index}", "title": "x" * 200, "description": ""}
            for index in range(32)
        ]
        assert api.post(
            "/internal/v1/embeddings",
            headers={"X-Internal-Token": TOKEN},
            json={"items": bounded_items},
        ).status_code == 200
        assert api.post(
            "/internal/v1/embeddings",
            headers={"X-Internal-Token": TOKEN},
            json={"items": bounded_items + [{"id": "video:33", "title": "x", "description": ""}]},
        ).status_code == 400


def test_health_not_ready_startup_failure_and_safe_routes():
    class NotReadyRuntime(FakeRuntime):
        ready = False

        def load(self):
            self.ready = False

    with TestClient(create_app(Settings(token=TOKEN), NotReadyRuntime())) as api:
        assert api.get("/health/live").json() == {"status": "live"}
        assert api.get("/health/ready").status_code == 503
        assert api.get(
            "/internal/v1/model", headers={"X-Internal-Token": TOKEN}
        ).status_code == 503
        assert api.get("/missing").json() == {"code": "NOT_FOUND", "error": "not found"}
        assert api.post("/health/live").status_code == 405

    class FailingRuntime(NotReadyRuntime):
        def load(self):
            raise RuntimeError("model failure")

    try:
        with TestClient(create_app(Settings(token=TOKEN), FailingRuntime())):
            pass
    except RuntimeError:
        pass
    else:
        raise AssertionError("startup model failure was ignored")


async def test_capacity_bounds_and_late_native_completion():
    release = Event()
    settings = Settings(
        token=TOKEN,
        max_concurrency=1,
        max_queue=0,
        queue_timeout_ms=100,
        request_timeout_ms=1_000,
    )
    capacity = Capacity(settings)
    first = asyncio.create_task(
        capacity.run(lambda: (release.wait(), [[1.0]])[1], time.monotonic())
    )
    await asyncio.sleep(0.02)
    try:
        await capacity.run(lambda: [[1.0]], time.monotonic())
    except OverCapacity:
        pass
    else:
        raise AssertionError("overflow was admitted")
    release.set()
    assert await first == [[1.0]]

    late_release = Event()
    timeout_capacity = Capacity(
        Settings(
            token=TOKEN,
            max_concurrency=1,
            max_queue=0,
            queue_timeout_ms=100,
            request_timeout_ms=1_000,
        )
    )
    try:
        await timeout_capacity.run(
            lambda: (late_release.wait(), [[1.0]])[1],
            time.monotonic() - 2,
        )
    except InferenceTimeout:
        pass
    else:
        raise AssertionError("late inference did not time out")
    assert timeout_capacity.admitted == 1
    late_release.set()
    for _ in range(20):
        if timeout_capacity.admitted == 0:
            break
        await asyncio.sleep(0.01)
    assert timeout_capacity.admitted == 0

    release_many = Event()
    bounded = Capacity(
        Settings(
            token=TOKEN,
            max_concurrency=2,
            max_queue=8,
            queue_timeout_ms=2_000,
            request_timeout_ms=15_000,
        )
    )
    tasks = [
        asyncio.create_task(
            bounded.run(lambda: (release_many.wait(), [[1.0]])[1], time.monotonic())
        )
        for _ in range(10)
    ]
    await asyncio.sleep(0.05)
    assert bounded.admitted == 10
    try:
        await bounded.run(lambda: [[1.0]], time.monotonic())
    except OverCapacity:
        pass
    else:
        raise AssertionError("eleventh request was admitted")
    release_many.set()
    await asyncio.gather(*tasks)
    assert bounded.admitted == 0
