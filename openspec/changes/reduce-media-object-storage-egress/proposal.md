## Why

The current production media path downloads the source once for processing, downloads the generated
file again to move it from a temporary key to its final key, and downloads it again whenever a
protected result is copied to a public key. Public playback responses also explicitly disable
browser caching. These behaviors multiply paid object-storage outbound traffic without improving
video quality or durability.

## What Changes

- Upload generated MP4 files directly from the Worker's local temporary file to their deterministic
  final protected key; remove the temporary object upload/download round trip.
- Treat successful object-storage PUT as atomic and verify the final object using size and checksum.
- Register uploaded covers directly as protected cover variants instead of downloading and uploading
  an identical copy.
- Replace physical protected/public object copies with logical exposure records and stable
  generation-based application URLs that continue pointing to one private stored file.
- Publish, take down, restore, reject, and privatize videos by changing database eligibility rather
  than copying or deleting the video body.
- Preserve existing legacy public object URLs during migration while all new exposure generations
  use the no-copy path.
- Allow 30-minute browser caching for public signed object responses, with public redirects cached
  for 25 minutes and a maximum 30-minute revocation window.
- Reuse one signed object URL during its safe cache window so repeated requests from the same browser
  can hit its HTTP cache.
- Add byte-level metrics by storage operation to measure source download, processing publication,
  exposure, repair, and playback-origin traffic before and after rollout.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `production-media-delivery`: Remove object-body round trips during output publication and public
  lifecycle transitions, introduce logical public exposure URLs, and permit bounded browser caching.
- `durable-media-work-jobs`: Require direct deterministic final-output publication without using the
  object store as a temporary processing workspace.
- `adaptive-web-playback`: Reuse stable versioned media URLs and bounded HTTP cache results without
  adding an unbounded browser video store.

## Impact

Affected areas include the media object-store contract, ffmpeg output publication, cover completion,
delivery catalog promotion/protection, public media authorization and redirect handling, exposure
URL formats, S3 response cache controls, playback source revisions, lifecycle reconciliation,
cleanup behavior, metrics, migration compatibility, API-flow tests, browser playback tests, and
media/deployment/operations documentation. No VPS persistent media storage or new CDN provider is
introduced by this change.
