package applicationmedia

import (
	"fmt"
	"strings"
	"time"
)

type ProcessingRequestedEvent struct {
	EventID        string    `json:"event_id"`
	AssetID        int64     `json:"asset_id"`
	ProfileVersion string    `json:"profile_version"`
	OccurredAt     time.Time `json:"occurred_at"`
}

func NewProcessingRequestedEvent(assetID int64, profileVersion string, occurredAt time.Time) *ProcessingRequestedEvent {
	profileVersion = strings.TrimSpace(profileVersion)
	if assetID <= 0 || profileVersion == "" {
		return nil
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	return &ProcessingRequestedEvent{
		EventID: fmt.Sprintf("media-processing:%d:%s", assetID, profileVersion),
		AssetID: assetID, ProfileVersion: profileVersion, OccurredAt: occurredAt.UTC(),
	}
}
