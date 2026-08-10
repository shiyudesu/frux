from __future__ import annotations

from dataclasses import dataclass
import ipaddress
import os
import unicodedata


SUPPORTED = {
    "FRUX_EMBEDDING_BIND_HOST",
    "FRUX_EMBEDDING_PORT",
    "FRUX_EMBEDDING_MAX_CONCURRENCY",
    "FRUX_EMBEDDING_MAX_QUEUE",
    "FRUX_EMBEDDING_QUEUE_TIMEOUT_MS",
    "FRUX_EMBEDDING_REQUEST_TIMEOUT_MS",
    "FRUX_EMBEDDING_LOG_LEVEL",
    "FRUX_EMBEDDING_MODEL_PATH",
    "FRUX_EMBEDDING_FIXTURE_PATH",
}


@dataclass(frozen=True)
class Settings:
    token: str
    bind_host: str = "0.0.0.0"
    port: int = 8081
    max_concurrency: int = 2
    max_queue: int = 8
    queue_timeout_ms: int = 2_000
    request_timeout_ms: int = 15_000
    log_level: str = "INFO"
    model_path: str = "/opt/frux/models/paraphrase-multilingual-MiniLM-L12-v2"
    fixture_path: str = "/app/fixtures/model-fixtures.json"


def strong_token(value: str) -> bool:
    value = value.strip()
    if (
        len(value) < 32
        or value.lower() == "replace-with-internal-token"
        or not ascii_safe_token(value)
    ):
        return False
    classes = {
        "lower": any(char.islower() for char in value),
        "upper": any(char.isupper() for char in value),
        "digit": any(char.isdigit() for char in value),
        "other": any(not char.isalnum() for char in value),
    }
    return sum(classes.values()) >= 3


def ascii_safe_token(value: str) -> bool:
    return value.isascii() and all(32 <= ord(char) <= 126 for char in value)


def _bounded_int(env: dict[str, str], name: str, default: int, low: int, high: int) -> int:
    try:
        value = int(env.get(name, str(default)))
    except ValueError as error:
        raise ValueError("invalid embedding configuration") from error
    if value < low or value > high:
        raise ValueError("invalid embedding configuration")
    return value


def load_settings(env: dict[str, str] | None = None) -> Settings:
    values = dict(os.environ if env is None else env)
    unknown = {name for name in values if name.startswith("FRUX_EMBEDDING_")} - SUPPORTED
    if unknown:
        raise ValueError("invalid embedding configuration")
    token = values.get("FRUX_INTERNAL_TOKEN", "").strip()
    if not strong_token(token):
        raise ValueError("invalid internal token configuration")
    host = values.get("FRUX_EMBEDDING_BIND_HOST", "0.0.0.0").strip()
    try:
        ipaddress.ip_address(host)
    except ValueError as error:
        raise ValueError("invalid embedding configuration") from error
    concurrency = _bounded_int(values, "FRUX_EMBEDDING_MAX_CONCURRENCY", 2, 1, 2)
    queue = _bounded_int(values, "FRUX_EMBEDDING_MAX_QUEUE", 8, 0, 8)
    queue_timeout = _bounded_int(values, "FRUX_EMBEDDING_QUEUE_TIMEOUT_MS", 2_000, 100, 2_000)
    request_timeout = _bounded_int(
        values, "FRUX_EMBEDDING_REQUEST_TIMEOUT_MS", 15_000, 1_000, 15_000
    )
    if request_timeout <= queue_timeout:
        raise ValueError("invalid embedding configuration")
    log_level = values.get("FRUX_EMBEDDING_LOG_LEVEL", "INFO").strip().upper()
    if log_level not in {"WARNING", "INFO", "DEBUG"}:
        raise ValueError("invalid embedding configuration")
    for name in (
        "OMP_NUM_THREADS",
        "MKL_NUM_THREADS",
        "OPENBLAS_NUM_THREADS",
        "NUMEXPR_NUM_THREADS",
    ):
        if values.get(name, "2") != "2":
            raise ValueError("invalid thread configuration")
    if values.get("TOKENIZERS_PARALLELISM", "false").lower() != "false":
        raise ValueError("invalid tokenizer configuration")
    return Settings(
        token=token,
        bind_host=host,
        port=_bounded_int(values, "FRUX_EMBEDDING_PORT", 8081, 1, 65_535),
        max_concurrency=concurrency,
        max_queue=queue,
        queue_timeout_ms=queue_timeout,
        request_timeout_ms=request_timeout,
        log_level=log_level,
        model_path=values.get(
            "FRUX_EMBEDDING_MODEL_PATH",
            "/opt/frux/models/paraphrase-multilingual-MiniLM-L12-v2",
        ),
        fixture_path=values.get(
            "FRUX_EMBEDDING_FIXTURE_PATH", "/app/fixtures/model-fixtures.json"
        ),
    )
