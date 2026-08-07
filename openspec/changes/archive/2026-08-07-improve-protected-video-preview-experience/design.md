## Context

Frux intentionally removes public media URLs from pending, processing, rejected, private, and offline videos. The backend already exposes owner-only `GET /api/media-assets/{assetId}/access`, and review preview access already returns separate protected media and cover URLs, but the Web treats a blank public URL as if the asset did not exist.

The upload page retains selected `File` objects but only creates an object URL for the cover. The reviewer detail uses the cover only as the `<video poster>`, which does not support independent inspection.

## Goals / Non-Goals

**Goals:**

- Let creators preview every owned, non-deleted work without changing public eligibility.
- Use an existing ready protected baseline/cover when possible, falling back to the protected original while processing.
- Add truthful loading, unavailable, expired, and unsupported-source states.
- Let reviewers inspect the current protected cover separately from video playback.
- Let users preview a selected local video before uploading it.
- Keep signed URLs in component memory only and revoke local object URLs.

**Non-Goals:**

- Making pending, rejected, private, or offline media anonymously readable.
- Adding a public video detail route for non-public lifecycle states.
- Editing or replacing the uploaded video/cover from the viewer.
- Generating a server-side preview before upload.
- Adding thumbnails or frame extraction beyond the existing media pipeline.

## Decisions

### Reuse owner asset-access authorization

The owner WorkViewer receives the authenticated consumer token and uses the existing asset-access endpoint for missing media and cover URLs. Public-profile WorkViewer instances do not receive a token and retain public-only behavior.

The access endpoint continues to verify immutable asset ownership and current video authorization. URLs remain short-lived and are never stored in localStorage, sessionStorage, route state, or persisted video objects.

Alternative considered: expose raw protected URLs in every creator query response. Rejected because list APIs would mint many unused credentials and increase leak surface.

### Prefer ready protected variants

Enhance `GetProtectedAssetAccess` through an optional ready-variant reader. For a ready video asset, select the deterministic baseline MP4; for a ready cover asset, select the deterministic cover variant. If no suitable ready variant exists, sign the protected original source.

This keeps pending-but-processed videos browser-compatible while still allowing a creator to inspect an uploaded source during processing. Variant selection does not promote the object or change public state.

### Resolve preview state inside WorkViewer

WorkViewer owns an in-memory generation token and separately resolves media and cover access. It ignores stale responses after close/video change, refreshes before the earliest signed URL expires, and offers retry after access or playback failure.

If only a cover is available, the viewer shows the cover with truthful processing/unavailable copy. Browser decode errors do not imply server processing failure.

### Add local upload object URLs

UploadPage creates object URLs independently for selected video and cover files. The video preview uses controls, muted/playsInline playback, and the selected cover as poster. Each URL is revoked when the corresponding file changes or the component unmounts.

No checksum, upload session, or API call is made for preview.

### Keep reviewer cover access separate

The review detail keeps the protected video player and adds a clearly labeled cover inspection section using the same expiring preview response. Cover access refreshes with the existing preview timer and disappears on permission/version conflicts.

## Risks / Trade-offs

- [Original source codec is not browser-playable during processing] → Prefer a ready baseline; otherwise show a clear unsupported/processing state without changing review status.
- [Signed URLs expire while a modal remains open] → Refresh shortly before expiry and expose manual retry.
- [Two protected asset requests add latency] → Resolve media and cover concurrently only when their public/owner URL is missing.
- [Object URLs retain large files in memory] → Revoke on replacement and unmount.
- [Reviewer cover may be absent] → Render an explicit unavailable state rather than reusing a video frame.

## Migration Plan

1. Deploy additive variant-aware protected access; endpoint shape remains compatible.
2. Deploy WorkViewer protected access and upload/reviewer preview UI.
3. Existing public videos continue using their current public URLs.
4. Rollback removes the new Web behavior; protected access remains safe and compatible.

## Open Questions

None. A creator preview is an owner-authorized temporary view, not publication.
