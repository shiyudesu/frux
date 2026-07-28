package domainrecommendation

import "strings"

const (
	MaxSessionIDLength              = 64
	MaxRecentVideoIDs               = 20
	MaxRefreshIndex                 = 1_000_000
	MaxPlaybackCapabilities         = 8
	NetworkClassOffline             = "offline"
	NetworkClassSlow2G              = "slow_2g"
	NetworkClass2G                  = "2g"
	NetworkClass3G                  = "3g"
	NetworkClass4G                  = "4g"
	NetworkClass5G                  = "5g"
	NetworkClassWiFi                = "wifi"
	NetworkClassEthernet            = "ethernet"
	NetworkClassUnknown             = "unknown"
	ViewportClassSmall              = "small"
	ViewportClassMedium             = "medium"
	ViewportClassLarge              = "large"
	ViewportClassUnknown            = "unknown"
	PlaybackCapabilityMP4           = "mp4"
	PlaybackCapabilityDASH          = "dash"
	PlaybackCapabilityMediaSource   = "media_source"
	PlaybackCapabilityMediaFeatures = "media_capabilities"
)

type RecommendationContextInput struct {
	RequestID            string
	SessionID            string
	RefreshIndex         int
	RecentVideoIDs       []int64
	CurrentVideoID       int64
	NetworkClass         string
	SaveData             bool
	ViewportClass        string
	PlaybackCapabilities []string
}

type RecommendationContext struct {
	RequestID            string
	SessionID            string
	RefreshIndex         int
	RecentVideoIDs       []int64
	CurrentVideoID       int64
	NetworkClass         string
	SaveData             bool
	ViewportClass        string
	PlaybackCapabilities []string
}

func NewRecommendationContext(input RecommendationContextInput) (*RecommendationContext, error) {
	requestID := strings.TrimSpace(input.RequestID)
	sessionID := strings.TrimSpace(input.SessionID)
	if len(requestID) > MaxRequestIDLength {
		return nil, ErrRequestIDTooLong
	}
	if len(sessionID) > MaxSessionIDLength {
		return nil, ErrSessionIDTooLong
	}
	if input.RefreshIndex < 0 || input.RefreshIndex > MaxRefreshIndex {
		return nil, ErrInvalidRefreshIndex
	}
	if input.CurrentVideoID < 0 {
		return nil, ErrInvalidVideoID
	}
	if len(input.RecentVideoIDs) > MaxRecentVideoIDs {
		return nil, ErrTooManyRecentVideoIDs
	}

	recentVideoIDs := make([]int64, 0, len(input.RecentVideoIDs))
	seenVideoIDs := make(map[int64]struct{}, len(input.RecentVideoIDs))
	for _, videoID := range input.RecentVideoIDs {
		if videoID <= 0 {
			return nil, ErrInvalidVideoID
		}
		if _, exists := seenVideoIDs[videoID]; exists {
			continue
		}
		seenVideoIDs[videoID] = struct{}{}
		recentVideoIDs = append(recentVideoIDs, videoID)
	}

	networkClass, ok := normalizeNetworkClass(input.NetworkClass)
	if !ok {
		return nil, ErrInvalidNetworkClass
	}
	viewportClass, ok := normalizeViewportClass(input.ViewportClass)
	if !ok {
		return nil, ErrInvalidViewportClass
	}
	playbackCapabilities, err := normalizePlaybackCapabilities(input.PlaybackCapabilities)
	if err != nil {
		return nil, err
	}

	return &RecommendationContext{
		RequestID:            requestID,
		SessionID:            sessionID,
		RefreshIndex:         input.RefreshIndex,
		RecentVideoIDs:       recentVideoIDs,
		CurrentVideoID:       input.CurrentVideoID,
		NetworkClass:         networkClass,
		SaveData:             input.SaveData,
		ViewportClass:        viewportClass,
		PlaybackCapabilities: playbackCapabilities,
	}, nil
}

func (c *RecommendationContext) Clone() *RecommendationContext {
	if c == nil {
		return nil
	}
	cloned := *c
	cloned.RecentVideoIDs = append([]int64(nil), c.RecentVideoIDs...)
	cloned.PlaybackCapabilities = append([]string(nil), c.PlaybackCapabilities...)
	return &cloned
}

func normalizeNetworkClass(value string) (string, bool) {
	value = normalizeContextToken(value)
	if value == "" {
		return NetworkClassUnknown, true
	}
	switch value {
	case NetworkClassOffline, NetworkClassSlow2G, NetworkClass2G, NetworkClass3G,
		NetworkClass4G, NetworkClass5G, NetworkClassWiFi, NetworkClassEthernet,
		NetworkClassUnknown:
		return value, true
	default:
		return "", false
	}
}

func normalizeViewportClass(value string) (string, bool) {
	value = normalizeContextToken(value)
	if value == "" {
		return ViewportClassUnknown, true
	}
	switch value {
	case ViewportClassSmall, ViewportClassMedium, ViewportClassLarge, ViewportClassUnknown:
		return value, true
	default:
		return "", false
	}
}

func normalizePlaybackCapabilities(values []string) ([]string, error) {
	if len(values) > MaxPlaybackCapabilities {
		return nil, ErrTooManyPlaybackCapabilities
	}
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = normalizeContextToken(value)
		switch value {
		case PlaybackCapabilityMP4, PlaybackCapabilityDASH, PlaybackCapabilityMediaSource, PlaybackCapabilityMediaFeatures:
		default:
			return nil, ErrInvalidPlaybackCapability
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized, nil
}

func normalizeContextToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.ReplaceAll(value, "-", "_")
}
