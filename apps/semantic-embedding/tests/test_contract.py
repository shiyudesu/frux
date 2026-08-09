from __future__ import annotations

import math
import os
import asyncio
from io import StringIO
import json
from pathlib import Path
import subprocess
import sys
from threading import Event
import time

from fastapi.testclient import TestClient
import pytest

TOKEN = "Strong-Internal-Token-For-Frux-123!"
os.environ.setdefault("FRUX_INTERNAL_TOKEN", TOKEN)

from frux_embedding.app import Capacity, InferenceTimeout, OverCapacity, create_app
from frux_embedding.constants import MODEL_DIMENSION, MODEL_NAME, MODEL_REVISION
from frux_embedding.model import ModelRuntime
from frux_embedding.normalization import canonical_text
from frux_embedding.settings import Settings, load_settings
from frux_embedding.startup import (
    StartupFailure,
    StartupFailureError,
    preload_runtime,
    report_failure,
    run_server,
)

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


def test_health_not_ready_and_safe_routes():
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
    calls = 0

    def expired_call():
        nonlocal calls
        calls += 1
        late_release.wait()
        return [[1.0]]

    try:
        await timeout_capacity.run(expired_call, time.monotonic() - 2)
    except InferenceTimeout:
        pass
    else:
        raise AssertionError("late inference did not time out")
    assert calls == 0
    assert timeout_capacity.admitted == 0
    assert timeout_capacity.semaphore._value == 1

    queued_release = Event()
    queued_calls = 0
    queued_capacity = Capacity(
        Settings(
            token=TOKEN,
            max_concurrency=1,
            max_queue=1,
            queue_timeout_ms=1_000,
            request_timeout_ms=200,
        )
    )
    active = asyncio.create_task(
        queued_capacity.run(
            lambda: (queued_release.wait(), [[1.0]])[1],
            time.monotonic(),
        )
    )
    await asyncio.sleep(0.01)

    def queued_call():
        nonlocal queued_calls
        queued_calls += 1
        return [[1.0]]

    with pytest.raises(InferenceTimeout):
        await queued_capacity.run(queued_call, time.monotonic() - 0.15)
    assert queued_calls == 0
    assert queued_capacity.admitted == 1
    assert queued_capacity.semaphore._value == 0
    queued_release.set()
    assert await active == [[1.0]]
    assert queued_capacity.admitted == 0
    assert queued_capacity.semaphore._value == 1

    post_acquire_calls = 0
    post_acquire = Capacity(
        Settings(
            token=TOKEN,
            max_concurrency=1,
            max_queue=0,
            queue_timeout_ms=100,
            request_timeout_ms=1_000,
        )
    )
    remaining = iter((1.0, 1.0, 1.0, -1.0))
    post_acquire._remaining = lambda _: next(remaining)

    def post_acquire_call():
        nonlocal post_acquire_calls
        post_acquire_calls += 1
        return [[1.0]]

    with pytest.raises(InferenceTimeout):
        await post_acquire.run(post_acquire_call, time.monotonic())
    assert post_acquire_calls == 0
    assert post_acquire.admitted == 0
    assert post_acquire.semaphore._value == 1

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


@pytest.mark.parametrize(
    ("error", "expected"),
    [
        (
            StartupFailureError(StartupFailure.MODEL_ARTIFACT),
            StartupFailure.MODEL_ARTIFACT,
        ),
        (
            StartupFailureError(StartupFailure.MODEL_METADATA),
            StartupFailure.MODEL_METADATA,
        ),
        (
            StartupFailureError(StartupFailure.FIXTURE_CONTRACT),
            StartupFailure.FIXTURE_CONTRACT,
        ),
        (RuntimeError("dependency detail"), StartupFailure.DEPENDENCY),
    ],
)
def test_startup_failure_classification_and_output_are_bounded(error, expected):
    class FailingRuntime:
        def load(self):
            raise error

    result = preload_runtime(FailingRuntime(), 1)
    assert result.failure is expected
    output = StringIO()
    report_failure(result, output)
    logged = output.getvalue()
    assert logged == f"semantic_embedding_startup_failed category={expected.value}\n"
    assert "dependency detail" not in logged
    assert "Traceback" not in logged


def test_startup_preload_timeout_is_bounded():
    release = Event()

    class BlockingRuntime:
        def load(self):
            release.wait()

    result = preload_runtime(BlockingRuntime(), 0.01)
    release.set()
    assert result.failure is StartupFailure.PRELOAD_TIMEOUT
    output = StringIO()
    report_failure(result, output)
    assert output.getvalue() == (
        "semantic_embedding_startup_failed category=preload_timeout\n"
    )


@pytest.mark.parametrize(
    "error",
    [
        StartupFailureError(StartupFailure.MODEL_ARTIFACT),
        StartupFailureError(StartupFailure.MODEL_METADATA),
        StartupFailureError(StartupFailure.FIXTURE_CONTRACT),
        RuntimeError("dependency secret detail"),
    ],
)
def test_controlled_entrypoint_returns_nonzero_without_server_logs(
    error, capsys
):
    class FailingRuntime:
        def load(self):
            raise error

    code = run_server(
        settings_loader=lambda: Settings(token=TOKEN),
        runtime_factory=lambda *_: FailingRuntime(),
        app_factory=lambda *_: pytest.fail("app must not be created"),
        server_runner=lambda *_: pytest.fail("server must not start"),
        startup_timeout_seconds=1,
    )
    output = capsys.readouterr()
    assert code == 1
    assert output.out == ""
    assert output.err.startswith("semantic_embedding_startup_failed category=")
    for forbidden in (
        "Traceback",
        "uvicorn",
        "fastapi",
        "dependency secret detail",
        TOKEN,
    ):
        assert forbidden not in output.err


def test_controlled_entrypoint_timeout_returns_nonzero(capsys):
    release = Event()

    class BlockingRuntime:
        def load(self):
            release.wait()

    code = run_server(
        settings_loader=lambda: Settings(token=TOKEN),
        runtime_factory=lambda *_: BlockingRuntime(),
        app_factory=lambda *_: pytest.fail("app must not be created"),
        server_runner=lambda *_: pytest.fail("server must not start"),
        startup_timeout_seconds=0.01,
    )
    release.set()
    output = capsys.readouterr()
    assert code == 1
    assert output.err == (
        "semantic_embedding_startup_failed category=preload_timeout\n"
    )


def test_model_runtime_classifies_artifact_metadata_and_fixture_failures(monkeypatch):
    runtime = ModelRuntime("missing-model", "missing-fixture")
    assert (
        preload_runtime(runtime, 1).failure is StartupFailure.MODEL_ARTIFACT
    )

    monkeypatch.setattr(Path, "read_text", lambda _: "{")
    assert (
        preload_runtime(ModelRuntime("model", "fixture"), 1).failure
        is StartupFailure.MODEL_ARTIFACT
    )

    monkeypatch.setattr(
        Path,
        "read_text",
        lambda _: json.dumps({"model": MODEL_NAME, "revision": "wrong"}),
    )
    assert (
        preload_runtime(ModelRuntime("model", "fixture"), 1).failure
        is StartupFailure.MODEL_METADATA
    )

    reads = iter(
        (
            json.dumps({"model": MODEL_NAME, "revision": MODEL_REVISION}),
            json.dumps({"model": MODEL_NAME, "revision": MODEL_REVISION}),
        )
    )
    monkeypatch.setattr(Path, "read_text", lambda _: next(reads))
    monkeypatch.setattr(ModelRuntime, "_load_model", lambda _: object())
    assert (
        preload_runtime(ModelRuntime("model", "fixture"), 1).failure
        is StartupFailure.FIXTURE_CONTRACT
    )


def test_startup_subprocess_redacts_model_failure_details():
    service_root = Path(__file__).parents[1]
    secret_path = service_root / "secret-model-path"
    secret_token = "Secret-Internal-Token-For-Startup-123!"
    env = {
        name: value
        for name, value in os.environ.items()
        if not name.startswith("FRUX_EMBEDDING_")
    }
    env.update(
        {
            "FRUX_INTERNAL_TOKEN": secret_token,
            "FRUX_EMBEDDING_MODEL_PATH": str(secret_path),
            "FRUX_EMBEDDING_FIXTURE_PATH": str(secret_path / "fixture.json"),
            "OMP_NUM_THREADS": "2",
            "MKL_NUM_THREADS": "2",
            "OPENBLAS_NUM_THREADS": "2",
            "NUMEXPR_NUM_THREADS": "2",
            "TOKENIZERS_PARALLELISM": "false",
            "PYTHONPATH": str(service_root / "src"),
        }
    )
    completed = subprocess.run(
        [sys.executable, "-m", "frux_embedding.main"],
        cwd=service_root,
        env=env,
        capture_output=True,
        text=True,
        timeout=10,
        check=False,
    )
    output = completed.stdout + completed.stderr
    assert completed.returncode != 0
    assert output == "semantic_embedding_startup_failed category=model_artifact\n"
    for forbidden in (
        "Traceback",
        "uvicorn",
        "fastapi",
        str(secret_path),
        secret_token,
        "FileNotFoundError",
    ):
        assert forbidden not in output
