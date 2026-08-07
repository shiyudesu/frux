## 1. Comment Identity and Marker Contract

- [x] 1.1 Extend the comment domain entity, restoration paths, and comment-like result with canonical account and video-author marker fields.
- [x] 1.2 Extend root/reply/thread/create/like HTTP DTOs with additive `user_account`, `reply_to_user_account`, `is_video_author`, and `liked_by_video_author` fields.
- [x] 1.3 Preserve deleted-root tombstone identity hiding and existing anonymous/viewer-specific fields.

## 2. Bounded Persistence Hydration

- [x] 2.1 Join the parent video and canonical account projections in the shared threaded-comment row query.
- [x] 2.2 Derive `is_video_author` and active `liked_by_video_author` for roots, previews, replies, thread context, creation, and idempotent replay without N+1 queries.
- [x] 2.3 Return the effective author-like marker from comment-like mutations, including idempotent replay and non-author likes.
- [x] 2.4 Add PostgreSQL tests for canonical account projection, author comments, author likes, unlike transitions, anonymous reads, previews, replies, and tombstones.

## 3. Web Identity and Discussion UX

- [x] 3.1 Extend strict Comment and comment-like response types and normalization for canonical account and author markers.
- [x] 3.2 Seed public-profile navigation from comment and reply identities using user ID, account, nickname, and avatar.
- [x] 3.3 Introduce one shared public-user avatar fallback and replace role-specific fallbacks on video-author and comment surfaces.
- [x] 3.4 Render “作者” beside video-author comments and “作者赞过” on endorsed comments with accessible, non-interactive marker styling.
- [x] 3.5 Update optimistic/confirmed comment-like state so author likes and unlikes change only the target marker without reloading threads.

## 4. Verification and Documentation

- [x] 4.1 Add domain/API-flow tests for additive identity and marker fields across comment surfaces.
- [x] 4.2 Add Web component and hook tests for identity navigation, shared avatar fallback, author badges, author-like mutation, and unrelated-thread stability.
- [x] 4.3 Run targeted interaction Go tests, real PostgreSQL threaded-comment tests, Web tests, and the strict production build.
- [x] 4.4 Update interaction, account/UI documentation, and `docs/当前问题.md` to mark issue 24 resolved and explain its relationship to issue 13.
