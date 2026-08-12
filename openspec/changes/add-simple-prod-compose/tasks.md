## 1. Simple Server Stack

- [x] 1.1 Add a concise `.env.prod.example` with domain, application, PostgreSQL, Redis, Rainyun, backup, and optional image variables.
- [x] 1.2 Add `config.prod.yaml` using container PostgreSQL/Redis, one local-mode Kafka broker, and Rainyun `frux1`.
- [x] 1.3 Add `docker-compose.prod.yml` with PostgreSQL, Redis, one Kafka, API, Worker, Web, and PostgreSQL backup; bind API/Web only to loopback ports.
- [x] 1.4 Document the existing host Caddy site block and retain the atomic PostgreSQL backup script.

## 2. Remove Unselected Strict Prototype

- [x] 2.1 Remove the three-broker production Compose, strict production environment/configuration, Kafka TLS/SCRAM/ACL scripts, and advanced production validation scripts.
- [x] 2.2 Remove the unused `kafka-provision` command, Docker image binary, and explicit production ACL provisioning code/tests.
- [x] 2.3 Remove or supersede the abandoned strict-production OpenSpec artifacts and runbook without changing the Rainyun integration plan.

## 3. Documentation

- [x] 3.1 Replace deployment instructions with the short simple `prod` workflow and exact startup command.
- [x] 3.2 Document that the stack is personal/pre-production, single-host, single-Kafka, internally plaintext, and not highly available.
- [x] 3.3 Keep Rainyun CORS, private Bucket, `media/*`, and one-database-per-Bucket safeguards.

## 4. Verification

- [x] 4.1 Render and inspect Prod Compose to confirm Rainyun is used, MinIO is absent, only loopback API/Web ports are published, and local Compose remains unchanged.
- [x] 4.2 Start an isolated simple `prod` validation stack through API/Worker health using non-secret test values.
- [x] 4.3 Run backend tests/build, frontend tests/build, local/`prod` Compose validation, and `openspec validate --all --strict`.
- [ ] 4.4 With the real domain and Rainyun credentials, deploy and verify HTTPS, upload, processing, playback, and backup.
