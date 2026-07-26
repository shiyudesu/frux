## Why

The current Web player is a direct `<video src>` wrapper with one active element and minimal controls. It does not consume capability-aware sources, use `buffer_ms`, retain adjacent players, or expose quality and playback-policy controls needed for reliable short-video switching.

## What Changes

- Add a typed player abstraction that supports baseline MP4 playback and adaptive-manifest playback with graceful fallback.
- Maintain a bounded current/previous/next player pool so Feed transitions reuse prepared media rather than rebuilding every stage.
- Select sources from browser codec support, network information, device capability, user quality preference, and server playback policy.
- Make `buffer_ms` control readiness and transition decisions, including conservative behavior on slow or data-saving networks.
- Add truthful buffering, retry, quality, speed, continuous-play, and media-error states while preserving current keyboard and accessible controls.
- Keep image fallback, reduced-motion behavior, fullscreen, seek, mute, and existing Feed interactions compatible.

## Capabilities

### New Capabilities

- `adaptive-web-playback`: Defines player pooling, capability-aware source selection, buffering policy, adaptive fallback, and extended playback controls.

### Modified Capabilities

- `douyin-style-web-experience`: Extends the immersive Feed stage requirements with real buffering/error states, quality selection, speed controls, and prepared adjacent-player transitions.

## Impact

- Affects Web dependencies, `VideoStage`, player controls, Feed swipe rendering, playback API types/configuration, media-source DTOs, accessibility tests, and browser smoke tests.
- Consumes production playback variants when available but must remain functional against legacy single-MP4 responses.
- Requires updates to playback, Feed, UI/UX, engineering, and browser-validation documentation.
