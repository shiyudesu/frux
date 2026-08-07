## 1. Upload Contract

- [x] 1.1 Add precise production upload-intent errors for per-kind size and unsupported filename/content type.
- [x] 1.2 Map the new upload validation errors to stable API codes and safe Web messages.
- [x] 1.3 Add backend tests for valid formats, oversized covers/videos, type mismatch, and completed-session replay.

## 2. Web Upload State

- [x] 2.1 Add complete-pair preflight validation before checksum or upload-session creation.
- [x] 2.2 Replace the shared upload attempt with per-file identity and completed-result state.
- [x] 2.3 Preserve unchanged completed uploads when the paired file fails or is replaced.
- [x] 2.4 Separate final video-creation idempotency state from media upload state and reset it only when the selected media pair changes.
- [x] 2.5 Keep progress and user-facing status accurate for cached, retried, and invalid files.

## 3. Regression Coverage

- [x] 3.1 Test missing-cover submit followed by valid cover selection without stale session failure.
- [x] 3.2 Test invalid or oversized cover preflight creates no upload sessions.
- [x] 3.3 Test one-sided upload completion is reused while only the failed/replaced side retries.
- [x] 3.4 Test transient video-creation retry reuses both uploaded assets and its stable idempotency key.

## 4. Documentation and Delivery

- [x] 4.1 Update the video module and current-issues tracker to describe the complete issue 25 behavior.
- [x] 4.2 Run targeted backend and Web tests, strict Web build, full Go tests, and strict OpenSpec validation.
- [x] 4.3 Independently review the change and fix all high-confidence findings.
- [x] 4.4 Validate the missing-cover and partial-retry journey in Windows Chrome against the deployed stack.
- [x] 4.5 Sync delta specs, archive the change, deploy the final build, and create the feature commit while preserving unrelated user edits.
