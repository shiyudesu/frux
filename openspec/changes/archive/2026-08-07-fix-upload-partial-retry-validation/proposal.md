## Why

The upload page can begin both media uploads before it knows that each selected file satisfies the production upload contract. If one side is rejected, the other side may already be completed, leaving users with a generic “upload information invalid” error and an opaque partial retry instead of a precise validation message and reliable reuse of completed work.

## What Changes

- Validate selected video and cover size, MIME family, and supported extension before creating either upload session.
- Report the specific invalid file and constraint instead of mapping all upload-intent failures to a generic message.
- Treat the video and cover as one stable upload attempt so a partial failure can reuse an already completed counterpart without uploading it again.
- Reset the stable attempt only when the selected file pair changes or the video is created successfully.
- Add regression coverage for missing-cover correction, invalid cover selection, and one-sided upload failure followed by retry.
- Correct issue 25 documentation so it remains open until the complete preflight and partial-retry flow is verified.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `web-frontend`: Require paired upload preflight, actionable file validation, and stable partial retry behavior on the upload page.
- `production-media-delivery`: Require upload-session validation and replay behavior to expose and preserve valid completed assets when the paired upload must be retried.

## Impact

Affected areas include the Web upload page, upload API helpers and error mapping, media upload-session validation/error contracts, upload page and media service tests, video module documentation, the current-issues tracker, and the two existing OpenSpec capabilities. No public media eligibility or storage schema changes are required.
