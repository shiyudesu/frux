import json
import os
from pathlib import Path
import subprocess
import time

import pytest

APPS = Path(__file__).parents[2]
COMPOSE = [
    "docker",
    "compose",
    "-f",
    str(APPS / "docker-compose.yml"),
    "-f",
    str(Path(__file__).with_name("docker-compose.semantic-outage.yml")),
]


def run(*args, check=True):
    return subprocess.run(
        [*COMPOSE, *args],
        cwd=APPS,
        check=check,
        text=True,
        capture_output=True,
        env={
            **os.environ,
            "FRUX_INTERNAL_TOKEN": "Compose-Semantic-Outage-Token-123!",
        },
    )


def sql(query):
    return run(
        "exec",
        "-T",
        "postgres",
        "psql",
        "-U",
        "frux",
        "-d",
        "frux",
        "-Atc",
        query,
    ).stdout.strip()


def wait_for(assertion, timeout=90):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        try:
            last = assertion()
            if last:
                return
        except (subprocess.CalledProcessError, OSError):
            pass
        time.sleep(1)
    raise AssertionError(f"condition did not recover; last={last!r}")


@pytest.mark.skipif(
    os.getenv("FRUX_RUN_COMPOSE_SEMANTIC_OUTAGE") != "1",
    reason="set FRUX_RUN_COMPOSE_SEMANTIC_OUTAGE=1 for the destructive Compose test",
)
def test_compose_semantic_outage_hash_handoff_and_recovery():
    if run("ps", "-q").stdout.strip():
        pytest.fail("stop the existing Frux Compose stack before this opt-in test")
    try:
        run(
            "up",
            "-d",
            "--build",
            "postgres",
            "redis",
            "rabbitmq",
            "kafka",
            "minio",
            "minio-init",
            "semantic-embedding",
            "worker",
        )
        wait_for(lambda: run("ps", "--status", "running", "-q", "worker").stdout.strip())
        wait_for(
            lambda: subprocess.run(
                [
                    "curl",
                    "--fail",
                    "--silent",
                    "--user",
                    "guest:guest",
                    "http://127.0.0.1:15672/api/exchanges/%2F/frux.video",
                ],
                check=False,
                capture_output=True,
            ).returncode
            == 0
        )
        now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        event = json.dumps(
            {
                "event_id": "video-published:990001:1",
                "video_id": 990001,
                "author_id": 1,
                "title": "Compose semantic outage",
                "description": "durable hash first",
                "media_url": "",
                "cover_url": "",
                "published_at": now,
                "occurred_at": now,
            }
        )
        body = json.dumps(
            {
                "properties": {},
                "routing_key": "video.published",
                "payload": event,
                "payload_encoding": "string",
            }
        ).encode()
        published = subprocess.run(
            [
                "curl",
                "--fail",
                "--silent",
                "--user",
                "guest:guest",
                "--header",
                "Content-Type: application/json",
                "--data-binary",
                body.decode(),
                "http://127.0.0.1:15672/api/exchanges/%2F/frux.video/publish",
            ],
            check=True,
            text=True,
            capture_output=True,
        )
        assert json.loads(published.stdout)["routed"] is True

        wait_for(
            lambda: sql(
                "SELECT "
                "(SELECT COUNT(*) FROM video_embedding WHERE video_id=990001 "
                "AND model='hash-ngram-v1') || ':' || "
                "(SELECT COUNT(*) FROM semantic_embedding_job WHERE video_id=990001) || ':' || "
                "(SELECT COUNT(*) FROM video_embedding WHERE video_id=990001 "
                "AND model='semantic-minilm-l12-v2@e8f8c211226b894f')"
            )
            == "1:1:0"
        )
        run("exec", "-T", "-u", "0", "semantic-embedding", "touch", "/state/ready")
        wait_for(
            lambda: sql(
                "SELECT "
                "(SELECT COUNT(*) FROM video_embedding WHERE video_id=990001 "
                "AND model='hash-ngram-v1') || ':' || "
                "(SELECT COUNT(*) FROM video_embedding WHERE video_id=990001 "
                "AND model='semantic-minilm-l12-v2@e8f8c211226b894f') || ':' || "
                "(SELECT COUNT(*) FROM semantic_embedding_job WHERE video_id=990001 "
                "AND state='completed')"
            )
            == "1:1:1",
            timeout=120,
        )
    finally:
        run("down", "-v", "--remove-orphans", check=False)
