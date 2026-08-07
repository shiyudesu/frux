## Why

Frux already protects reviewer and owner media, but the Web does not expose that capability consistently: reviewers cannot inspect the cover separately, creators cannot play pending/processing/non-public works whose public URL is intentionally blank, and the upload page shows only the selected cover rather than the selected video. These gaps make protected lifecycle states look broken even though the underlying files exist.

## What Changes

- Add an explicit reviewer cover-inspection surface alongside the protected video preview.
- Let authenticated creators open their own non-deleted pending, processing, rejected, private, or offline works through short-lived protected media/cover access without making them public.
- Prefer a protected ready baseline/cover variant for owner asset access when available, with the protected original as the processing/legacy fallback.
- Add loading, expiry, unavailable, unsupported-source, and retry states to the creator work viewer.
- Add local object-URL video preview on the upload page before any network upload, using the selected cover as poster and revoking URLs when files change or the page unmounts.
- Preserve anonymous/public visibility rules and prevent protected preview URLs from entering persistent Web storage.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `human-review-workflow`: Require separate protected cover inspection for the current review subject.
- `creator-content-management`: Allow creators to preview their own non-public lifecycle states safely from the works surface.
- `production-media-delivery`: Prefer protected playable variants for owner access while preserving source protection and expiry.
- `web-frontend`: Provide local pre-upload video preview with safe object-URL lifecycle and truthful preview states.

## Impact

Affected areas include media protected-access selection, upload/access API types, reviewer detail UI, creator work viewer and profile integration, upload-page object URL management, modal/loading/error styles, API/real-storage tests, Web component/page tests, and review/video/media/UI documentation. No public media eligibility rule changes.
