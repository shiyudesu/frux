## Context

Production uses one private Rainyun bucket. The browser uploads originals directly. Worker downloads
the full source, creates a local MP4, uploads that MP4 to a temporary object, downloads the temporary
object, and uploads it again under a deterministic processed key. When a video becomes public,
`DeliveryCatalog` downloads the protected processed file and uploads another copy under
`media/v2/<generation>/...`. Restoring a video repeats that public copy.

Public media GET currently returns a short-lived 307 redirect, but both the redirect and signed S3
response explicitly use `private, no-store`. A browser therefore cannot reuse the redirect or media
response through normal HTTP caching.

The bucket is private and all public playback already passes through Frux authorization before a
signed GET is issued. Physical public copies are therefore not required for access control.

## Goals / Non-Goals

**Goals:**

- Reduce pre-playback object-storage outbound bytes from approximately source plus two output-sized
  downloads to only the source download.
- Upload every generated video body at most once.
- Stop copying media bodies during publish, take-down, restore, reject, or privacy transitions.
- Use a maximum 30-minute public revocation window selected for stronger browser-cache reuse.
- Allow ordinary browser HTTP caching within that window.
- Preserve Range, HEAD, ETag, owner preview, reviewer preview, cleanup, and legacy playback.
- Measure the remaining outbound-byte sources.

**Non-Goals:**

- Remove the one full source download required by the current Worker.
- Add CDN, R2, browser Service Worker video storage, P2P delivery, or persistent VPS media storage.
- Add cache lifetimes longer than 30 minutes.
- Make protected originals or private video responses cacheable.

## Decisions

### Upload processed output directly to its final protected key

After ffmpeg completes, Worker computes the output checksum and deterministic key:

```text
processed/{asset_id}/{profile_version}/{checksum}/source.mp4
```

Worker first performs HEAD:

- matching size and checksum: reuse the existing object;
- object absent: PUT the local file directly to the final key, then HEAD verify;
- conflicting metadata: fail explicitly.

S3 PUT exposes the completed object atomically, so a second object-store temporary key is
unnecessary. If the process exits after PUT but before PostgreSQL finalization, the existing orphan
reconciliation discovers and removes the unreferenced deterministic file after the safety delay.

Alternative: use S3 `CopyObject` from a temporary key. Rejected because it still creates an
unnecessary temporary object and provider-specific copy behavior is not needed when the local output
already exists.

### Use uploaded cover files directly

Validated cover uploads are already immutable, checksummed, private, and browser-compatible. Cover
completion creates the ready cover variant referencing the uploaded key instead of downloading and
uploading an identical `processed/*` copy.

Cleanup deduplication continues to handle the original asset and cover variant referencing the same
key.

### Separate storage keys from public exposure URLs

`media_variant.object_key` remains the one protected storage key for new variants. Add an optional
`exposure_generation` column. Publishing:

1. generates a random exposure generation;
2. marks the variant public and stores the generation in PostgreSQL;
3. returns a virtual URL such as:

```text
/media/v3/{generation}/{variant_id}/source.mp4
```

The URL contains no storage key. The public-media resolver verifies:

- variant ID and generation match;
- variant remains public;
- its video remains published, public, approved, and media-ready.

It then signs the unchanged protected storage key. Taking a video down clears public eligibility;
restoring creates a new generation. No video body is copied or moved.

Alternative: retain physical `media/v2/*` copies and use provider-side copy. Rejected because it
still duplicates storage, complicates cleanup, and is unnecessary for a private bucket with
application authorization.

### Keep legacy exposure URLs during migration

Existing `media/v2/*` variants are migrated only when their protected counterpart is verified:

- parse the protected `processed/*` key and old generation;
- store the protected key plus generation in PostgreSQL;
- expose the new v3 URL;
- retain the old public object for at least the 30-minute cache window, then schedule cleanup.

If the protected counterpart is missing, reconciliation repairs it using the legacy object before
switching database identity. New videos never create `media/v2/*` bodies.

### Cache public redirects for 25 minutes and signed responses for 30 minutes

For an eligible v3 public MP4:

- Frux 307 response: `public, max-age=1500, must-revalidate`;
- signed object response override: `public, max-age=1800, must-revalidate`;
- signed GET lifetime: 30 minutes;
- stable ETag and Range behavior remain.

Frux keeps a bounded in-memory signed-URL cache for at most 25 minutes, keyed by exposure generation
and variant ID. If the browser does not reuse its cached 307, it still receives the same signed URL
during the safe window.

After take-down, the old redirect and signed URL can remain usable for up to 30 minutes. Feed,
search, profile, detail, and new media authorization stop immediately, but a browser that already
cached the redirect or received the signed URL may continue playback until that bound. Private,
owner, reviewer, original, and moderation access remains `private, no-store`.

Alternative: cache public videos for hours or days. Rejected because the selected 30-minute window
already delays complete playback revocation and longer periods would make enforcement less credible.

### Keep playback source revisions tied to exposure generation

The Web player continues to use normal `<video>` Range requests. Source revision is derived from the
v3 exposure URL, so a restored video with a new generation invalidates any old player resource.
No Service Worker or whole-file browser download is added.

### Add outbound-byte accounting

Add low-cardinality counters for exact bytes read through application-controlled object-store
streams:

- source video processing;
- cover compatibility migration;
- legacy exposure repair;
- moderation preparation;
- review/owner proxy reads where applicable.

Public signed-GET traffic cannot be measured exactly by the application. The public handler records
requested Range/full-object byte estimates, while Rainyun billing remains authoritative. Metrics
must not contain video IDs, object keys, URLs, or user identities.

## Risks / Trade-offs

- **A final PUT can become orphaned before database commit** → deterministic keys and delayed orphan
  reconciliation make cleanup safe and idempotent.
- **Database eligibility now protects one private file instead of physical movement** → public-media
  resolution rechecks current video and variant state before every new signed URL.
- **A cached public response can remain usable for 30 minutes after take-down** → the UI removes the
  video and denies new URLs immediately, operators are told about the bounded delay, and no public
  cache setting exceeds 30 minutes.
- **Browser Range caching varies by browser** → retain correct Range/ETag semantics and measure
  origin requests; do not promise complete-file cache reuse.
- **API instances have separate signed-URL caches** → each cache is bounded and safe; cache misses
  affect only request count, not correctness.
- **Legacy public objects may lack protected counterparts** → repair before identity migration and
  retain existing delivery until repair succeeds.

## Migration Plan

1. Add exposure-generation persistence and v3 virtual resolver support while keeping v2 reads.
2. Deploy direct final-output and direct cover-variant publication for new uploads.
3. Switch new public transitions to database-only v3 exposure.
4. Enable bounded public redirect and signed-response caching.
5. Reconcile existing public v2 variants in bounded batches, repairing missing protected files
   before switching them.
6. After at least 30 minutes, schedule legacy public object cleanup.
7. Compare Rainyun outbound billing and application byte metrics before enabling broad migration.
8. Rollback keeps v2 compatibility; v3 variants retain one valid protected object and can be served
   by the previous protected-access path until forward deployment resumes.

## Open Questions

None.
