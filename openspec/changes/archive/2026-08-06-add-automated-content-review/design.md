## Context

This change follows the review-aware video lifecycle. Frux already uses RabbitMQ and internal-token endpoints, but it does not host a moderation model. The review boundary must accept provider-neutral evidence, remain idempotent under redelivery, and avoid reducing multimodal results to one opaque score.

## Goals / Non-Goals

**Goals:**

- Create exactly one active review case for each video review subject version.
- Persist bounded machine labels, confidence, evidence references, and provenance.
- Route cases deterministically to approve, reject, or human review under a versioned policy.
- Keep provider outages from publishing content or blocking worker recovery.

**Non-Goals:**

- Model inference, frame extraction, OCR, ASR, provider selection, or model training.
- Human queue behavior, appeals, policy editing UI, or arbitrary policy expressions.

## Decisions

### Model case, evidence, and decision separately

`review_case` tracks subject, version, status, and current policy. `review_signal` stores immutable provider/model/label/confidence/evidence facts. `review_decision` stores each derived automated decision and its policy version. Unique keys make result delivery idempotent.

### Identify the reviewed subject explicitly

The video exposes a positive `review_version`. A case is unique on `(video_id, review_version)`. Later content changes can create a new version without rewriting old evidence.

### Trigger intake only when reviewable assets exist

A durable event or reconciliation job creates the case when a pending-review video has its required media baseline. Duplicate events return the same case. The video stays pending when intake fails.

### Use a closed taxonomy and versioned threshold policy

The first policy maps registered labels to reject and human-review thresholds and defines a default outcome. Unknown labels are retained as evidence but cannot silently cause approval. Policy validation follows the existing recommendation-policy pattern: typed bounds, immutable versions, explicit activation.

### Apply automated outcomes atomically

Auto-approval or rejection inserts the decision, closes or escalates the case, and applies the video transition in one database transaction. Human escalation changes only the case status.

Alternative: let the model callback directly update video status. Rejected because it loses policy versioning, evidence history, and provider independence.

## Risks / Trade-offs

- [Provider-specific evidence schemas diverge] -> Store normalized labels plus bounded opaque evidence references, not arbitrary raw results.
- [Threshold mistakes auto-reject valid content] -> Bootstrap conservatively, require policy versioning, and expose outcome metrics.
- [Case creation event is lost] -> Add periodic reconciliation for pending review videos with ready media and no active case.
- [Duplicate callbacks race] -> Use unique provider result keys and transactional case locking.

## Migration Plan

1. Add review tables, policy validation, and bootstrap an inactive or human-routing policy.
2. Add case intake and reconciliation without changing video states.
3. Add internal result ingestion and metrics.
4. Activate routing only after synthetic and API-flow tests cover each threshold path.
5. Roll back by disabling the active policy; pending cases remain review-gated.

## Open Questions

- Which external moderation provider or in-house service will produce the first normalized result.
