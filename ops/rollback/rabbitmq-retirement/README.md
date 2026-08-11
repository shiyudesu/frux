# Bounded RabbitMQ rollback artifact

This directory is retained only for the seven-day post-retirement rollback window.

**Do not switch consumers to this RabbitMQ configuration while Kafka has pending work.** Freeze
action/view/video/media ingress, wait for every source and retry Group to reach zero lag, and verify
the action/view/publication outboxes have no pending rows before stopping the Kafka-only Worker. If
zero lag cannot be proven, keep the previous release on Kafka-active configuration instead of using
this RabbitMQ rollback config.

- Previous release source: Git commit `fb29a27`
- Previous API/Worker image: set `FRUX_PREVIOUS_API_IMAGE` to the immutable image built from that commit
- Previous configuration: use the included `config.rabbitmq.yaml`, or set `FRUX_PREVIOUS_CONFIG` to an equivalent read-only file
- Broker credentials: provide `FRUX_RABBITMQ_USER`, `FRUX_RABBITMQ_PASSWORD`, and
  `FRUX_RABBITMQ_ERLANG_COOKIE` through the deployment secret

The rollback username and password must contain only URL-safe
`A-Z a-z 0-9 . _ ~ -` characters because the previous release reads them from AMQP URI userinfo.
Generate passwords with `openssl rand -hex 32`, not base64.

Validate the preserved manifest with:

```bash
FRUX_PREVIOUS_API_IMAGE=frux-api:fb29a27 \
FRUX_RABBITMQ_USER=rollback \
FRUX_RABBITMQ_PASSWORD=replace-me \
FRUX_RABBITMQ_ERLANG_COOKIE='replace-with-a-strong-cookie' \
FRUX_INTERNAL_TOKEN='replace-with-a-strong-token-123A!' \
FRUX_JWT_SECRET='replace-with-a-strong-jwt-secret' \
docker compose -f ops/rollback/rabbitmq-retirement/docker-compose.rabbitmq.yml config
```

Do not merge this manifest into the supported Kafka-only Compose file.
