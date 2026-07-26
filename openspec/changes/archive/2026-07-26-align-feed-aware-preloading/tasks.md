## 1. Preload Policy and Types

- [x] 1.1 Define strict Web types for preload generations, resource keys, readiness, effective policy, and acquired resource handles.
- [x] 1.2 Implement network/save-data policy normalization and derive bounded preload count and buffer targets.
- [x] 1.3 Add unit tests for WiFi/5G, 4G/default, slow-network, offline, and save-data policies.

## 2. Preload Controller

- [x] 2.1 Implement a bounded `FeedPreloadController` that owns active, previous, and forward candidate resources.
- [x] 2.2 Add retained native-video preparation with metadata, buffered-range, `canplay`, error, and timeout state.
- [x] 2.3 Implement generation cancellation, abort handling, source-revision invalidation, retry cooldown, and LRU cleanup.
- [x] 2.4 Add controller tests for acquisition, reuse, reprioritization, cancellation, failure isolation, and resource release.

## 3. Feed Integration

- [x] 3.1 Expose active Feed scene, request generation, ordered items, index, and pagination state to a dedicated preload hook.
- [x] 3.2 Derive preload candidates directly from the current ordered Feed list.
- [x] 3.3 Trigger Feed pagination early enough to populate the configured preload window near page boundaries.
- [x] 3.4 Allow `VideoStage` to adopt or reuse a prepared resource without duplicating media listeners.
- [x] 3.5 Remove disposable hidden metadata probes from the primary Web path.

## 4. Compatibility Endpoint

- [x] 4.1 Stop primary Web use of `/api/preload-videos` while preserving the endpoint for compatible clients.
- [x] 4.2 Document and test the endpoint as compatibility/refill behavior rather than scene ordering authority.
- [x] 4.3 Add metrics or debug state for preload attempts, readiness, reuse, cancellation, and failure without high-cardinality labels.

## 5. Verification and Documentation

- [x] 5.1 Add Feed tests proving recommendation, hot, following, and timeline preload order matches their returned items.
- [x] 5.2 Add browser checks for forward/back switching, scene changes, slow network, save-data, and memory cleanup.
- [x] 5.3 Update feed, playback, optimization, UI/UX, engineering, and performance-testing documentation.
- [x] 5.4 Run targeted Go tests, the Web production build, browser preload checks in Windows Chrome, and strict OpenSpec validation.
