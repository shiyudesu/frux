## Why

Once videos have a review-gated lifecycle, Frux needs a deterministic way to create review work and consume automated moderation signals without coupling the video domain to a particular model provider. A small machine-review change establishes that boundary before human operations are added.

## What Changes

- Create one idempotent review case for each reviewable video version.
- Accept authenticated internal machine-review results containing bounded labels, scores, evidence references, model version, and policy version.
- Evaluate versioned thresholds to auto-approve, auto-reject, or route a case to human review.
- Persist every machine decision and signal as immutable review evidence.
- Make unavailable or malformed moderation results observable and retryable without publishing the video.
- Exclude model hosting, frame extraction, OCR/ASR implementation, human assignment, appeals, and admin UI.

## Capabilities

### New Capabilities

- `automated-content-review`: Idempotent review-case intake, machine evidence, and policy-based routing.

### Modified Capabilities

None.

## Impact

This adds review domain/application/persistence/HTTP packages, internal service-authenticated endpoints, policy bootstrap, worker or event integration, migrations, metrics, tests, and review documentation. It depends on `establish-video-review-lifecycle`.
