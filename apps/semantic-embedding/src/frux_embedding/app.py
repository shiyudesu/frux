from __future__ import annotations

from contextlib import asynccontextmanager
import hmac
import re
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


def error_response(status: int, code: str, message: str, headers: dict[str, str] | None = None):
    return JSONResponse(
        status_code=status, content={"code": code, "error": message}, headers=headers
    )


class RequestBoundary:
    def __init__(self, app, settings: Settings):
        self.app = app
        self.settings = settings

    async def __call__(self, scope, receive, send):
        if scope["type"] != "http":
            await self.app(scope, receive, send)
            return
        scope.setdefault("state", {})["started"] = time.monotonic()
        path = scope.get("path", "")
        headers = {
            key.decode("latin1").lower(): value.decode("latin1")
            for key, value in scope.get("headers", [])
        }
        if path in PROTECTED:
            supplied = headers.get("x-internal-token")
            if supplied is None:
                await error_response(
                    401, "AUTH_INTERNAL_TOKEN_REQUIRED", "internal token required"
                )(scope, receive, send)
                return
            if not hmac.compare_digest(supplied.strip(), self.settings.token):
                await error_response(
                    401, "AUTH_INVALID_INTERNAL_TOKEN", "invalid internal token"
                )(scope, receive, send)
                return
        try:
            content_length = int(headers.get("content-length", "0"))
        except ValueError:
            content_length = MAX_REQUEST_BYTES + 1
        if content_length > MAX_REQUEST_BYTES:
            await error_response(413, "REQUEST_TOO_LARGE", "request too large")(
                scope, receive, send
            )
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
            await self.app(scope, bounded_receive, send)
        except BodyTooLarge:
            await error_response(413, "REQUEST_TOO_LARGE", "request too large")(
                scope, receive, send
            )


class BodyTooLarge(Exception):
    pass


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


def create_app(settings: Settings, runtime: ModelRuntime) -> FastAPI:
    capacity = Capacity(settings, runtime)

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
    app.add_middleware(RequestBoundary, settings=settings)

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
        if not runtime.ready:
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
