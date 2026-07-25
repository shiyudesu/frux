## Context

`useFeed` has the exact ordered items for timeline, hot, following, and recommendation scenes. It nevertheless calls `/api/preload-videos`, whose repository returns globally newer-to-older public videos by `published_at`. The returned items can diverge from the next Feed candidates. The client creates temporary hidden videos with `preload=metadata` and removes them as soon as metadata or `canplay` fires, so no prepared player state is transferred to the visible stage.

This change must improve switching without requiring the production media pipeline or adaptive player to land first.

## Goals / Non-Goals

**Goals:**

- Preload only candidates that can actually become adjacent items in the active Feed.
- Retain bounded prepared media state through a Feed transition.
- Use network and playback configuration to limit work.
- Cancel stale work and avoid cross-scene or cross-request contamination.
- Preserve a simple MP4 fallback and current Feed APIs.

**Non-Goals:**

- Transcoding or generating media variants.
- Implementing adaptive manifests or quality switching.
- Persisting preload state across a full page reload.
- Preloading an unbounded Feed window.

## Decisions

### 1. Treat the active Feed item array as the ordering source of truth

The preloader receives `{scene, requestID, items, activeIndex}` from `useFeed`. It derives the forward window from `items[activeIndex+1...]` and optionally retains the previous item for back navigation. It never queries a separate global ordering for candidates already present in the Feed.

Near the end of the loaded page, the existing Feed pagination path fetches the next page first; those appended items then enter the same preload window. The compatibility preload endpoint remains available for older clients but is removed from the primary Web flow.

### 2. Introduce a bounded preload controller

A `FeedPreloadController` owns at most:

- one previous candidate resource,
- the active resource,
- `preload_count` forward resources, capped by policy and an absolute maximum.

Each resource is keyed by scene, request generation, and video ID. Entries hold a retained video element or fetch/controller state, readiness, buffered duration, last use, and cleanup callback. `VideoStage` can adopt or reuse the prepared element/state when the candidate becomes active.

Alternative: browser-global URL caching through disposable probes. This was rejected because current probes discard decoded/player state and offer no cancellation or readiness visibility.

### 3. Make policy depend on effective network conditions

The client normalizes `navigator.connection` and applies:

- offline/save-data: no media-byte preload; cover only.
- 3G/slow-3G: metadata for the next item only.
- 4G/default: prepare the next item and metadata for the remaining configured window.
- WiFi/5G: prepare up to `preload_count`, subject to memory limits.

`buffer_ms` defines the target playable buffered duration for the immediate next candidate. With plain MP4, readiness is derived from `buffered`; if the browser does not expose useful ranges, `canplay` is the fallback.

### 4. Cancel by generation and release by LRU

Changing Feed scene, request ID, authentication generation, or replacing the list aborts all obsolete work. Index changes reprioritize rather than duplicate matching entries. A strict LRU and total prepared-resource cap release distant candidates, revoke object URLs, detach events, clear sources, and call `load()` to free browser resources.

### 5. Preserve failure isolation

Preload errors never fail the Feed. A failed candidate is marked with a short retry cooldown and plays through the normal visible-stage path if selected. Unauthorized API errors continue to follow existing session handling; public media failures remain local to the candidate.

## Risks / Trade-offs

- [Retained videos consume memory] -> Bound slots, buffered target, and LRU cleanup; reduce policy on low-memory or slow-network devices.
- [Browser buffering behavior varies] -> Use progressive enhancement with metadata/canplay fallback.
- [Feed pagination may arrive too late] -> Trigger existing `loadMoreFeed` earlier based on active index plus preload window.
- [Prepared element transfer complicates React ownership] -> Encapsulate DOM ownership in one controller and expose typed acquire/release handles.
- [Signed URLs expire while retained] -> Treat playback source identity and expiry as part of the resource key and reacquire when stale.

## Migration Plan

1. Add the preload controller and tests while keeping the current endpoint call disabled behind a Web flag.
2. Feed it current ordered items and cover-only prewarming first.
3. Enable retained next-item MP4 preparation and measure memory, requests, and transition startup.
4. Remove the primary Web call to `/api/preload-videos`; retain endpoint compatibility.
5. Roll back by disabling the controller and restoring metadata-only visible playback.

## Open Questions

- Whether the previous item should keep buffered bytes or only metadata on memory-constrained devices.
- The initial absolute memory/resource cap before real-device measurements are available.
