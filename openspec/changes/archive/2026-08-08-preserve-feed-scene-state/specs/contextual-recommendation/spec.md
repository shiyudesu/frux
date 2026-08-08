## ADDED Requirements

### Requirement: Recommendation Scene Context Isolation
The Web client SHALL keep Recommendation request identity, session identity, signed pagination state, recent-video context, and accepted-feedback suppression scoped to the Recommendation scene. Temporarily activating another Feed scene MUST NOT reset or contaminate a valid retained Recommendation session.

#### Scenario: User leaves and returns to Recommendation
- **WHEN** the user leaves a committed Recommendation scene for another Feed route and returns while its snapshot remains valid
- **THEN** the client restores the same request, session, cursor, active video, and suppression state without starting another recommendation query

#### Scenario: Recommendation is intentionally refreshed
- **WHEN** the client creates a new Recommendation request after an explicit refresh
- **THEN** `recent_video_ids` and `current_video_id` are derived only from retained Recommendation cards and the Recommendation refresh index advances within its logical session

#### Scenario: Another Feed scene was active
- **WHEN** Timeline, Following, or Hot is the scene being left before Recommendation loads or refreshes
- **THEN** cards and indices from that scene do not appear in Recommendation recent-video or current-video context

#### Scenario: Recommendation feedback was accepted before leaving
- **WHEN** a video or author was suppressed by accepted recommendation feedback and the user temporarily visits another Feed scene
- **THEN** returning to Recommendation keeps that suppression for the retained recommendation session
