## Why

GCFeed preloading currently queries videos by global publish order, so recommendation, hot, and following scenes can preload media that is not actually next in the active Feed. The browser also discards metadata probes immediately, limiting the value of the preload configuration.

## What Changes

- Make preload decisions follow the exact ordered candidates already returned by the active Feed request.
- Separate in-page candidate prebuffering from cross-page Feed refill so preloading does not invent a second ordering model.
- Retain bounded warm player resources for the current, previous, and next candidates instead of creating disposable metadata probes.
- Apply `preload_count` and `buffer_ms` as real client policy inputs with network-aware limits.
- Cancel or release obsolete preload work when the scene, request generation, cursor, or active index changes.
- Preserve MP4 metadata-only fallback when a browser or network cannot safely prebuffer media bytes.

## Capabilities

### New Capabilities

- `feed-aware-preloading`: Defines ordered candidate preloading, bounded player-resource retention, cancellation, refill, and fallback behavior.

### Modified Capabilities

## Impact

- Affects Feed response/use-hook state, playback configuration, `VideoStage`, preload utilities, playback service/repository behavior, and scene pagination tests.
- The existing `/api/preload-videos` endpoint will be narrowed to ordered Feed refill or compatibility use rather than global timeline ordering.
- Requires updates to feed, playback, UI/UX, performance, and optimization documentation.
