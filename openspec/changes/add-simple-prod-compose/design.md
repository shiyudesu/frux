## Context

The current worktree contains a strict production prototype with three secured Kafka brokers, ACL provisioning, monitoring, certificate management, and Kafka volume backup. The user selected a simpler personal server deployment whose mental model is the existing local Compose stack running on a remote server, with Rainyun replacing MinIO.

## Goals / Non-Goals

**Goals:**

- Provide a short `prod` deployment path with one Compose file and one environment file.
- Run PostgreSQL, Redis, one Kafka broker, API, Worker, Web, and PostgreSQL backup in Docker.
- Integrate with the server's existing systemd Caddy through loopback-only API/Web ports.
- Use Rainyun `frux1`, persistent volumes, strong secrets, HTTPS, and no public database/message-broker ports.
- Preserve the existing local Compose/MinIO workflow.
- Remove unused strict-production Kafka security, provisioning, monitoring, and backup machinery.

**Non-Goals:**

- Production-grade Kafka durability, authentication, TLS, ACLs, or high availability.
- Prometheus/Grafana in the simple stack.
- Kafka log backup or seamless Kafka migration.
- Claiming the deployment is suitable for critical or multi-user production.

## Decisions

### Add a separate simple prod stack

`apps/docker-compose.prod.yml` will be structurally similar to local Compose but use required environment secrets from `.env.prod`, Rainyun configuration from `config.prod.yaml`, loopback-only API/Web ports, and no MinIO. `apps/docker-compose.yml` remains unchanged.

### Use one internal Kafka broker

Kafka runs as one KRaft broker/controller with no published host port. Application configuration uses Kafka environment `local` with local topic provisioning enabled because Frux production validation intentionally requires three secured brokers.

This deployment is therefore documented as personal/pre-production. Broker or host loss interrupts event processing, and Kafka data is not treated as highly durable.

### Keep minimum useful safeguards

PostgreSQL and Redis require strong passwords and persistent volumes. Prod publishes API and Web only on `127.0.0.1`; the existing host Caddy owns public ports 80/443 and HTTPS. PostgreSQL backup runs periodically.

### Remove the unselected strict prototype

The three-broker production Compose, Kafka certificate/SCRAM/ACL scripts, `kafka-provision` command, production monitoring wiring, strict production OpenSpec implementation code, and related advanced runbook are removed or replaced by the simpler server surfaces.

Rainyun policy documentation remains because Bucket privacy and CORS are still required.

## Risks / Trade-offs

- [Single Kafka broker loses availability and may lose events] → PostgreSQL remains authoritative for durable jobs/outboxes; document that this is not critical production.
- [Internal Kafka traffic is plaintext and unauthenticated] → Publish no Kafka port and keep it on a private Compose network.
- [One server is one failure domain] → Keep PostgreSQL backups outside its data volume and copy them off-host.
- [Simplification removes monitoring dashboards] → Use container health, logs, and `/health`; add monitoring later if needed.

## Migration Plan

1. Add simple `prod` environment, application configuration, Compose loopback ports, and backup files.
2. Remove strict-production Kafka/security/provisioner surfaces that are no longer selected.
3. Update deployment documentation to one short `prod` workflow and label limitations.
4. Validate local Compose remains unchanged and the simple `prod` Compose renders and starts with isolated validation values.
5. With a real domain and Rainyun credentials, run the final HTTPS/upload/playback check.

## Open Questions

- What domain will be used for the server?
