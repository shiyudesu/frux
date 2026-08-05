## 1. Failure Classification and Topology

- [ ] 1.1 Inventory current consumers and classify their terminal, retryable, and critical-delivery requirements.
- [ ] 1.2 Extend RabbitMQ configuration with versioned source queues, delivery limits, DLXs, DLQs, overflow, and queue-type settings.
- [ ] 1.3 Declare quorum and dead-letter topology with bounded queue lengths and publisher-confirm safety.
- [ ] 1.4 Update consumer acknowledgement logic to reject terminal messages and bound retryable redelivery.
- [ ] 1.5 Add unit tests for error classification, delivery limits, original event identity, and poison-message behavior.

## 2. Controlled Queue Migration

- [ ] 2.1 Add versioned queue bindings without redeclaring existing classic queues.
- [ ] 2.2 Run old and new consumers through an idempotent drain and cutover path.
- [ ] 2.3 Add integration tests for duplicate delivery, dead-letter routing, unavailable DLQ targets, and publisher confirms.
- [ ] 2.4 Document rollback bindings and queue drain criteria for each migrated consumer.

## 3. Operator Inspection and Replay

- [ ] 3.1 Add a server-side RabbitMQ management adapter for queue summaries and bounded redacted previews.
- [ ] 3.2 Add `governance.execute` protected dead-letter summary and preview endpoints.
- [ ] 3.3 Implement single-message replay with route validation, unchanged business payload, original event ID, and new replay ID.
- [ ] 3.4 Acknowledge the dead-letter message only after replay publisher confirmation.
- [ ] 3.5 Commit replay success or failure audit facts and add API-flow tests for forbidden, confirmed, timeout, and duplicate replay paths.

## 4. Observability and Documentation

- [ ] 4.1 Add low-cardinality metrics for retries, exhaustion, DLQ depth, routing failure, replay, and replay failure.
- [ ] 4.2 Add Prometheus alerts and Grafana panels for dead-letter backlog and recovery failures.
- [ ] 4.3 Update RabbitMQ Compose/configuration, governance, monitoring, product, architecture, deployment, and engineering documentation.
- [ ] 4.4 Run targeted MQ tests, RabbitMQ integration tests, the full Go suite, Compose config validation, and strict OpenSpec validation.
