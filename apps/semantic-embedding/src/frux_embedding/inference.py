from __future__ import annotations

import asyncio
import multiprocessing
import os
import threading
import time
from typing import Any

from .constants import STARTUP_TIMEOUT_SECONDS


class WorkerTimeout(Exception):
    pass


class WorkerUnavailable(Exception):
    pass


def _silence_worker_output() -> None:
    descriptor = os.open(os.devnull, os.O_WRONLY)
    try:
        os.dup2(descriptor, 1)
        os.dup2(descriptor, 2)
    finally:
        os.close(descriptor)


def _worker_main(connection, runtime) -> None:
    _silence_worker_output()
    try:
        runtime.load()
    except BaseException:
        try:
            connection.send(("startup_error", None))
        except BaseException:
            pass
        connection.close()
        return
    connection.send(("ready", None))
    while True:
        try:
            command = connection.recv()
        except (EOFError, OSError):
            break
        if command is None:
            break
        request_id, texts = command
        try:
            result = runtime.encode(texts)
        except BaseException:
            try:
                connection.send(("error", request_id))
            except BaseException:
                break
            continue
        try:
            connection.send(("success", request_id, result))
        except BaseException:
            break
    connection.close()


class InferenceWorker:
    def __init__(self, process_context, runtime) -> None:
        parent, child = process_context.Pipe(duplex=True)
        process = process_context.Process(
            target=_worker_main,
            args=(child, runtime),
            name="semantic-embedding-inference",
            daemon=True,
        )
        process.start()
        child.close()
        self.connection = parent
        self.process = process

    def wait_ready(self, startup_timeout: float) -> None:
        if not self.connection.poll(startup_timeout):
            self.terminate()
            raise WorkerUnavailable
        try:
            status, _ = self.connection.recv()
        except (EOFError, OSError):
            self.terminate()
            raise WorkerUnavailable from None
        if status != "ready":
            self.terminate()
            raise WorkerUnavailable

    @property
    def pid(self) -> int | None:
        return self.process.pid

    def infer(self, request_id: int, texts: list[str], timeout: float):
        try:
            self.connection.send((request_id, texts))
        except (BrokenPipeError, EOFError, OSError):
            raise WorkerUnavailable from None
        if timeout <= 0 or not self.connection.poll(timeout):
            raise WorkerTimeout
        try:
            response = self.connection.recv()
        except (EOFError, OSError):
            raise WorkerUnavailable from None
        if len(response) == 3 and response[:2] == ("success", request_id):
            return response[2]
        raise WorkerUnavailable

    def terminate(self) -> None:
        try:
            self.connection.close()
        except BaseException:
            pass
        if self.process.is_alive():
            self.process.terminate()
            self.process.join(timeout=1)
        if self.process.is_alive():
            self.process.kill()
            self.process.join(timeout=1)
        else:
            self.process.join(timeout=0)


class ProcessPool:
    def __init__(
        self,
        runtime,
        size: int,
        startup_deadline: float | None = None,
    ) -> None:
        self.runtime = runtime
        self.size = size
        self.context = multiprocessing.get_context("spawn")
        self.available: asyncio.Queue[InferenceWorker] = asyncio.Queue(maxsize=size)
        self.workers: set[InferenceWorker] = set()
        self.starting: set[InferenceWorker] = set()
        self.state_lock = threading.Lock()
        self.replacements: set[asyncio.Task[Any]] = set()
        self.monitor: asyncio.Task[Any] | None = None
        self.closed = False
        self.closed_event = asyncio.Event()
        deadline = startup_deadline or time.monotonic() + STARTUP_TIMEOUT_SECONDS
        try:
            for _ in range(size):
                worker = self._new_worker(deadline)
                self.workers.add(worker)
                self.available.put_nowait(worker)
        except BaseException:
            for worker in list(self.workers):
                worker.terminate()
            self.workers.clear()
            raise
        try:
            self.start_monitor()
        except RuntimeError:
            pass

    def _new_worker(self, deadline: float) -> InferenceWorker:
        with self.state_lock:
            if self.closed:
                raise WorkerUnavailable
        worker = InferenceWorker(self.context, self.runtime)
        with self.state_lock:
            if self.closed:
                worker.terminate()
                raise WorkerUnavailable
            self.starting.add(worker)
        try:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                worker.terminate()
                raise WorkerUnavailable
            worker.wait_ready(remaining)
            return worker
        finally:
            with self.state_lock:
                self.starting.discard(worker)

    async def acquire(self, timeout: float) -> InferenceWorker:
        self.start_monitor()
        try:
            return await asyncio.wait_for(self.available.get(), timeout)
        except TimeoutError:
            raise

    def start_monitor(self) -> None:
        if self.closed or (self.monitor is not None and not self.monitor.done()):
            return
        self.monitor = asyncio.get_running_loop().create_task(
            self._monitor_idle_workers()
        )

    async def _monitor_idle_workers(self) -> None:
        while not self.closed:
            try:
                await asyncio.wait_for(self.closed_event.wait(), 0.05)
                return
            except TimeoutError:
                pass
            available: list[InferenceWorker] = []
            dead: list[InferenceWorker] = []
            while True:
                try:
                    worker = self.available.get_nowait()
                except asyncio.QueueEmpty:
                    break
                if worker.process.is_alive():
                    available.append(worker)
                else:
                    dead.append(worker)
            if not self.closed:
                for worker in available:
                    self.available.put_nowait(worker)
                for worker in dead:
                    self.recycle(worker)

    def release(self, worker: InferenceWorker) -> None:
        if not self.closed:
            self.available.put_nowait(worker)

    def recycle(self, worker: InferenceWorker) -> None:
        worker.terminate()
        self.workers.discard(worker)
        if self.closed:
            return
        task = asyncio.create_task(self._replace())
        self.replacements.add(task)
        task.add_done_callback(self.replacements.discard)

    async def _replace(self) -> None:
        delays = (0.0, 0.1, 0.5, 1.0, 2.0, 5.0)
        attempt = 0
        while not self.closed:
            delay = delays[min(attempt, len(delays) - 1)]
            if delay > 0:
                try:
                    await asyncio.wait_for(self.closed_event.wait(), delay)
                    return
                except TimeoutError:
                    pass
            try:
                worker = await asyncio.to_thread(
                    self._new_worker,
                    time.monotonic() + min(30.0, STARTUP_TIMEOUT_SECONDS),
                )
            except BaseException:
                attempt += 1
                continue
            if self.closed:
                worker.terminate()
                return
            self.workers.add(worker)
            self.available.put_nowait(worker)
            return

    async def close(self) -> None:
        with self.state_lock:
            self.closed = True
            starting = list(self.starting)
        self.closed_event.set()
        monitor = self.monitor
        if monitor is not None:
            await asyncio.gather(monitor, return_exceptions=True)
        workers = list(self.workers) + starting
        self.workers.clear()
        await asyncio.gather(
            *(asyncio.to_thread(worker.terminate) for worker in workers),
            return_exceptions=True,
        )
        tasks = list(self.replacements)
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)

    def pids(self) -> set[int]:
        with self.state_lock:
            workers = list(self.workers)
        return {
            worker.pid
            for worker in workers
            if worker.pid is not None and worker.process.is_alive()
        }

    def live_capacity(self) -> int:
        with self.state_lock:
            workers = list(self.workers)
        return sum(worker.process.is_alive() for worker in workers)


class Capacity:
    def __init__(
        self,
        settings,
        runtime,
        startup_deadline: float | None = None,
    ) -> None:
        self.settings = settings
        self.pool = ProcessPool(
            runtime,
            settings.max_concurrency,
            startup_deadline=startup_deadline,
        )
        self.lock = asyncio.Lock()
        self.admitted = 0
        self.sequence = 0

    async def run(self, texts: list[str], started: float):
        if self._remaining(started) <= 0:
            raise InferenceTimeout
        async with self.lock:
            if self._remaining(started) <= 0:
                raise InferenceTimeout
            if self.admitted >= self.settings.max_concurrency + self.settings.max_queue:
                raise OverCapacity
            self.admitted += 1
            self.sequence += 1
            request_id = self.sequence
        worker = None
        try:
            remaining = self._remaining(started)
            if remaining <= 0:
                raise InferenceTimeout
            queue_timeout = self.settings.queue_timeout_ms / 1000
            deadline_limited = remaining <= queue_timeout
            try:
                worker = await self.pool.acquire(min(queue_timeout, remaining))
            except TimeoutError as error:
                if deadline_limited or self._remaining(started) <= 0:
                    raise InferenceTimeout from error
                raise QueueTimeout from error
            remaining = self._remaining(started)
            if remaining <= 0:
                self.pool.release(worker)
                worker = None
                raise InferenceTimeout
            future = asyncio.create_task(
                asyncio.to_thread(worker.infer, request_id, texts, remaining)
            )
            try:
                result = await future
            except WorkerTimeout as error:
                self.pool.recycle(worker)
                worker = None
                raise InferenceTimeout from error
            except asyncio.CancelledError:
                self.pool.recycle(worker)
                worker = None
                raise
            except WorkerUnavailable:
                self.pool.recycle(worker)
                worker = None
                raise
            self.pool.release(worker)
            worker = None
            return result
        finally:
            if worker is not None:
                self.pool.release(worker)
            async with self.lock:
                self.admitted -= 1

    def _remaining(self, started: float) -> float:
        return self.settings.request_timeout_ms / 1000 - (
            time.monotonic() - started
        )

    async def close(self) -> None:
        await self.pool.close()

    def start(self) -> None:
        self.pool.start_monitor()

    def worker_pids(self) -> set[int]:
        return self.pool.pids()

    def live_capacity(self) -> int:
        return self.pool.live_capacity()


class OverCapacity(Exception):
    pass


class QueueTimeout(Exception):
    pass


class InferenceTimeout(Exception):
    pass
