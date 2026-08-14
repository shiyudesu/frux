## MODIFIED Requirements

### Requirement: Canonical Comment Identity Presentation
Comment APIs SHALL project each active comment author and visible direct reply target from the canonical account source using user ID, current nickname, and current avatar while omitting the private account identifier. The Web SHALL use the same fallback avatar and authoritative profile navigation for the same user across video-author and comment surfaces.

#### Scenario: Comment author has a public identity
- **WHEN** an active root, reply preview, full reply, or thread target is returned
- **THEN** its author projection includes the canonical user ID, nickname, and avatar and contains no account identifier

#### Scenario: Direct reply target remains visible
- **WHEN** a reply directly targets an active comment
- **THEN** the reply target projection includes the target user's canonical user ID, nickname, and avatar and contains no account identifier

#### Scenario: Account has no avatar
- **WHEN** the same user appears as video author, commenter, reply target, or cached public profile without an explicit avatar
- **THEN** the Web renders one shared user-avatar fallback rather than role-specific identities

#### Scenario: Comment identity is activated
- **WHEN** a user activates a comment author or direct reply target
- **THEN** the Web navigates by the authoritative user ID and seeds the profile cache only with public nickname and avatar data until the public profile API refreshes it
