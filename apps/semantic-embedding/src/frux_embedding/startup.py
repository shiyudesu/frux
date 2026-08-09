from __future__ import annotations

from dataclasses import dataclass
from enum import Enum
import queue
import sys
import threading
from typing import TextIO

from .constants import STARTUP_TIMEOUT_SECONDS


class StartupFailure(str, Enum):
    CONFIGURATION = "configuration"
    MODEL_ARTIFACT = "model_artifact"
    MODEL_METADATA = "model_metadata"
    FIXTURE_CONTRACT = "fixture_contract"
    PRELOAD_TIMEOUT = "preload_timeout"
    DEPENDENCY = "dependency"
    INTERNAL = "internal"


class StartupFailureError(RuntimeError):
    def __init__(self, category: StartupFailure):
        super().__init__("semantic embedding startup failed")
        self.category = category


@dataclass(frozen=True)
class StartupResult:
    failure: StartupFailure | None = None

    @property
    def succeeded(self) -> bool:
        return self.failure is None


def preload_runtime(runtime, timeout_seconds: float) -> StartupResult:
    outcome: queue.Queue[Exception | None] = queue.Queue(maxsize=1)

    def load() -> None:
        try:
            runtime.load()
        except Exception as error:
            outcome.put(error)
        else:
            outcome.put(None)

    thread = threading.Thread(
        target=load,
        name="semantic-embedding-preload",
        daemon=True,
    )
    thread.start()
    try:
        error = outcome.get(timeout=timeout_seconds)
    except queue.Empty:
        return StartupResult(StartupFailure.PRELOAD_TIMEOUT)
    if error is None:
        return StartupResult()
    if isinstance(error, StartupFailureError):
        return StartupResult(error.category)
    return StartupResult(StartupFailure.DEPENDENCY)


def report_failure(result: StartupResult, stream: TextIO | None = None) -> None:
    if stream is None:
        stream = sys.stderr
    category = result.failure or StartupFailure.INTERNAL
    stream.write(f"semantic_embedding_startup_failed category={category.value}\n")
    stream.flush()


def run_server(
    *,
    settings_loader=None,
    runtime_factory=None,
    app_factory=None,
    server_runner=None,
    startup_timeout_seconds: float = STARTUP_TIMEOUT_SECONDS,
) -> int:
    if settings_loader is None:
        try:
            from .settings import load_settings

            settings_loader = load_settings
        except Exception:
            report_failure(StartupResult(StartupFailure.DEPENDENCY))
            return 1
    try:
        settings = settings_loader()
    except Exception:
        report_failure(StartupResult(StartupFailure.CONFIGURATION))
        return 1

    if runtime_factory is None:
        try:
            from .model import ModelRuntime

            runtime_factory = ModelRuntime
        except Exception:
            report_failure(StartupResult(StartupFailure.DEPENDENCY))
            return 1
    try:
        runtime = runtime_factory(settings.model_path, settings.fixture_path)
    except Exception:
        report_failure(StartupResult(StartupFailure.DEPENDENCY))
        return 1

    result = preload_runtime(runtime, startup_timeout_seconds)
    if not result.succeeded:
        report_failure(result)
        return 1

    try:
        if app_factory is None:
            from .app import create_app

            app_factory = create_app
        app = app_factory(settings, runtime)
        if server_runner is None:
            import uvicorn

            def server_runner(application, configuration):
                uvicorn.run(
                    application,
                    host=configuration.bind_host,
                    port=configuration.port,
                    workers=1,
                    log_level=configuration.log_level.lower(),
                    access_log=False,
                    server_header=False,
                    date_header=False,
                )

        server_runner(app, settings)
    except Exception:
        report_failure(StartupResult(StartupFailure.INTERNAL))
        return 1
    return 0
