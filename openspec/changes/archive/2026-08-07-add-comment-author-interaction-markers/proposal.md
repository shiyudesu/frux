## Why

Comment cards currently expose only a partial nickname/avatar projection and use a different fallback avatar from the video-author surface, so the same account can appear to be two identities. The discussion UI also cannot identify comments written by the video author or comments liked by that author, both of which are important context in short-video discussions.

## What Changes

- Return the comment author's canonical public account identifier together with the existing live nickname and avatar projection.
- Return additive `is_video_author` and `liked_by_video_author` fields for root comments, reply previews, full replies, thread context, and newly created comments.
- Return the effective `liked_by_video_author` state from comment-like mutations so author likes update without reloading the discussion.
- Keep identity and marker hydration set-based and bounded; do not add per-comment account, video, or like queries.
- Use one shared user-avatar fallback across video-author, comment-author, reply-target, and cached public-profile entry points.
- Render clear “作者” and “作者赞过” markers while preserving tombstone privacy and existing viewer-specific like/delete behavior.
- Navigate from comment identities using the authoritative user ID/account projection instead of treating comment authors as a separate account system.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `threaded-comments`: Require canonical comment identity projection, video-author authorship markers, and video-author-like markers across API and Web behavior.

## Impact

Affected areas include the interaction comment domain entity and like result, PostgreSQL comment hydration queries, HTTP DTOs, comment API types and normalization, optimistic comment-like state, threaded comment rendering/styles, public-profile cache inputs, API-flow/PostgreSQL tests, Web component/hook tests, and interaction/UI documentation. The fields are additive and existing clients remain compatible.
