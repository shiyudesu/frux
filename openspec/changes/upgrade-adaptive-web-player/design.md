## Context

`VideoStage` owns one native video element per rendered stage, assigns a single `src`, loops playback, and tracks play, mute, seek, fullscreen, and coarse QoS state. Adjacent stages use `preload=none`; the separate preloader does not transfer player state. Playback configuration contains `buffer_ms`, but the player does not consume it.

Production media delivery will add optional source variants and a DASH manifest, but the player must continue working with legacy MP4-only items.

## Goals / Non-Goals

**Goals:**

- Provide a typed player state machine and adapter boundary.
- Reuse a bounded previous/current/next player pool during Feed transitions.
- Select playable sources from capabilities, network, policy, and user preference.
- Use `buffer_ms` as a real readiness target.
- Add truthful quality, speed, buffering, retry, and continuous-play controls.
- Preserve accessibility, current interactions, and legacy MP4 fallback.

**Non-Goals:**

- Copying Douyin's proprietary XGPlayer code or controls.
- Implementing DRM, picture-in-picture, casting, or offline download.
- Replacing the Feed recommendation or pagination algorithms.
- Making unsupported codecs playable through software decoding.

## Decisions

### 1. Introduce adapters instead of replacing the GCFeed UI

Define a `PlayerAdapter` interface for load, play, pause, seek, mute, rate, quality, state subscription, buffered range, and destroy. Implement:

- `NativeMP4Adapter` for current `media_url` and MP4 variants.
- `DashAdapter` using `dash.js` for DASH manifests when MediaSource and required codecs are supported.

The existing GCFeed controls consume adapter state and remain locally owned. If DASH initialization or playback fails, the controller falls back to the best compatible MP4 source.

Alternative: adopt a full third-party player UI. This was rejected because it would conflict with the existing typed components, accessibility, and visual system.

### 2. Model playback as an explicit state machine

States are `idle`, `loading`, `ready`, `playing`, `paused`, `buffering`, `ended`, and `error`. Events from the adapter, source selector, Feed activation, and browser lifecycle drive transitions. Rendering and telemetry read this state instead of inferring behavior from scattered React booleans.

Errors retain structured category and recoverability: source unavailable, unsupported codec, manifest, network, decode, autoplay, and unknown.

### 3. Keep a three-slot player pool

The Feed owns slots for previous, current, and next. A slot is keyed by scene/request generation/video/source revision and contains the video element, adapter, readiness, and playback state. Swipe changes roles instead of destroying all players. Items outside the pool are released.

The pool integrates with `feed-aware-preloading`; if that change is not yet present, it can prepare the next slot itself using the same policy interface.

### 4. Select sources through capabilities and policy

Selection inputs include:

- `HTMLMediaElement.canPlayType` and `MediaCapabilities.decodingInfo` when available,
- MediaSource/DASH support,
- network effective type, downlink, RTT, and save-data,
- viewport/device pixel ratio,
- server playback policy,
- validated user quality preference.

`auto` chooses a conservative initial rendition and allows the DASH adapter to adapt within server bounds. Manual quality locks a rendition until the source becomes unavailable. Legacy responses synthesize one MP4 source.

### 5. Apply `buffer_ms` to activation and transition

The next slot is `ready` when its playable buffered duration reaches `buffer_ms`; `canplay` is the fallback when ranges are unavailable. A committed swipe activates immediately if ready, otherwise shows a buffering state without reverting the navigation. Slow/save-data networks may use a lower policy target rather than downloading large buffers.

### 6. Extend controls without inventing unsupported behavior

Controls add:

- auto/manual quality based on available sources,
- playback rates from a bounded list,
- buffering and recoverable retry state,
- continuous-play preference,
- existing play, time, seek, mute, fullscreen, keyboard shortcuts, and reduced motion.

Current looping remains the default compatibility behavior. When continuous play is enabled, completion advances to the next Feed item instead of looping, provided a next item exists.

### 7. Keep player state outside broad page re-renders

High-frequency time/buffer updates use adapter subscriptions and localized state rather than rebuilding the full Feed page. Only user-visible normalized state enters React. All adapter listeners, object URLs, timers, and media sources are disposed on pool release.

## Risks / Trade-offs

- [DASH dependency increases bundle size] -> Lazy-load the adapter only for DASH sources and keep native MP4 in the initial path.
- [Three players increase memory] -> Enforce slot count and cooperate with network/memory policy.
- [Browser codec reporting can be inaccurate] -> Attempt playback with structured fallback and remember session failures.
- [Manual quality switches can interrupt playback] -> Preserve current position and play state, and expose temporary buffering state.
- [Player pooling complicates DOM ownership] -> Centralize ownership in one Feed player-pool component and test lifecycle transitions.

## Migration Plan

1. Add player types/state machine and wrap current native behavior without UI changes.
2. Move current controls and QoS hooks onto adapter state.
3. Introduce the three-slot pool for MP4-only playback.
4. Consume additive playback sources and lazy-load DASH support.
5. Enable quality, speed, and continuous-play controls.
6. Roll out by feature flag and compare startup, errors, memory, and rebuffering.
7. Roll back to the native adapter while retaining compatible source metadata.

## Open Questions

- Final `dash.js` version and bundle-loading boundary.
- Whether continuous play should become the default after usage data is available.
