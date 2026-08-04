## MODIFIED Requirements

### Requirement: Truthful Profile Tabs
The authenticated profile SHALL expose Works, Likes, Favorites, Watch History, and Watch Later tabs backed by real data. The Works tab SHALL expose Published, Private Works, and Collections secondary views. Recommend, Short Drama, and Appointments tabs MUST NOT be rendered on the personal profile.

#### Scenario: User changes primary tab
- **WHEN** the user activates a supported primary tab
- **THEN** the page loads that tab's real data and presents loading, error, empty, and ready states

#### Scenario: Recommendation remains outside the personal profile
- **WHEN** the authenticated profile navigation is rendered
- **THEN** no Recommend tab is present and recommendation discovery remains available through the recommendation Feed route

#### Scenario: Unsupported Douyin domains are absent
- **WHEN** the profile navigation is rendered
- **THEN** no Short Drama or Appointments tab or cosmetic-only control is present
