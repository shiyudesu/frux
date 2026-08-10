from pathlib import Path

import yaml


def test_compose_semantic_service_contract():
    compose = yaml.safe_load(
        (Path(__file__).parents[2] / "docker-compose.yml").read_text()
    )
    service = compose["services"]["semantic-embedding"]
    worker = compose["services"]["worker"]
    assert service["expose"] == ["8081"]
    assert "ports" not in service
    assert service["read_only"] is True
    assert service["cap_drop"] == ["ALL"]
    assert service["security_opt"] == ["no-new-privileges:true"]
    assert service.get("depends_on") is None
    assert worker["depends_on"]["semantic-embedding"]["condition"] == "service_started"
    assert worker["environment"]["FRUX_INTERNAL_TOKEN"].startswith(
        "${FRUX_INTERNAL_TOKEN:"
    )
    assert worker["environment"]["FRUX_SEMANTIC_EMBEDDING_URL"].endswith(
        "http://semantic-embedding:8081}"
    )
