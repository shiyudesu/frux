## 1. Outbound Traffic Baseline

- [x] 1.1 Add registered low-cardinality byte counters for processing source reads, legacy repair reads, protected preview reads, and estimated public Range/full requests
- [x] 1.2 Instrument existing source, cover, promotion, protection, moderation, and public-delivery paths to establish a before-change baseline
- [x] 1.3 Add metrics tests that reject user, video, asset, URL, and object-key labels

## 2. Direct Final Output Publication

- [x] 2.1 Replace processed-video temporary object upload/download with deterministic final-key HEAD/reuse/PUT/verify logic
- [x] 2.2 Preserve fenced finalization and delayed orphan cleanup when final PUT succeeds before PostgreSQL commit
- [x] 2.3 Reject deterministic-key size or checksum conflicts without overwriting the existing file
- [x] 2.4 Update progress reporting so the single final PUT represents output upload progress
- [x] 2.5 Add unit and integration tests proving one output PUT, zero temporary GETs, idempotent reuse, conflict handling, cancellation, and orphan recovery

## 3. No-Copy Cover Completion

- [x] 3.1 Record a validated uploaded cover key directly as the ready protected cover variant
- [x] 3.2 Remove cover body GET/PUT duplication while preserving metadata, owner preview, publication, and cleanup behavior
- [x] 3.3 Add tests for cover replay, public exposure, owner access, shared-key cleanup deduplication, and missing upload metadata

## 4. Logical Public Exposure

- [x] 4.1 Add nullable exposure-generation persistence to media variants and register migration/index support
- [x] 4.2 Add v3 virtual exposure URL construction that contains generation and variant identity but no storage key
- [x] 4.3 Add a public-media resolver that validates generation, variant public state, and current video eligibility before returning the protected storage key
- [x] 4.4 Replace new promotion/protection body copies with atomic public flag/generation updates and new-generation restore behavior
- [x] 4.5 Keep owner, reviewer, moderation, and private playback on the unchanged protected storage key with `private, no-store`
- [x] 4.6 Add transaction, authorization, lifecycle, concurrent generation, and no-body-copy tests

## 5. Legacy Exposure Migration and Reconciliation

- [x] 5.1 Parse existing `media/v2` keys into protected keys and legacy generations without changing completed video metadata prematurely
- [x] 5.2 Add bounded reconciliation that verifies or repairs the protected counterpart before switching a legacy public variant to v3 identity
- [x] 5.3 Retain old public objects through the 30-minute cache window and schedule delayed cleanup after successful migration
- [x] 5.4 Keep existing v2 public URLs readable during rollout and rollback
- [x] 5.5 Add PostgreSQL/object-store tests for successful migration, missing protected repair, interrupted migration, cleanup retry, and rollback compatibility

## 6. Bounded Browser HTTP Caching

- [x] 6.1 Add a public-only presigned GET path whose response cache control is `public, max-age=1800, must-revalidate`
- [x] 6.2 Cache eligible v3 redirects for less than the signed URL lifetime and add a bounded in-memory signed-URL reuse cache
- [x] 6.3 Keep HEAD, Range, ETag, content length, and partial-response behavior correct for virtual exposure URLs
- [x] 6.4 Keep public-ineligible and protected responses non-cacheable and deny new redirects immediately
- [x] 6.5 Bind Web playback source revision to exposure generation without adding Service Worker or persistent whole-video storage
- [x] 6.6 Add browser/API tests for 25-minute redirect reuse, 30-minute signed URL expiry and revalidation, Range seeking, generation changes, take-down, and protected-preview isolation

## 7. Cleanup and Documentation

- [x] 7.1 Remove obsolete temporary-media and physical-promotion helpers only after legacy callers are migrated
- [x] 7.2 Update media, architecture, engineering, playback, monitoring, deployment, operations, optimization, and Rainyun documentation with before/after traffic diagrams

## 8. Validation

- [x] 8.1 Run targeted processor, upload, catalog, public-media, lifecycle, reconciliation, migration, API-flow, player, and browser tests
- [x] 8.2 Run full Go tests/vet/build, Web lint/tests/build, Compose validation, and strict OpenSpec validation
- [x] 8.3 Resolve the staged Rainyun billing comparison as not applicable to the fresh NAT deployment, which starts with an empty PostgreSQL database and MinIO bucket and migrates no legacy public variants
