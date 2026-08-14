## Why

The strict single-server production prototype adds operational complexity that the current personal deployment does not need. The selected goal is a straightforward server installation that mirrors local Compose, swaps MinIO for Rainyun, and adds only the minimum public-entry, secret, persistence, and database-backup safeguards.

## What Changes

- Add a simple `prod` Compose stack with one PostgreSQL, Redis, Kafka, API, Worker, Web, and PostgreSQL backup service.
- Bind API and Web only to loopback ports so the server's existing host Caddy can route the Frux domain.
- Use Rainyun bucket `frux1` instead of MinIO while preserving the separate local development Compose.
- Use strong environment-injected secrets, private container networking, persistent volumes, HTTPS, and basic PostgreSQL backup.
- Run Kafka as one internal plaintext KRaft broker with local topic provisioning and no public port.
- Remove the unselected three-broker Kafka TLS/SCRAM/ACL provisioner, production monitoring, Kafka certificate, and Kafka volume-backup implementation.
- Explicitly label the simple stack as personal/pre-production rather than highly available production.

## Capabilities

### New Capabilities

- `simple-server-deployment`: Defines the minimal one-server Docker deployment, security boundary, persistence, backup, Rainyun integration, and accepted limitations.

### Modified Capabilities

None.

## Impact

- Replaces the current complex production Compose prototype with simpler `prod`-named Compose and configuration files.
- Removes unused Kafka provisioning code and production-only Kafka security scripts.
- Updates `prod` deployment and Rainyun documentation.
- Leaves the default local Compose and MinIO workflow unchanged.
