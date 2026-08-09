from __future__ import annotations

from contextlib import asynccontextmanager
import hmac
import logging
import re
import sys
import time

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from starlette.exceptions import HTTPException

from .constants import (
    MAX_REQUEST_BYTES,
    MAX_TOTAL_CODEPOINTS,
    MODEL_DEVICE,
    MODEL_DIMENSION,
    MODEL_DTYPE,
    MODEL_NAME,
    MODEL_NORMALIZED,
    MODEL_REVISION,
    MAX_SEQUENCE_TOKENS,
)
from .model import ModelRuntime
from .inference import (
    Capacity,
    InferenceTimeout,
    OverCapacity,
    QueueTimeout,
)
from .normalization import canonical_text
from .schemas import (
    EmbeddingItemResponse,
    EmbeddingRequest,
    EmbeddingResponse,
    Limits,
    ModelMetadata,
)
from .settings import Settings, load_settings

ITEM_ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$")
PROTECTED = {"/internal/v1/model", "/internal/v1/embeddings"}
REQUEST_LOGGER = logging.getLogger("frux_embedding.requests")


def error_response(status: int, code: str, message: str, headers: dict[str, str] | None = None):
    return JSONResponse(
        status_code=status, content={"code": code, "error": message}, headers=headers
    )


class RequestBoundary:
    def __init__(self, app, settings: Settings, capacity: Capacity):
        self.app = app
        self.settings = settings
        self.capacity = capacity

    async def __call__(self, scope, receive, send):
        if scope["type"] != "http":
            await self.app(scope, receive, send)
            return
        started = time.monotonic()
        scope.setdefault("state", {})["started"] = started
        path = scope.get("path", "")
        route = request_route(scope.get("method", ""), path)
        status = 500

        async def observed_send(message):
            nonlocal status
            if message["type"] == "http.response.start":
                status = int(message["status"])
            await send(message)

        headers = {
            key.decode("latin1").lower(): value.decode("latin1")
            for key, value in scope.get("headers", [])
        }
        try:
            if path in PROTECTED:
                supplied = headers.get("x-internal-token")
                if supplied is None:
                    await error_response(
                        401, "AUTH_INTERNAL_TOKEN_REQUIRED", "internal token required"
                    )(scope, receive, observed_send)
                    return
                if not hmac.compare_digest(supplied.strip(), self.settings.token):
                    await error_response(
                        401, "AUTH_INVALID_INTERNAL_TOKEN", "invalid internal token"
                    )(scope, receive, observed_send)
                    return
            try:
                content_length = int(headers.get("content-length", "0"))
            except ValueError:
                content_length = MAX_REQUEST_BYTES + 1
            if content_length > MAX_REQUEST_BYTES:
                await error_response(
                    413, "REQUEST_TOO_LARGE", "request too large"
                )(scope, receive, observed_send)
                return
            size = 0

            async def bounded_receive():
                nonlocal size
                message = await receive()
                if message["type"] == "http.request":
                    size += len(message.get("body", b""))
                    if size > MAX_REQUEST_BYTES:
                        raise BodyTooLarge
                return message

            try:
                await self.app(scope, bounded_receive, observed_send)
            except BodyTooLarge:
                await error_response(413, "REQUEST_TOO_LARGE", "request too large")(
                    scope, receive, observed_send
                )
        finally:
            duration_ms = max(0, round((time.monotonic() - started) * 1000))
            REQUEST_LOGGER.info(
                "route=%s status=%d duration_ms=%d result=%s capacity=%d",
                route,
                status,
                duration_ms,
                request_result(status),
                self.capacity.live_capacity(),
            )


class BodyTooLarge(Exception):
    pass


def request_route(method: str, path: str) -> str:
    return {
        ("GET", "/health/live"): "live",
        ("GET", "/health/ready"): "ready",
        ("GET", "/internal/v1/model"): "model",
        ("POST", "/internal/v1/embeddings"): "embeddings",
    }.get((method.upper(), path), "unknown")


def request_result(status: int) -> str:
    if 200 <= status < 300:
        return "success"
    if status == 401:
        return "auth"
    if status in {400, 404, 405, 413, 422}:
        return "validation"
    if status == 429:
        return "overload"
    if status == 504:
        return "timeout"
    if status == 503:
        return "unavailable"
    return "internal"


def configure_request_logging(level: str) -> None:
    REQUEST_LOGGER.setLevel(getattr(logging, level.upper(), logging.INFO))
    if not any(
        getattr(handler, "_frux_bounded_request_handler", False)
        for handler in REQUEST_LOGGER.handlers
    ):
        handler = logging.StreamHandler(sys.stderr)
        handler.setFormatter(logging.Formatter("%(message)s"))
        handler._frux_bounded_request_handler = True
        REQUEST_LOGGER.addHandler(handler)
    REQUEST_LOGGER.propagate = False


def metadata() -> ModelMetadata:
    return ModelMetadata(
        model=MODEL_NAME,
        revision=MODEL_REVISION,
        dimension=MODEL_DIMENSION,
        max_sequence_tokens=MAX_SEQUENCE_TOKENS,
        dtype=MODEL_DTYPE,
        normalized=MODEL_NORMALIZED,
        device=MODEL_DEVICE,
        limits=Limits(),
    )


def create_app(
    settings: Settings,
    runtime: ModelRuntime,
    startup_deadline: float | None = None,
) -> FastAPI:
    configure_request_logging(settings.log_level)
    capacity = Capacity(settings, runtime, startup_deadline=startup_deadline)

    @asynccontextmanager
    async def lifespan(_: FastAPI):
        try:
            yield
        finally:
            await capacity.close()

    app = FastAPI(
        docs_url=None,
        redoc_url=None,
        openapi_url=None,
        lifespan=lifespan,
    )
    app.state.settings = settings
    app.state.runtime = runtime
    app.state.capacity = capacity
    app.add_middleware(RequestBoundary, settings=settings, capacity=capacity)

    @app.exception_handler(RequestValidationError)
    async def validation_error(_: Request, __: RequestValidationError):
        return error_response(400, "INVALID_REQUEST", "invalid request")

    @app.exception_handler(BodyTooLarge)
    async def body_too_large(_: Request, __: BodyTooLarge):
        return error_response(413, "REQUEST_TOO_LARGE", "request too large")

    @app.exception_handler(HTTPException)
    async def http_error(_: Request, error: HTTPException):
        if error.status_code == 404:
            return error_response(404, "NOT_FOUND", "not found")
        if error.status_code == 405:
            return error_response(405, "METHOD_NOT_ALLOWED", "method not allowed")
        return error_response(error.status_code, "INVALID_REQUEST", "invalid request")

    @app.exception_handler(Exception)
    async def unexpected(_: Request, __: Exception):
        return error_response(500, "INTERNAL_ERROR", "internal error")

    @app.get("/health/live")
    async def live():
        return {"status": "live"}

    @app.get("/health/ready")
    async def ready():
        if (
            not runtime.ready
            or app.state.capacity.live_capacity() < settings.max_concurrency
        ):
            return JSONResponse(status_code=503, content={"status": "not_ready"})
        return {"status": "ready"}

    @app.get("/internal/v1/model", response_model=ModelMetadata)
    async def model_contract():
        if not runtime.ready:
            return error_response(503, "NOT_READY", "not ready")
        return metadata()

    @app.post("/internal/v1/embeddings", response_model=EmbeddingResponse)
    async def embeddings(request: Request, body: EmbeddingRequest):
        if not runtime.ready:
            return error_response(503, "NOT_READY", "not ready")
        seen: set[str] = set()
        normalized: list[tuple[str, str, str, str]] = []
        total = 0
        for item in body.items:
            if not ITEM_ID.fullmatch(item.id) or item.id in seen:
                return error_response(400, "INVALID_REQUEST", "invalid request")
            seen.add(item.id)
            try:
                title, description, text = canonical_text(item.title, item.description)
            except ValueError:
                return error_response(400, "INVALID_REQUEST", "invalid request")
            total += len(title) + len(description)
            if total > MAX_TOTAL_CODEPOINTS:
                return error_response(400, "INVALID_REQUEST", "invalid request")
            normalized.append((item.id, title, description, text))
        started = getattr(request.state, "started", time.monotonic())
        try:
            vectors = await app.state.capacity.run(
                [item[3] for item in normalized], started
            )
        except OverCapacity:
            return error_response(
                429, "OVER_CAPACITY", "over capacity", {"Retry-After": "1"}
            )
        except QueueTimeout:
            return error_response(
                429, "OVER_CAPACITY", "over capacity", {"Retry-After": "1"}
            )
        except InferenceTimeout:
            return error_response(504, "INFERENCE_TIMEOUT", "inference timeout")
        except Exception:
            return error_response(500, "INTERNAL_ERROR", "internal error")
        return EmbeddingResponse(
            model=MODEL_NAME,
            revision=MODEL_REVISION,
            dimension=MODEL_DIMENSION,
            items=[
                EmbeddingItemResponse(id=item[0], index=index, embedding=vectors[index])
                for index, item in enumerate(normalized)
            ],
        )

    return app
