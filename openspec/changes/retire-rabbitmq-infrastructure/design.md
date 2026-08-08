## Context

RabbitMQ currently participates in API optional capabilities, Worker-required startup, five event/command routes, quorum/DLX topology, operator dead-letter APIs, metrics, alerts, dashboards, Compose, configuration, documentation, and tests. The preceding Kafka changes intentionally retain RabbitMQ for per-stream rollback.

Retirement is safe only after Kafka event producers and active groups are stable, PostgreSQL jobs own media/semantic retry state, Kafka failure recovery is operational, and no unresolved RabbitMQ DLQ records remain.

## Goals / Non-Goals

**Goals:**

- Define objective prerequisites for removing RabbitMQ.
- Remove all runtime AMQP and RabbitMQ Management API dependencies.
- Remove old admin endpoints only after Kafka replacements exist.
- Ensure API and Worker startup, Compose, monitoring, and documentation describe one final messaging architecture.
- Preserve historical audit facts and business data.

**Non-Goals:**

- Performing another business event redesign.
- Deleting Kafka retry/DLQ records or PostgreSQL jobs.
- Automatically converting unresolved RabbitMQ payloads to Kafka without operator review.
- Providing a permanent dual-broker mode after retirement.

## Decisions

### Gate retirement on an observation window

Every registered stream must remain in Kafka primary/active mode for at least one documented publication and consumption observation window. The gate requires:

- no RabbitMQ primary publications;
- no active RabbitMQ business consumers;
- legacy source queues at zero ready and unacknowledged records;
- all RabbitMQ DLQs reviewed, replayed, explicitly waived, or exported under an audited procedure;
- Kafka producer error, active-group lag, retry/DLQ, fallback, and duplicate metrics within thresholds;
- PostgreSQL media and semantic job polling proven during lost-wakeup tests.

The gate is checked operationally and documented; code does not silently delete broker resources.

### Remove old RabbitMQ admin APIs as a breaking internal change

Kafka recovery introduces topic/partition/offset endpoints with different response and request shapes. The RabbitMQ queue endpoints are removed rather than kept as aliases because queue names, head previews, destructive acknowledgements, and `x-death` provenance no longer exist.

Existing immutable admin audit facts remain queryable. No payload is copied from RabbitMQ into PostgreSQL solely for retirement.

### Delete AMQP composition rather than preserve a generic broker facade

Application ports remain event- or command-specific, but RabbitMQ adapters, topology, management client, migration modes, and AMQP configuration are deleted. Kafka adapters and PostgreSQL job dispatchers wire directly through those narrow ports.

Alternative: retain a generic broker switch forever. Rejected because it preserves unused complexity and encourages lowest-common-denominator messaging semantics.

### Make Kafka explicit in final startup dependencies

The Worker requires Kafka, PostgreSQL, and Redis for its Kafka consumer responsibilities, while media and semantic job recovery still relies on PostgreSQL polling. The API may degrade optional asynchronous enhancements only where the owning application contract explicitly supports fallback; configuration validation no longer accepts AMQP URLs.

Compose removes the RabbitMQ service, management port, volume, dependencies, and health checks. Kafka remains the only message broker service.

### Remove RabbitMQ-only monitoring and documentation

The RabbitMQ dead-letter dashboard, queue-depth alerts, routing-failure metrics, queue migration modes, and RabbitMQ module document are removed or replaced with Kafka lag/retry/DLQ and durable-job documentation. Architecture and engineering documents explicitly distinguish retained Kafka events from PostgreSQL jobs and wakeup commands.

## Risks / Trade-offs

- [A late RabbitMQ record is stranded] -> Require zero ready/unacknowledged queues and audited DLQ disposition before shutdown.
- [Kafka regression occurs after AMQP code deletion] -> Keep the prior release artifact and infrastructure manifest for deployment rollback during a defined post-retirement window.
- [Removing admin endpoints breaks internal tooling] -> Deploy and document Kafka replacements first, then change clients before removal.
- [Historical docs lose incident context] -> Preserve archived OpenSpec changes and audit facts while updating current operational docs.
- [An indirect AMQP dependency remains] -> Add repository searches, dependency checks, Compose validation, and startup tests that fail on RabbitMQ configuration or imports.

## Migration Plan

1. Complete and validate all prerequisite Kafka changes.
2. Enter a no-Rabbit-primary observation window and verify drain/recovery gates.
3. Stop RabbitMQ publishers and consumers but keep the service running for final inspection.
4. Resolve or export every allowlisted RabbitMQ DLQ record and record the decision.
5. Deploy code without AMQP adapters or old admin routes.
6. Remove RabbitMQ Compose resources, monitoring assets, configuration, dependency, tests, and current documentation.
7. Observe Kafka and durable-job signals through the post-retirement rollback window, then remove external RabbitMQ infrastructure.

Rollback deploys the previous release and restores its RabbitMQ service/configuration. There is no runtime toggle once the AMQP code is removed.

## Open Questions

None.
