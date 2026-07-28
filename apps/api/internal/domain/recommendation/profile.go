package domainrecommendation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	MaxProfileVectorDimensions = 512
	MaxProfileAffinityEntries  = 100
	MaxProfileEventTypeLength  = 32
	MaxProfileEventIDLength    = 128
	MaxProfileComponentWeight  = 100

	DefaultProfileLongTermHalfLife = 30 * 24 * time.Hour
	DefaultProfileRecentHalfLife   = 24 * time.Hour
)

// ProfileDecay controls the independent half-lives of materialized interest
// components. It is operational configuration, not source-event identity.
type ProfileDecay struct {
	LongTermHalfLife time.Duration
	RecentHalfLife   time.Duration
}

func DefaultProfileDecay() ProfileDecay {
	return ProfileDecay{
		LongTermHalfLife: DefaultProfileLongTermHalfLife,
		RecentHalfLife:   DefaultProfileRecentHalfLife,
	}
}

func (d ProfileDecay) Normalized() ProfileDecay {
	defaults := DefaultProfileDecay()
	if d.LongTermHalfLife <= 0 {
		d.LongTermHalfLife = defaults.LongTermHalfLife
	}
	if d.RecentHalfLife <= 0 {
		d.RecentHalfLife = defaults.RecentHalfLife
	}
	return d
}

type ProfileEventInput struct {
	UserID                int64
	SourceEventID         string
	EventType             string
	OccurredAt            time.Time
	SourceVideoID         int64
	SourceAuthorID        int64
	SourceAction          string
	SourceSignal          string
	LongTermVector        []float64
	RecentVector          []float64
	AuthorAffinities      map[int64]float64
	NegativeTopicVector   []float64
	NegativeAuthorWeights map[int64]float64
	Decay                 ProfileDecay
}

type ProfileEvent struct {
	UserID                int64
	SourceEventID         string
	EventType             string
	OccurredAt            time.Time
	SourceVideoID         int64
	SourceAuthorID        int64
	SourceAction          string
	SourceSignal          string
	LongTermVector        []float64
	RecentVector          []float64
	AuthorAffinities      map[int64]float64
	NegativeTopicVector   []float64
	NegativeAuthorWeights map[int64]float64
	PayloadHash           string
	Decay                 ProfileDecay
}

type UserInterestProfile struct {
	UserID                   int64
	LongTermVector           []float64
	RecentVector             []float64
	AuthorAffinities         map[int64]float64
	NegativeTopicVector      []float64
	NegativeAuthorAffinities map[int64]float64
	Version                  int64
	UpdatedAt                time.Time
}

func NewProfileEvent(input ProfileEventInput) (*ProfileEvent, error) {
	if input.UserID <= 0 {
		return nil, ErrInvalidUserID
	}
	sourceEventID := strings.TrimSpace(input.SourceEventID)
	if sourceEventID == "" {
		return nil, ErrProfileEventIDRequired
	}
	if len(sourceEventID) > MaxProfileEventIDLength {
		return nil, ErrProfileEventIDTooLong
	}
	eventType := strings.ToLower(strings.TrimSpace(input.EventType))
	if eventType == "" || len(eventType) > MaxProfileEventTypeLength || input.OccurredAt.IsZero() {
		return nil, ErrInvalidProfileEvent
	}
	sourceAction := strings.ToLower(strings.TrimSpace(input.SourceAction))
	sourceSignal := strings.TrimSpace(input.SourceSignal)
	if input.SourceVideoID < 0 || input.SourceAuthorID < 0 ||
		len(sourceAction) > MaxProfileEventTypeLength || len(sourceSignal) > MaxProfileEventIDLength {
		return nil, ErrInvalidProfileEvent
	}
	longTerm, err := normalizeProfileVector(input.LongTermVector)
	if err != nil {
		return nil, err
	}
	recent, err := normalizeProfileVector(input.RecentVector)
	if err != nil {
		return nil, err
	}
	negativeTopics, err := normalizeProfileVector(input.NegativeTopicVector)
	if err != nil {
		return nil, err
	}
	authors, err := normalizeAffinities(input.AuthorAffinities)
	if err != nil {
		return nil, err
	}
	negativeAuthors, err := normalizeAffinities(input.NegativeAuthorWeights)
	if err != nil {
		return nil, err
	}
	event := &ProfileEvent{
		UserID: input.UserID, SourceEventID: sourceEventID, EventType: eventType, OccurredAt: input.OccurredAt.UTC(),
		SourceVideoID: input.SourceVideoID, SourceAuthorID: input.SourceAuthorID,
		SourceAction: sourceAction, SourceSignal: sourceSignal,
		LongTermVector: longTerm, RecentVector: recent, NegativeTopicVector: negativeTopics,
		AuthorAffinities: authors, NegativeAuthorWeights: negativeAuthors,
		Decay: input.Decay.Normalized(),
	}
	payload, err := json.Marshal(struct {
		UserID         int64     `json:"user_id"`
		SourceEventID  string    `json:"source_event_id"`
		EventType      string    `json:"event_type"`
		OccurredAt     time.Time `json:"occurred_at"`
		SourceVideoID  int64     `json:"source_video_id,omitempty"`
		SourceAuthorID int64     `json:"source_author_id,omitempty"`
		SourceAction   string    `json:"source_action,omitempty"`
		SourceSignal   string    `json:"source_signal,omitempty"`
	}{
		UserID: input.UserID, SourceEventID: sourceEventID, EventType: eventType, OccurredAt: input.OccurredAt.UTC(),
		SourceVideoID: input.SourceVideoID, SourceAuthorID: input.SourceAuthorID,
		SourceAction: sourceAction, SourceSignal: sourceSignal,
	})
	if err != nil {
		return nil, ErrInvalidProfileEvent
	}
	sum := sha256.Sum256(payload)
	event.PayloadHash = hex.EncodeToString(sum[:])
	return event, nil
}

func RestoreUserInterestProfile(userID int64, longTerm []float64, recent []float64, authors map[int64]float64, negativeTopics []float64, negativeAuthors map[int64]float64, version int64, updatedAt time.Time) *UserInterestProfile {
	if userID <= 0 || version < 0 || updatedAt.IsZero() {
		return nil
	}
	return &UserInterestProfile{
		UserID: userID, LongTermVector: cloneVector(longTerm), RecentVector: cloneVector(recent),
		AuthorAffinities: cloneAffinities(authors), NegativeTopicVector: cloneVector(negativeTopics),
		NegativeAuthorAffinities: cloneAffinities(negativeAuthors), Version: version, UpdatedAt: updatedAt.UTC(),
	}
}

func (p *UserInterestProfile) Apply(event *ProfileEvent) (*UserInterestProfile, error) {
	return p.ApplyWithDecay(event, DefaultProfileDecay())
}

// ApplyWithDecay stores a profile at one stable materialization timestamp.
// Existing components advance to the later of the profile and source-event
// timestamps; delayed source components advance to that same timestamp before
// they are added. This makes out-of-order delivery deterministic without
// aging an input once at ingestion and again at ranking read. Source-event
// identity remains unchanged because decay configuration is not part of its
// payload hash.
func (p *UserInterestProfile) ApplyWithDecay(event *ProfileEvent, decay ProfileDecay) (*UserInterestProfile, error) {
	if p == nil || event == nil || p.UserID != event.UserID {
		return nil, ErrInvalidProfileEvent
	}
	materializedAt := p.UpdatedAt.UTC()
	if materializedAt.IsZero() || event.OccurredAt.UTC().After(materializedAt) {
		materializedAt = event.OccurredAt.UTC()
	}
	base := p.DecayTo(materializedAt, decay)
	eventFactors := profileDecayFactorsBetween(event.OccurredAt, materializedAt, decay)
	longTerm, err := addNormalizedVector(base.LongTermVector, scaleProfileVector(event.LongTermVector, eventFactors.LongTerm))
	if err != nil {
		return nil, err
	}
	recent, err := addNormalizedVector(base.RecentVector, scaleProfileVector(event.RecentVector, eventFactors.Recent))
	if err != nil {
		return nil, err
	}
	negativeTopics, err := addNormalizedVector(base.NegativeTopicVector, scaleProfileVector(event.NegativeTopicVector, eventFactors.Recent))
	if err != nil {
		return nil, err
	}
	return &UserInterestProfile{
		UserID: p.UserID, LongTermVector: longTerm, RecentVector: recent,
		AuthorAffinities:         addAffinities(base.AuthorAffinities, scaleProfileAffinities(event.AuthorAffinities, eventFactors.Recent)),
		NegativeTopicVector:      negativeTopics,
		NegativeAuthorAffinities: addAffinities(base.NegativeAuthorAffinities, scaleProfileAffinities(event.NegativeAuthorWeights, eventFactors.Recent)),
		Version:                  base.Version + 1, UpdatedAt: materializedAt,
	}, nil
}

// DecayTo returns an in-memory profile view at the requested timestamp. It
// never moves a profile backward in time and deliberately does not mutate the
// persisted source profile during ranking reads.
func (p *UserInterestProfile) DecayTo(at time.Time, decay ProfileDecay) *UserInterestProfile {
	if p == nil {
		return nil
	}
	result := p.Clone()
	if at.IsZero() || !at.UTC().After(result.UpdatedAt.UTC()) {
		return result
	}
	factors := result.DecayFactors(at, decay)
	result.LongTermVector = scaleProfileVector(result.LongTermVector, factors.LongTerm)
	result.RecentVector = scaleProfileVector(result.RecentVector, factors.Recent)
	result.NegativeTopicVector = scaleProfileVector(result.NegativeTopicVector, factors.Recent)
	result.AuthorAffinities = scaleProfileAffinities(result.AuthorAffinities, factors.Recent)
	result.NegativeAuthorAffinities = scaleProfileAffinities(result.NegativeAuthorAffinities, factors.Recent)
	result.UpdatedAt = at.UTC()
	return result
}

type ProfileDecayFactors struct {
	LongTerm float64
	Recent   float64
}

func (p *UserInterestProfile) DecayFactors(at time.Time, decay ProfileDecay) ProfileDecayFactors {
	if p == nil || at.IsZero() || !at.UTC().After(p.UpdatedAt.UTC()) {
		return ProfileDecayFactors{LongTerm: 1, Recent: 1}
	}
	return profileDecayFactorsBetween(p.UpdatedAt, at, decay)
}

func profileDecayFactorsBetween(from, to time.Time, decay ProfileDecay) ProfileDecayFactors {
	if from.IsZero() || to.IsZero() || !to.UTC().After(from.UTC()) {
		return ProfileDecayFactors{LongTerm: 1, Recent: 1}
	}
	decay = decay.Normalized()
	age := to.UTC().Sub(from.UTC())
	return ProfileDecayFactors{
		LongTerm: profileDecayFactor(age, decay.LongTermHalfLife),
		Recent:   profileDecayFactor(age, decay.RecentHalfLife),
	}
}

func EmptyUserInterestProfile(userID int64, updatedAt time.Time) *UserInterestProfile {
	return &UserInterestProfile{
		UserID: userID, AuthorAffinities: map[int64]float64{}, NegativeAuthorAffinities: map[int64]float64{},
		UpdatedAt: updatedAt.UTC(),
	}
}

func (p *UserInterestProfile) Clone() *UserInterestProfile {
	if p == nil {
		return nil
	}
	return RestoreUserInterestProfile(p.UserID, p.LongTermVector, p.RecentVector, p.AuthorAffinities, p.NegativeTopicVector, p.NegativeAuthorAffinities, p.Version, p.UpdatedAt)
}

func normalizeProfileVector(values []float64) ([]float64, error) {
	if len(values) > MaxProfileVectorDimensions {
		return nil, ErrProfileVectorTooLarge
	}
	result := make([]float64, len(values))
	for index, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > MaxProfileComponentWeight {
			return nil, ErrInvalidProfileVector
		}
		result[index] = value
	}
	return result, nil
}

func addNormalizedVector(current []float64, delta []float64) ([]float64, error) {
	if len(current) == 0 {
		return cloneVector(delta), nil
	}
	if len(delta) == 0 {
		return cloneVector(current), nil
	}
	if len(current) != len(delta) {
		return nil, ErrInvalidProfileVector
	}
	combined := make([]float64, len(current))
	for index := range current {
		combined[index] = current[index] + delta[index]
	}
	for index := range combined {
		if combined[index] > MaxProfileComponentWeight {
			combined[index] = MaxProfileComponentWeight
		}
		if combined[index] < -MaxProfileComponentWeight {
			combined[index] = -MaxProfileComponentWeight
		}
	}
	return combined, nil
}

func normalizeAffinities(values map[int64]float64) (map[int64]float64, error) {
	if len(values) > MaxProfileAffinityEntries {
		return nil, ErrProfileMapTooLarge
	}
	normalized := make(map[int64]float64, len(values))
	for authorID, value := range values {
		if authorID <= 0 || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > MaxProfileComponentWeight {
			return nil, ErrInvalidProfileAffinity
		}
		if value > 0 {
			normalized[authorID] = value
		}
	}
	return normalized, nil
}

func addAffinities(current map[int64]float64, delta map[int64]float64) map[int64]float64 {
	combined := cloneAffinities(current)
	for authorID, value := range delta {
		combined[authorID] += value
		if combined[authorID] > MaxProfileComponentWeight {
			combined[authorID] = MaxProfileComponentWeight
		}
	}
	if len(combined) <= MaxProfileAffinityEntries {
		return combined
	}
	type item struct {
		authorID int64
		value    float64
	}
	items := make([]item, 0, len(combined))
	for authorID, value := range combined {
		items = append(items, item{authorID: authorID, value: value})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].value == items[j].value {
			return items[i].authorID < items[j].authorID
		}
		return items[i].value > items[j].value
	})
	bounded := make(map[int64]float64, MaxProfileAffinityEntries)
	for _, item := range items[:MaxProfileAffinityEntries] {
		bounded[item.authorID] = item.value
	}
	return bounded
}

func cloneVector(values []float64) []float64 {
	return append([]float64(nil), values...)
}

func cloneAffinities(values map[int64]float64) map[int64]float64 {
	cloned := make(map[int64]float64, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func profileDecayFactor(age, halfLife time.Duration) float64 {
	if age <= 0 || halfLife <= 0 {
		return 1
	}
	return math.Max(0, math.Min(1, math.Exp(-math.Ln2*age.Hours()/halfLife.Hours())))
}

func scaleProfileVector(values []float64, factor float64) []float64 {
	scaled := make([]float64, len(values))
	for index, value := range values {
		scaled[index] = value * factor
	}
	return scaled
}

func scaleProfileAffinities(values map[int64]float64, factor float64) map[int64]float64 {
	scaled := make(map[int64]float64, len(values))
	for authorID, value := range values {
		if value *= factor; value > 0 {
			scaled[authorID] = value
		}
	}
	return scaled
}
