## ADDED Requirements

### Requirement: Canonical Comment Identity Presentation
Comment APIs SHALL project each active comment author and visible direct reply target from the canonical account identity using user ID, public account identifier, current nickname, and current avatar. The Web SHALL use the same fallback avatar and authoritative profile navigation for the same user across video-author and comment surfaces.

#### Scenario: Comment author has a public identity
- **WHEN** an active root, reply preview, full reply, or thread target is returned
- **THEN** its author projection includes the canonical user ID, account, nickname, and avatar from the account source of truth

#### Scenario: Direct reply target remains visible
- **WHEN** a reply directly targets an active comment
- **THEN** the reply target projection includes the target user's canonical account, nickname, and avatar

#### Scenario: Account has no avatar
- **WHEN** the same account appears as video author, commenter, reply target, or cached public profile without an explicit avatar
- **THEN** the Web renders one shared user-avatar fallback rather than role-specific identities

#### Scenario: Comment identity is activated
- **WHEN** a user activates a comment author or direct reply target
- **THEN** the Web navigates by the authoritative user ID and seeds the profile cache with the canonical account projection until the public profile API refreshes it

#### Scenario: Deleted root becomes a tombstone
- **WHEN** a self-deleted root remains visible only to preserve active replies
- **THEN** its public projection hides user identity, content, and author markers

### Requirement: Video Author Discussion Markers
Every active comment projection SHALL state whether the comment was written by the parent video's author and whether it is currently liked by that author. These markers SHALL be derived from the immutable video author and active comment-like facts without per-comment query growth.

#### Scenario: Video author writes a comment
- **WHEN** a root comment or reply has `user_id` equal to the parent video's author
- **THEN** every API surface returns `is_video_author=true` and the Web displays an “作者” marker beside that identity

#### Scenario: Another user writes a comment
- **WHEN** a comment belongs to a user other than the video author
- **THEN** `is_video_author=false` and no author marker is displayed

#### Scenario: Video author likes a comment
- **WHEN** an active like exists from the video's author to an active root or reply
- **THEN** every comment projection returns `liked_by_video_author=true` and the Web displays “作者赞过”

#### Scenario: Video author changes the like state
- **WHEN** the video's author likes or unlikes a visible comment
- **THEN** the mutation response returns the effective `liked_by_video_author` state and only that visible comment updates without a thread reload

#### Scenario: Ordinary viewer likes a comment
- **WHEN** a non-author viewer changes their own comment-like state
- **THEN** viewer-specific `liked` changes while `liked_by_video_author` continues to reflect only the video author's active like

#### Scenario: Root page hydrates previews
- **WHEN** roots and bounded reply previews are listed
- **THEN** canonical identities and both author markers are hydrated with bounded set-based queries rather than one account, video, or like query per comment
