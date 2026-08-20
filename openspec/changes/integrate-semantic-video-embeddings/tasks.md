## 1. Prerequisite Gate and Shared Contract

- [ ] 1.1 Verify recommendation-roadmap steps 1–5 and `migrate-video-workflows-to-kafka` are implemented, accepted, and archived.
- [ ] 1.2 Reconcile the provider-neutral embedder, fixed provider/model/revision/dimension/`semantic-text-v1` identity, privacy, validation, cache, circuit, cost, and quota contracts.
- [ ] 1.3 Add tests preventing provider SDK types or provider calls in API, publication, Feed, ranking, profile, and Kafka-handler paths.

## 2. Full-Identity Job and Vector Persistence

- [ ] 2.1 Add semantic job persistence keyed by video plus provider/model/revision/dimension/canonicalizer with text hash, explicit states, generation, attempts, availability, lease fields, retry-after, bounded error class, and timestamps.
- [ ] 2.2 Add side-by-side semantic vector persistence with the complete provenance tuple, text hash, exact dimension, normalized vector, and no raw text or credential fields.
- [ ] 2.3 Implement stable `SKIP LOCKED` claims, heartbeat, expired-lease reclaim, and generation/token/text-hash fencing for every state/vector mutation.
- [ ] 2.4 Add migrations and tests for duplicate handoff, changed text, stale lease, reclaim, identical no-op, hash coexistence, and separate future identities.

## 3. Hash-Safe Kafka Handoff and Commit Semantics

- [ ] 3.1 Extend accepted publication/hash intake to validate the record, preserve/create `hash-ngram-v1`, compute `semantic-text-v1` hash, and idempotently upsert the semantic job without calling the provider.
- [ ] 3.2 Enforce `hash durable -> semantic job durable -> handler success` and prove publish/Feed never wait for semantic work.
- [ ] 3.3 Implement and test source commit after handoff or acknowledged retry publication, retry commit after handoff/next-retry/DLQ acknowledgement, and poison-record commit only after DLQ acknowledgement.
- [ ] 3.4 Add redelivery, uncertain commit, retry/DLQ publication failure, Feed-group isolation, and changed-text generation tests.

## 4. Leased Semantic Worker

- [ ] 4.1 Re-read and lock current video state under each lease, require published/public eligibility, recompute canonical text/hash, and refresh or terminally classify stale/ineligible work before provider access.
- [ ] 4.2 Invoke the narrow adapter only from the leased worker and conditionally persist/complete the matching full-identity fact in a fenced transaction.
- [ ] 4.3 Implement database retry timing at 5s, 30s, 2m, 10m, 30m, then exponential capped at 2h, honoring bounded `Retry-After` and deterministic jitter.
- [ ] 4.4 Add replica-local claim gating for provider circuit, QPS, quota, budget, and configuration without blocking unrelated worker startup.
- [ ] 4.5 Add outage, throttle, quota, auth, contract, cancellation, lease-loss, source-change, multi-replica, cache-hit, and recovery tests.

## 5. Terminal Operations Requeue and Cleanup

- [ ] 5.1 Define bounded retryable/terminal classifications with no raw provider errors, text, IDs, URLs, or credentials.
- [ ] 5.2 Add an authenticated non-public operator command to inspect bounded counts and requeue selected retry/terminal jobs with generation fencing and audit metrics.
- [ ] 5.3 Add bounded cleanup: succeeded jobs after seven days only with matching vector, terminal jobs after at least 30 days, and no deletion of active jobs/vectors/hash/other identities.
- [ ] 5.4 Add requeue, retention, cleanup fencing, missing-vector, and concurrent-cleanup tests.

## 6. SLA Coverage Metrics and Documentation

- [ ] 6.1 Add bounded metrics for job states/age, claims, leases, retries, requeue, cleanup, provider/circuit/cost/quota outcomes, exact-contract coverage, 15-minute/24-hour SLA, and hash coverage.
- [ ] 6.2 Implement rollout reports requiring three consecutive 24-hour windows at 99% exact-contract coverage, terminal rate at most 0.1%, explained uncovered rows, 100% hash coverage, and no unrelated-worker lag regression.
- [ ] 6.3 Update embedding/video/worker/Kafka/metrics/operations docs for privacy, full provenance, handoff and source/retry/DLQ commits, job states, backoff, requeue, cleanup, SLA, rollout, and rollback.
- [ ] 6.4 Document that the gate validates producer completeness only and adds no historical scan or recommendation consumption.

## 7. Validation

- [ ] 7.1 Run targeted Go tests for job/vector persistence, Kafka handoff/commits, worker fencing/retries, provider failure isolation, requeue/cleanup, metrics, SLA, and redaction.
- [ ] 7.2 Build `./cmd/feed` and `./cmd/worker`, run the complete Go suite, and validate rendered configuration without requiring real provider credentials.
- [ ] 7.3 Confirm zero synchronous provider calls, zero hash-row mutations from semantic work, no local model runtime, and no retrieval/profile/ranking/policy changes.
- [ ] 7.4 Run `openspec validate --all --strict` and inspect the final planning/implementation diff for cross-artifact consistency.
