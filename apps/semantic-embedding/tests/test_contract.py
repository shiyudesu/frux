from __future__ import annotations

import math
import multiprocessing
import os
import asyncio
import logging
from io import StringIO
import json
from pathlib import Path
import subprocess
import sys
from threading import Event
import time
from types import SimpleNamespace

from fastapi.testclient import TestClient
import httpx
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

CANONICALIZATION_FIXTURES = (
    Path(__file__).parents[1] / "fixtures" / "canonicalization-fixtures.json"
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


class ControlledRuntime:
    ready = True

    def __init__(self, gate=None):
        self.gate = gate

    def load(self):
        self.ready = True

    def encode(self, texts):
        if self.gate is not None:
            self.gate.wait()
        return [[1.0] for _ in texts]


class HangingRuntime:
    ready = True

    def __init__(self, gate, secret):
        self.gate = gate
        self.secret = secret

    def load(self):
        self.ready = True

    def encode(self, texts):
        if texts == ["hang"]:
            print(self.secret, flush=True)
            self.gate.wait()
        return [[1.0] for _ in texts]


class NotReadyRuntime(FakeRuntime):
    ready = False

    def load(self):
        self.ready = False


class ReplacementBlockingRuntime:
    ready = True

    def __init__(self, loads, replacement_gate, inference_gate):
        self.loads = loads
        self.replacement_gate = replacement_gate
        self.inference_gate = inference_gate

    def load(self):
        with self.loads.get_lock():
            self.loads.value += 1
            count = self.loads.value
        if count > 1:
            self.replacement_gate.wait()
        self.ready = True

    def encode(self, texts):
        if texts == ["hang"]:
            self.inference_gate.wait()
        return [[1.0] for _ in texts]


class RecoveringReplacementRuntime:
    ready = True

    def __init__(self, loads, initial_workers, recovery):
        self.loads = loads
        self.initial_workers = initial_workers
        self.recovery = recovery

    def load(self):
        with self.loads.get_lock():
            self.loads.value += 1
            count = self.loads.value
        if count > self.initial_workers and not self.recovery.is_set():
            raise RuntimeError("replacement unavailable")
        self.ready = True

    def encode(self, texts):
        return [[1.0] for _ in texts]


class PhasedStartupRuntime:
    ready = False

    def __init__(self, loads, delay):
        self.loads = loads
        self.delay = delay

    def load(self):
        with self.loads.get_lock():
            self.loads.value += 1
            count = self.loads.value
        time.sleep(0.05 if count == 1 else self.delay)
        self.ready = True

    def encode(self, texts):
        return [[1.0] for _ in texts]


class RaisingCapacity:
    def __init__(self, error=None):
        self.error = error

    async def run(self, texts, started):
        if self.error is not None:
            raise self.error
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


def test_shared_go_python_canonicalization_fixtures():
    fixtures = json.loads(CANONICALIZATION_FIXTURES.read_text())
    for fixture in fixtures:
        title, description, text = canonical_text(
            fixture["title"], fixture["description"]
        )
        assert title == fixture["canonical_title"]
        assert description == fixture["canonical_description"]
        assert text == fixture["text"]


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
    with TestClient(create_app(Settings(token=TOKEN), NotReadyRuntime())) as api:
        assert api.get("/health/live").json() == {"status": "live"}
        assert api.get("/health/ready").status_code == 503
        assert api.get(
            "/internal/v1/model", headers={"X-Internal-Token": TOKEN}
        ).status_code == 503
        assert api.get("/missing").json() == {"code": "NOT_FOUND", "error": "not found"}
        assert api.post("/health/live").status_code == 405

async def test_capacity_bounds_and_late_native_completion():
    process_context = multiprocessing.get_context("spawn")
    release = process_context.Event()
    settings = Settings(
        token=TOKEN,
        max_concurrency=1,
        max_queue=0,
        queue_timeout_ms=100,
        request_timeout_ms=1_000,
    )
    capacity = Capacity(settings, ControlledRuntime(release))
    first = asyncio.create_task(
        capacity.run(["first"], time.monotonic())
    )
    await asyncio.sleep(0.02)
    try:
        await capacity.run(["overflow"], time.monotonic())
    except OverCapacity:
        pass
    else:
        raise AssertionError("overflow was admitted")
    release.set()
    assert await first == [[1.0]]
    await capacity.close()

    timeout_capacity = Capacity(
        Settings(
            token=TOKEN,
            max_concurrency=1,
            max_queue=0,
            queue_timeout_ms=100,
            request_timeout_ms=1_000,
        ),
        ControlledRuntime(),
    )
    try:
        await timeout_capacity.run(["expired"], time.monotonic() - 2)
    except InferenceTimeout:
        pass
    else:
        raise AssertionError("late inference did not time out")
    assert timeout_capacity.admitted == 0
    assert timeout_capacity.pool.available.qsize() == 1
    await timeout_capacity.close()

    queued_release = process_context.Event()
    queued_capacity = Capacity(
        Settings(
            token=TOKEN,
            max_concurrency=1,
            max_queue=1,
            queue_timeout_ms=1_000,
            request_timeout_ms=200,
        ),
        ControlledRuntime(queued_release),
    )
    active = asyncio.create_task(
        queued_capacity.run(["active"], time.monotonic())
    )
    await asyncio.sleep(0.01)
    with pytest.raises(InferenceTimeout):
        await queued_capacity.run(["queued"], time.monotonic() - 0.15)
    assert queued_capacity.admitted == 1
    assert queued_capacity.pool.available.qsize() == 0
    queued_release.set()
    assert await active == [[1.0]]
    assert queued_capacity.admitted == 0
    assert queued_capacity.pool.available.qsize() == 1
    await queued_capacity.close()

    release_many = process_context.Event()
    bounded = Capacity(
        Settings(
            token=TOKEN,
            max_concurrency=2,
            max_queue=8,
            queue_timeout_ms=2_000,
            request_timeout_ms=15_000,
        ),
        ControlledRuntime(release_many),
    )
    tasks = [
        asyncio.create_task(
            bounded.run([f"request-{index}"], time.monotonic())
        )
        for index in range(10)
    ]
    await asyncio.sleep(0.05)
    assert bounded.admitted == 10
    try:
        await bounded.run(["eleventh"], time.monotonic())
    except OverCapacity:
        pass
    else:
        raise AssertionError("eleventh request was admitted")
    release_many.set()
    await asyncio.gather(*tasks)
    assert bounded.admitted == 0
    await bounded.close()


async def test_hung_inference_is_killed_replaced_and_does_not_leak(capsys):
    process_context = multiprocessing.get_context("spawn")
    hang = process_context.Event()
    secret = "semantic-input-must-not-be-logged"

    capacity = Capacity(
        Settings(
            token=TOKEN,
            max_concurrency=1,
            max_queue=0,
            queue_timeout_ms=100,
            request_timeout_ms=150,
        ),
        HangingRuntime(hang, secret),
    )
    original = capacity.worker_pids()
    assert len(original) == 1
    with pytest.raises(InferenceTimeout):
        await capacity.run(["hang"], time.monotonic())
    assert capacity.admitted == 0
    deadline = time.monotonic() + 2
    replacement = set()
    while time.monotonic() < deadline:
        replacement = capacity.worker_pids()
        if replacement and replacement != original:
            break
        await asyncio.sleep(0.02)
    assert len(replacement) == 1
    assert replacement.isdisjoint(original)
    assert await capacity.run(["recovered"], time.monotonic()) == [[1.0]]
    all_pids = original | replacement
    await capacity.close()
    active = {child.pid for child in multiprocessing.active_children()}
    assert all_pids.isdisjoint(active)
    output = capsys.readouterr()
    assert secret not in output.out
    assert secret not in output.err


async def test_idle_worker_death_is_recycled_before_inference():
    capacity = Capacity(
        Settings(
            token=TOKEN,
            max_concurrency=1,
            max_queue=0,
            queue_timeout_ms=1_000,
            request_timeout_ms=1_000,
        ),
        ControlledRuntime(),
    )
    original = capacity.worker_pids()
    worker = next(iter(capacity.pool.workers))
    worker.process.terminate()
    worker.process.join(timeout=1)
    deadline = time.monotonic() + 3
    replacement = set()
    while time.monotonic() < deadline:
        replacement = capacity.worker_pids()
        if replacement and replacement.isdisjoint(original):
            break
        await asyncio.sleep(0.02)
    assert len(replacement) == 1
    assert replacement.isdisjoint(original)
    assert await capacity.run(["recovered"], time.monotonic()) == [[1.0]]
    await capacity.close()


async def test_idle_replacement_retries_until_runtime_recovers():
    process_context = multiprocessing.get_context("spawn")
    loads = process_context.Value("i", 0)
    recovery = process_context.Event()
    capacity = Capacity(
        Settings(
            token=TOKEN,
            max_concurrency=1,
            max_queue=0,
            queue_timeout_ms=1_000,
            request_timeout_ms=1_000,
        ),
        RecoveringReplacementRuntime(loads, 1, recovery),
    )
    worker = next(iter(capacity.pool.workers))
    worker.process.terminate()
    worker.process.join(timeout=1)
    deadline = time.monotonic() + 2
    while time.monotonic() < deadline:
        with loads.get_lock():
            if loads.value >= 2:
                break
        await asyncio.sleep(0.02)
    with loads.get_lock():
        assert loads.value >= 2
    assert capacity.live_capacity() == 0
    recovery.set()
    deadline = time.monotonic() + 4
    while time.monotonic() < deadline and capacity.live_capacity() == 0:
        await asyncio.sleep(0.05)
    assert capacity.live_capacity() == 1
    assert await capacity.run(["recovered"], time.monotonic()) == [[1.0]]
    await capacity.close()


async def test_idle_worker_loss_changes_readiness_from_503_to_200():
    settings = Settings(
        token=TOKEN,
        max_concurrency=1,
        max_queue=0,
        queue_timeout_ms=1_000,
        request_timeout_ms=1_000,
    )
    app = create_app(settings, ControlledRuntime())
    capacity = app.state.capacity
    transport = httpx.ASGITransport(app=app)
    try:
        async with httpx.AsyncClient(
            transport=transport, base_url="http://semantic.test"
        ) as api:
            worker = next(iter(capacity.pool.workers))
            worker.process.terminate()
            worker.process.join(timeout=1)
            assert (await api.get("/health/ready")).status_code == 503
            deadline = time.monotonic() + 3
            while time.monotonic() < deadline:
                response = await api.get("/health/ready")
                if response.status_code == 200:
                    break
                await asyncio.sleep(0.02)
            assert response.status_code == 200
    finally:
        await capacity.close()


async def test_shutdown_terminates_in_progress_process_replacement():
    process_context = multiprocessing.get_context("spawn")
    loads = process_context.Value("i", 0)
    replacement_gate = process_context.Event()
    inference_gate = process_context.Event()
    capacity = Capacity(
        Settings(
            token=TOKEN,
            max_concurrency=1,
            max_queue=0,
            queue_timeout_ms=100,
            request_timeout_ms=150,
        ),
        ReplacementBlockingRuntime(loads, replacement_gate, inference_gate),
    )
    original = capacity.worker_pids()
    with pytest.raises(InferenceTimeout):
        await capacity.run(["hang"], time.monotonic())
    deadline = time.monotonic() + 2
    while time.monotonic() < deadline:
        with loads.get_lock():
            if loads.value >= 2:
                break
        await asyncio.sleep(0.02)
    with loads.get_lock():
        assert loads.value >= 2
    await asyncio.wait_for(capacity.close(), timeout=1)
    active = multiprocessing.active_children()
    assert original.isdisjoint({child.pid for child in active})
    assert all(child.name != "semantic-embedding-inference" for child in active)


async def test_shutdown_stops_idle_monitor_and_replacement():
    process_context = multiprocessing.get_context("spawn")
    loads = process_context.Value("i", 0)
    replacement_gate = process_context.Event()
    inference_gate = process_context.Event()
    capacity = Capacity(
        Settings(
            token=TOKEN,
            max_concurrency=1,
            max_queue=0,
            queue_timeout_ms=100,
            request_timeout_ms=150,
        ),
        ReplacementBlockingRuntime(loads, replacement_gate, inference_gate),
    )
    original = capacity.worker_pids()
    worker = next(iter(capacity.pool.workers))
    worker.process.terminate()
    worker.process.join(timeout=1)
    deadline = time.monotonic() + 2
    while time.monotonic() < deadline:
        with loads.get_lock():
            if loads.value >= 2:
                break
        await asyncio.sleep(0.02)
    with loads.get_lock():
        assert loads.value >= 2
    await asyncio.wait_for(capacity.close(), timeout=1)
    active = multiprocessing.active_children()
    assert original.isdisjoint({child.pid for child in active})
    assert all(child.name != "semantic-embedding-inference" for child in active)


async def test_all_worker_loss_fails_readiness_and_recovers():
    process_context = multiprocessing.get_context("spawn")
    loads = process_context.Value("i", 0)
    recovery = process_context.Event()
    settings = Settings(
        token=TOKEN,
        max_concurrency=2,
        max_queue=0,
        queue_timeout_ms=100,
        request_timeout_ms=1_000,
    )
    app = create_app(
        settings,
        RecoveringReplacementRuntime(loads, settings.max_concurrency, recovery),
    )
    capacity = app.state.capacity
    transport = httpx.ASGITransport(app=app)
    try:
        async with httpx.AsyncClient(
            transport=transport, base_url="http://semantic.test"
        ) as api:
            assert (await api.get("/health/ready")).status_code == 200
            workers = [
                await capacity.pool.acquire(0.1)
                for _ in range(settings.max_concurrency)
            ]
            for worker in workers:
                capacity.pool.recycle(worker)
            assert capacity.live_capacity() == 0
            assert (await api.get("/health/ready")).status_code == 503
            deadline = time.monotonic() + 2
            while time.monotonic() < deadline:
                with loads.get_lock():
                    if loads.value > settings.max_concurrency:
                        break
                await asyncio.sleep(0.02)
            recovery.set()
            deadline = time.monotonic() + 4
            while time.monotonic() < deadline:
                response = await api.get("/health/ready")
                if response.status_code == 200:
                    break
                await asyncio.sleep(0.05)
            assert response.status_code == 200
            assert capacity.live_capacity() == settings.max_concurrency
    finally:
        await capacity.close()


def test_single_outer_startup_deadline_covers_complete_pool(capsys):
    process_context = multiprocessing.get_context("spawn")
    loads = process_context.Value("i", 0)
    started = time.monotonic()
    code = run_server(
        settings_loader=lambda: Settings(token=TOKEN, max_concurrency=2),
        runtime_factory=lambda *_: PhasedStartupRuntime(loads, 2),
        app_factory=create_app,
        server_runner=lambda *_: pytest.fail("server must not start"),
        startup_timeout_seconds=0.8,
    )
    elapsed = time.monotonic() - started
    assert code == 1
    assert elapsed < 1.8
    assert capsys.readouterr().err == (
        "semantic_embedding_startup_failed category=startup_timeout\n"
    )
    with loads.get_lock():
        assert loads.value >= 2


def test_uvicorn_access_logging_remains_disabled(monkeypatch):
    observed = {}

    def run(application, **kwargs):
        observed.update(kwargs)

    monkeypatch.setitem(sys.modules, "uvicorn", SimpleNamespace(run=run))
    assert run_server(
        settings_loader=lambda: Settings(token=TOKEN),
        runtime_factory=lambda *_: FakeRuntime(),
        app_factory=lambda *_: object(),
        startup_timeout_seconds=1,
    ) == 0
    assert observed["access_log"] is False
    assert observed["server_header"] is False
    assert observed["date_header"] is False


def test_bounded_structured_operational_logging():
    app = create_app(
        Settings(token=TOKEN, max_concurrency=1),
        FakeRuntime(),
    )
    secret_id = "video:secret-id"
    secret_text = "private semantic text"
    secret_token = "query-secret-token"
    stream = StringIO()
    handler = logging.StreamHandler(stream)
    handler.setFormatter(logging.Formatter("%(message)s"))
    logger = logging.getLogger("frux_embedding.requests")
    logger.addHandler(handler)
    try:
        with TestClient(app) as api:
            app.state.capacity = RaisingCapacity()
            assert api.post(
                "/internal/v1/embeddings",
                headers={"X-Internal-Token": TOKEN},
                json={
                    "items": [
                        {
                            "id": secret_id,
                            "title": secret_text,
                            "description": "",
                        }
                    ]
                },
            ).status_code == 200
            assert api.post(
                "/internal/v1/embeddings",
                headers={"X-Internal-Token": TOKEN},
                json={"items": [{"id": "bad id", "title": "x", "description": ""}]},
            ).status_code == 400
            assert api.get("/internal/v1/model").status_code == 401
            for error, status in (
                (OverCapacity(), 429),
                (InferenceTimeout(), 504),
                (RuntimeError("raw internal detail"), 500),
            ):
                app.state.capacity = RaisingCapacity(error)
                assert api.post(
                    "/internal/v1/embeddings",
                    headers={"X-Internal-Token": TOKEN},
                    json={
                        "items": [
                            {
                                "id": secret_id,
                                "title": secret_text,
                                "description": "",
                            }
                        ]
                    },
                ).status_code == status
            assert api.get(f"/private-path?token={secret_token}").status_code == 404
    finally:
        logger.removeHandler(handler)

    messages = stream.getvalue().splitlines()
    joined = "\n".join(messages)
    for result in ("success", "validation", "auth", "overload", "timeout", "internal"):
        assert f"result={result}" in joined
    for message in messages:
        assert set(part.split("=", 1)[0] for part in message.split()) == {
            "route",
            "status",
            "duration_ms",
            "result",
            "capacity",
        }
    for forbidden in (
        secret_id,
        secret_text,
        "private description",
        TOKEN,
        secret_token,
        "/private-path",
        "raw internal detail",
        "http://",
        "[[",
    ):
        assert forbidden not in joined


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
