## MODIFIED Requirements

### Requirement: Profile and work presentation
Own and public profile routes SHALL use a full-width banner-style header, circular avatar, inline relation/work/received-like counts, compact actions, route-appropriate primary and secondary tabs, profile filters, and a dense portrait work grid. Own-profile tabs SHALL be backed by real profile-dashboard, personal-video-library, and creator-content-management APIs. Existing profile editing, relation lists, follow actions, public navigation, and work viewing SHALL remain functional.

#### Scenario: Public profile renders
- **WHEN** a public user profile loads successfully
- **THEN** the redesigned header displays the available avatar, nickname, bio, public counts, actions, public tabs, public collections, and work grid without exposing private controls or content

#### Scenario: Own profile remains editable
- **WHEN** the authenticated user opens and saves profile editing
- **THEN** profile and avatar upload requests succeed and the full-width profile header reflects the saved nickname, avatar, bio, and gender

#### Scenario: Own profile content tabs render
- **WHEN** the authenticated user opens their profile
- **THEN** Works, Recommend, Likes, Favorites, Watch History, and Watch Later tabs render with real data while Short Drama and Appointments are absent

#### Scenario: Work viewer opens
- **WHEN** a user selects a readable work card
- **THEN** the work viewer opens with the correct media, title, counts, and close behavior

### Requirement: Existing frontend contracts remain stable
The redesigned frontend SHALL preserve existing typed routes, local-storage validation, strict TypeScript, authentication redirects, and existing API functions while using additive profile, personal-library, creator-management, and privacy APIs. Existing endpoint response shapes MUST remain compatible unless a new endpoint is used for the expanded behavior.

#### Scenario: Production build succeeds
- **WHEN** the redesigned page and new typed API modules are built with the existing build script
- **THEN** `tsc --noEmit` and `vite build` complete without type suppressions, explicit `any`, or a routing-library dependency

#### Scenario: Existing API boundaries remain compatible
- **WHEN** an existing client continues using authentication, Feed, interactions, messages, simple profile, relations, upload, and simple video-list APIs
- **THEN** those calls remain valid while new profile capabilities use additive typed endpoints and fields
