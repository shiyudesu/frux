## MODIFIED Requirements

### Requirement: Profile and work presentation
Own and public profile routes SHALL use a full-width banner-style header, circular avatar, inline relation/work/received-like counts, compact actions, route-appropriate primary and secondary tabs, profile filters, and a dense portrait work grid. Own-profile tabs SHALL be backed by real profile-dashboard, personal-video-library, and creator-content-management APIs and SHALL NOT include a personal Recommend tab. Selecting a readable personal-library card SHALL open the immersive collection queue, while creator work and collection behavior SHALL remain truthful for their visibility and ownership context. Existing profile editing, relation lists, follow actions, public navigation, and work viewing SHALL remain functional.

#### Scenario: Public profile renders
- **WHEN** a public user profile loads successfully
- **THEN** the redesigned header displays the available avatar, nickname, bio, public counts, actions, public tabs, public collections, and work grid without exposing private controls or content

#### Scenario: Own profile remains editable
- **WHEN** the authenticated user opens and saves profile editing
- **THEN** profile and avatar upload requests succeed and the full-width profile header reflects the saved nickname, avatar, bio, and gender

#### Scenario: Own profile content tabs render
- **WHEN** the authenticated user opens their profile
- **THEN** Works, Likes, Favorites, Watch History, and Watch Later tabs render with real data while Recommend, Short Drama, and Appointments are absent

#### Scenario: Personal library queue opens
- **WHEN** a user selects a readable card from Likes, Favorites, Watch History, or Watch Later
- **THEN** an immersive full-screen player opens at the selected item and can continue through that ordered collection

#### Scenario: Creator work viewer remains truthful
- **WHEN** a user selects a creator work whose context is not a personal-library queue
- **THEN** the work opens through the supported viewer behavior with the correct media, title, visibility permissions, counts, and close behavior
