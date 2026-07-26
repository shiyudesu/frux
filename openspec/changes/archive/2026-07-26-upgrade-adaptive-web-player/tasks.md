## 1. Player Foundation

- [x] 1.1 Add the selected pinned DASH dependency with pnpm and preserve the sole lockfile policy.
- [x] 1.2 Define strict playback source, normalized player state, error, quality, and adapter interfaces.
- [x] 1.3 Implement and unit-test the player state machine and transition guards.
- [x] 1.4 Wrap current native MP4 behavior in `NativeMP4Adapter` without changing visible behavior.

## 2. Adaptive Adapter

- [x] 2.1 Implement lazy loading and lifecycle management for `DashAdapter`.
- [x] 2.2 Map DASH events, qualities, buffered ranges, playback rate, seeking, and errors into normalized player state.
- [x] 2.3 Implement structured DASH-to-MP4 fallback preserving position, mute, rate, and intended play state.
- [x] 2.4 Add adapter tests for initialization, quality changes, retry, fallback, and destroy cleanup.

## 3. Player Pool and Feed Integration

- [x] 3.1 Implement a bounded previous/current/next player pool keyed by Feed generation, video ID, and source revision.
- [x] 3.2 Refactor Feed stage rendering so swipe transitions reassign prepared slots instead of rebuilding unrelated players.
- [x] 3.3 Integrate with `feed-aware-preloading` handles when present and retain an internal MP4 preparation fallback.
- [x] 3.4 Ensure scene changes, authentication changes, item replacement, and unmount destroy obsolete adapters and sources.
- [x] 3.5 Add tests for forward/back slot rotation, stale generations, and maximum slot count.

## 4. Capability and Buffer Policy

- [x] 4.1 Implement codec, MediaSource, MediaCapabilities, network, save-data, viewport, and user-preference detection.
- [x] 4.2 Normalize legacy `media_url` into one compatible playback source.
- [x] 4.3 Select automatic initial quality and adaptive bounds from server policy and client capability.
- [x] 4.4 Apply `buffer_ms` to next-slot readiness and active buffering transitions.
- [x] 4.5 Persist and validate manual quality, speed, and continuous-play preferences.

## 5. Controls and Accessibility

- [x] 5.1 Refactor `FeedPlayerControls` to consume normalized adapter state.
- [x] 5.2 Add accessible auto/manual quality selection and effective-quality display.
- [x] 5.3 Add bounded playback-rate controls and effective-rate display.
- [x] 5.4 Add truthful loading, buffering, recoverable retry, fallback, and terminal error UI.
- [x] 5.5 Add continuous-play behavior while preserving current loop default and keyboard shortcuts.
- [x] 5.6 Verify focus order, labels, reduced motion, mobile layout, and non-video fallback.

## 6. Integration and Verification

- [x] 6.1 Connect behavior-event and playback-telemetry hooks to normalized player transitions without duplicate reporting.
- [x] 6.2 Add component/browser tests for MP4, DASH, fallback, quality, speed, buffering, retry, swipe, fullscreen, and image items.
- [x] 6.3 Measure initial/adaptive chunk size and ensure DASH code is not in the baseline MP4 startup path.
- [x] 6.4 Update playback, Feed, UI/UX, engineering, optimization, and browser-testing documentation.
- [x] 6.5 Run the Web build, targeted Go contract tests, Windows Chrome desktop/mobile checks, and strict OpenSpec validation.
