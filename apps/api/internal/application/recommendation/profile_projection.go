package applicationrecommendation

import (
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	applicationinteraction "github.com/shiyudesu/frux/internal/application/interaction"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	"context"
	"math"
	"strconv"
	"strings"
	"time"
)

type ProfileFeature struct {
	Vector   []float64
	AuthorID int64
}

type ProfileProjectionRepository interface {
	domainrecommendation.ProfileRepository
	LoadProfileFeature(ctx context.Context, videoID int64) (ProfileFeature, bool, error)
}

type ProfileWeighting struct {
	LongTermHalfLife time.Duration
	RecentHalfLife   time.Duration
	ProgressWeight   float64
	CompletionWeight float64
	SkipWeight       float64
	LikeWeight       float64
	FavoriteWeight   float64
	FollowWeight     float64
	FeedbackWeight   float64
	MaxSignalWeight  float64
}

func DefaultProfileWeighting() ProfileWeighting {
	return ProfileWeighting{
		LongTermHalfLife: domainrecommendation.DefaultProfileLongTermHalfLife,
		RecentHalfLife:   domainrecommendation.DefaultProfileRecentHalfLife,
		ProgressWeight:   0.5, CompletionWeight: 1, SkipWeight: 0.8, LikeWeight: 1, FavoriteWeight: 1.25,
		FollowWeight: 0.75, FeedbackWeight: 1.5, MaxSignalWeight: 2,
	}
}

func (w ProfileWeighting) normalized() ProfileWeighting {
	d := DefaultProfileWeighting()
	if w.LongTermHalfLife <= 0 {
		w.LongTermHalfLife = d.LongTermHalfLife
	}
	if w.RecentHalfLife <= 0 {
		w.RecentHalfLife = d.RecentHalfLife
	}
	if w.MaxSignalWeight <= 0 {
		w.MaxSignalWeight = d.MaxSignalWeight
	}
	return w
}

type ProfileProjector struct {
	repo    ProfileProjectionRepository
	now     func() time.Time
	weights ProfileWeighting
}

func NewProfileProjector(repo ProfileProjectionRepository, options ...ProfileProjectorOption) *ProfileProjector {
	p := &ProfileProjector{repo: repo, now: func() time.Time { return time.Now().UTC() }, weights: DefaultProfileWeighting()}
	for _, option := range options {
		option(p)
	}
	p.weights = p.weights.normalized()
	return p
}

type ProfileProjectorOption func(*ProfileProjector)

func WithProfileWeighting(weights ProfileWeighting) ProfileProjectorOption {
	return func(p *ProfileProjector) { p.weights = weights }
}
func WithProfileProjectionNow(now func() time.Time) ProfileProjectorOption {
	return func(p *ProfileProjector) {
		if now != nil {
			p.now = now
		}
	}
}

func (p *ProfileProjector) ApplyView(ctx context.Context, event *applicationexposure.ViewEventRecordedEvent) (bool, error) {
	if event == nil || event.EventID == "" || event.UserID <= 0 || event.VideoID <= 0 {
		return false, domainrecommendation.ErrInvalidProfileEvent
	}
	weight, negative := p.viewWeight(event)
	if weight == 0 {
		return false, nil
	}
	return p.applyVideo(ctx, event.UserID, event.EventID, event.EventType, event.OccurredAt, event.VideoID, weight, negative, viewSourceSignal(event))
}

func (p *ProfileProjector) ApplyAction(ctx context.Context, event *applicationinteraction.ActionChangedEvent) (bool, error) {
	if event == nil || event.EventID == "" || !event.Active {
		return false, nil
	}
	weight := 0.0
	switch event.ActionType {
	case "LIKE":
		weight = p.weights.LikeWeight
	case "FAVORITE":
		weight = p.weights.FavoriteWeight
	default:
		return false, nil
	}
	return p.applyVideo(ctx, event.UserID, event.EventID, event.ActionType, event.OccurredAt, event.VideoID, weight, false, strconv.FormatBool(event.Active))
}

// ApplyFollow projects either a follow or an unfollow as an author-affinity event.
func (p *ProfileProjector) ApplyFollow(ctx context.Context, eventID string, userID, authorID int64, active bool, occurredAt time.Time) (bool, error) {
	if eventID == "" || userID <= 0 || authorID <= 0 || occurredAt.IsZero() {
		return false, domainrecommendation.ErrInvalidProfileEvent
	}
	weight := p.boundWeight(p.weights.FollowWeight)
	input := domainrecommendation.ProfileEventInput{
		UserID: userID, SourceEventID: eventID, EventType: "follow", OccurredAt: occurredAt,
		SourceAuthorID: authorID, SourceAction: "follow", SourceSignal: strconv.FormatBool(active),
		Decay: p.profileDecay(),
	}
	if active {
		input.AuthorAffinities = map[int64]float64{authorID: weight}
	} else {
		input.NegativeAuthorWeights = map[int64]float64{authorID: weight}
	}
	return p.apply(ctx, input)
}

func (p *ProfileProjector) applyVideo(ctx context.Context, userID int64, eventID, eventType string, occurredAt time.Time, videoID int64, weight float64, negative bool, sourceSignal string) (bool, error) {
	feature, ok, err := p.repo.LoadProfileFeature(ctx, videoID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, domainrecommendation.ErrProfileFeatureUnavailable
	}
	recentWeight := p.boundWeight(weight)
	if recentWeight == 0 {
		return false, nil
	}
	input := domainrecommendation.ProfileEventInput{
		UserID: userID, SourceEventID: eventID, EventType: eventType, OccurredAt: occurredAt,
		SourceVideoID: videoID, SourceAction: strings.ToLower(strings.TrimSpace(eventType)), SourceSignal: sourceSignal,
		Decay: p.profileDecay(),
	}
	if negative {
		input.NegativeTopicVector = scaleVector(feature.Vector, recentWeight)
		if feature.AuthorID > 0 {
			input.NegativeAuthorWeights = map[int64]float64{feature.AuthorID: recentWeight}
		}
	} else {
		longTermWeight := p.boundWeight(weight)
		input.LongTermVector = scaleVector(feature.Vector, longTermWeight)
		input.RecentVector = scaleVector(feature.Vector, recentWeight)
		if feature.AuthorID > 0 {
			input.AuthorAffinities = map[int64]float64{feature.AuthorID: recentWeight}
		}
	}
	return p.apply(ctx, input)
}

func (p *ProfileProjector) apply(ctx context.Context, input domainrecommendation.ProfileEventInput) (bool, error) {
	event, err := domainrecommendation.NewProfileEvent(input)
	if err != nil {
		return false, err
	}
	_, applied, err := p.repo.ApplyProfileEvent(ctx, event)
	return applied, err
}

func (p *ProfileProjector) viewWeight(event *applicationexposure.ViewEventRecordedEvent) (float64, bool) {
	ratio := 0.0
	if event.DurationMs != nil && *event.DurationMs > 0 {
		ratio = math.Max(float64(event.PositionMs), float64(event.WatchMs)) / float64(*event.DurationMs)
	}
	if event.Completed || event.EventType == "complete" {
		return p.weights.CompletionWeight, false
	}
	if event.EventType == "skip" && ratio <= .2 {
		return p.weights.SkipWeight, true
	}
	if event.EventType == "progress" && ratio >= .5 {
		return p.weights.ProgressWeight, false
	}
	return 0, false
}

func (p *ProfileProjector) boundWeight(weight float64) float64 {
	if weight <= 0 {
		return 0
	}
	if weight > p.weights.MaxSignalWeight {
		return p.weights.MaxSignalWeight
	}
	return weight
}
func scaleVector(values []float64, weight float64) []float64 {
	out := make([]float64, len(values))
	for i, value := range values {
		out[i] = value * weight
	}
	return out
}

// ApplyFeedback projects durable negative feedback using the feedback row ID as
// the stable source event ID; replay is therefore safe.
func (p *ProfileProjector) ApplyFeedback(ctx context.Context, feedback *domainrecommendation.Feedback) (bool, error) {
	if feedback == nil || feedback.ID <= 0 {
		return false, domainrecommendation.ErrInvalidProfileEvent
	}
	eventID := "feedback:" + strconv.FormatInt(feedback.ID, 10)
	if feedback.FeedbackType == domainrecommendation.FeedbackTypeReduceAuthor {
		if feedback.SuppressionScope != domainrecommendation.SuppressionScopeAuthor || feedback.SuppressionScopeID <= 0 {
			return false, domainrecommendation.ErrInvalidProfileEvent
		}
		weight := p.boundWeight(p.weights.FeedbackWeight)
		return p.apply(ctx, domainrecommendation.ProfileEventInput{
			UserID: feedback.UserID, SourceEventID: eventID, EventType: feedback.FeedbackType, OccurredAt: feedback.CreatedAt,
			SourceVideoID: feedback.VideoID, SourceAuthorID: feedback.SuppressionScopeID, SourceAction: feedback.FeedbackType,
			NegativeAuthorWeights: map[int64]float64{feedback.SuppressionScopeID: weight},
			Decay:                 p.profileDecay(),
		})
	}
	return p.applyVideo(ctx, feedback.UserID, eventID, feedback.FeedbackType, feedback.CreatedAt, feedback.VideoID, p.weights.FeedbackWeight, true, feedback.FeedbackType)
}

func (p *ProfileProjector) profileDecay() domainrecommendation.ProfileDecay {
	return domainrecommendation.ProfileDecay{
		LongTermHalfLife: p.weights.LongTermHalfLife,
		RecentHalfLife:   p.weights.RecentHalfLife,
	}.Normalized()
}

func viewSourceSignal(event *applicationexposure.ViewEventRecordedEvent) string {
	if event == nil {
		return ""
	}
	duration := 0
	if event.DurationMs != nil {
		duration = *event.DurationMs
	}
	return strconv.Itoa(event.PositionMs) + "|" + strconv.Itoa(event.WatchMs) + "|" + strconv.Itoa(duration) + "|" + strconv.FormatBool(event.Completed)
}
