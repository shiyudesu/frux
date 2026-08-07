## 1. Variant-Aware Owner Access

- [x] 1.1 Add optional ready-variant lookup to protected asset access without changing the public response shape.
- [x] 1.2 Select the deterministic protected baseline for ready video assets and protected cover variant for ready cover assets, with original fallback.
- [x] 1.3 Preserve owner, referenced-video, deleted-asset, private-cache, and non-owner denial rules.
- [x] 1.4 Add unit and real PostgreSQL tests for baseline/cover selection, processing fallback, ownership denial, and missing variants.

## 2. Creator Work Preview

- [x] 2.1 Add a typed Web client for owner protected asset access.
- [x] 2.2 Extend WorkViewer with in-memory media/cover access loading, stale-response fencing, expiry refresh, retry, and playback-error states.
- [x] 2.3 Pass consumer authentication only from the owner ProfilePage while preserving public-profile WorkViewer behavior.
- [x] 2.4 Ensure pending, processing, rejected, private, and offline owned cards remain selectable and display truthful preview status.
- [x] 2.5 Add WorkViewer/Profile tests for protected playback, cover-only fallback, unsupported source, access errors, expiry, close, and non-owner/public behavior.

## 3. Reviewer Cover Inspection

- [x] 3.1 Add a separately labeled protected cover section to ReviewDetailPage.
- [x] 3.2 Keep cover and video access synchronized across refresh, expiry, permission denial, and case-version conflicts.
- [x] 3.3 Add Admin UI tests for available, unavailable, refreshed, and revoked cover access.

## 4. Local Upload Preview

- [x] 4.1 Create and revoke independent local object URLs for selected video and cover files.
- [x] 4.2 Render an in-page video player with controls, playsInline, muted preview, and selected cover poster before upload.
- [x] 4.3 Show a local decode limitation without clearing the file selection or starting an upload.
- [x] 4.4 Add UploadPage tests for preview creation, replacement cleanup, unmount cleanup, poster binding, and no-network preview.

## 5. Verification and Documentation

- [x] 5.1 Run targeted media/access/API tests, real PostgreSQL tests, Admin/Profile/Upload Web tests, and the strict production build.
- [x] 5.2 Run an independent code review and Windows Chrome validation for all three preview surfaces.
- [x] 5.3 Update review, video/media, creator-management, UI/UX, and `docs/当前问题.md` documentation for issues 23, 26, and 28.
