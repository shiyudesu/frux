## ADDED Requirements

### Requirement: Feed Scene Continuity Browser Coverage
The browser smoke workflow SHALL verify scene-scoped Feed restoration, request isolation, invalidation, and active-video continuity using stable Feed markers and observed network requests.

#### Scenario: Direct Feed navigation restores active video
- **WHEN** the workflow advances from the first video in one Feed scene, records `data-active-video-id`, visits another Feed route, and returns directly
- **THEN** the recorded active video ID is restored and no additional first-page request is issued for the retained scene

#### Scenario: Browser Back restores active video
- **WHEN** the workflow advances within a Feed scene, navigates to another Feed route, and uses browser Back
- **THEN** the original route and active video ID are restored without a console error or horizontal-layout regression

#### Scenario: Late response cannot overwrite active scene
- **WHEN** the workflow delays a Feed response, changes scenes, and releases the delayed response
- **THEN** the delayed response does not change the destination scene's active video, cards, or route

#### Scenario: Authentication invalidates retained scenes
- **WHEN** the workflow creates authenticated Feed snapshots and then changes or clears the authenticated identity
- **THEN** protected Recommendation and Following data from the previous identity cannot be restored

#### Scenario: Recommendation context remains scene scoped
- **WHEN** the workflow visits another Feed scene before returning to or refreshing Recommendation
- **THEN** any new recommendation request excludes current and recent video IDs sourced only from the other scene

#### Scenario: Sidebar refresh targets the active scene
- **WHEN** the workflow activates the only visible Feed refresh control
- **THEN** the active scene issues exactly one new first-page request, starts at its first card, other retained scene positions remain unchanged, and inactive Feed destinations have no refresh icon
