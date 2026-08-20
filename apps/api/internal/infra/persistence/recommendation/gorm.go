package infrarecommendation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	applicationrecommendation "github.com/shiyudesu/frux/internal/application/recommendation"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainexposure "github.com/shiyudesu/frux/internal/domain/exposure"
	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	domainvideo "github.com/shiyudesu/frux/internal/domain/video"
	infraexposure "github.com/shiyudesu/frux/internal/infra/persistence/exposure"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const hotScoreExpression = "COALESCE(vs.like_count, 0) * 3 + COALESCE(vs.comment_count, 0) * 5 + COALESCE(vs.favorite_count, 0) * 4"
const positiveEventWindow = 30 * 24 * time.Hour

const (
	servedEvidenceKindFirstPage    = "first_page"
	servedEvidenceKindDegradedPage = "degraded_page"
)

type Repository struct {
	db *gorm.DB
}

type candidateModel struct {
	VideoID     int64
	AuthorID    int64
	HotScore    int
	PublishedAt time.Time
}

type videoVectorModel struct {
	VideoID       int64
	EmbeddingJSON string
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListCandidatePool(ctx context.Context, userID int64, limit int) ([]*domainrecommendation.Candidate, error) {
	if limit <= 0 {
		return []*domainrecommendation.Candidate{}, nil
	}

	var models []candidateModel
	err := r.db.WithContext(ctx).
		Table("video AS v").
		Select("v.id AS video_id, v.author_id, ("+hotScoreExpression+") AS hot_score, v.published_at").
		Joins("LEFT JOIN video_stat AS vs ON vs.video_id = v.id").
		Where("v.status = ? AND v.visibility = ? AND v.media_status IN ? AND v.published_at IS NOT NULL", domainvideo.StatusPublished, domainvideo.VisibilityPublic, []string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady}).
		Order("hot_score DESC").
		Order("v.published_at DESC").
		Order("v.id DESC").
		Limit(limit).
		Scan(&models).
		Error
	if err != nil {
		return nil, err
	}

	candidates := make([]*domainrecommendation.Candidate, 0, len(models))
	for _, model := range models {
		candidates = append(candidates, domainrecommendation.RestoreCandidate(
			model.VideoID,
			model.AuthorID,
			0,
			0,
			model.HotScore,
			0,
			"",
			model.PublishedAt,
		))
	}
	return candidates, nil
}

// ListFreshCandidates returns only currently readable public videos in stable
// publish order. Recall callers still revalidate the IDs before response use.
func (r *Repository) ListFreshCandidates(ctx context.Context, limit int) ([]*domainrecommendation.Candidate, error) {
	return r.listPublicCandidates(ctx, limit, nil, "", "v.published_at DESC", "v.id DESC")
}

func (r *Repository) ListHotCandidates(ctx context.Context, limit int) ([]*domainrecommendation.Candidate, error) {
	return r.listPublicCandidates(ctx, limit, nil, "", "hot_score DESC", "v.published_at DESC", "v.id DESC")
}

func (r *Repository) ListPublicCandidatesByAuthors(ctx context.Context, authorIDs []int64, limit int) ([]*domainrecommendation.Candidate, error) {
	if len(authorIDs) == 0 {
		return []*domainrecommendation.Candidate{}, nil
	}
	return r.listPublicCandidates(ctx, limit, authorIDs, "", "v.published_at DESC", "v.id DESC")
}

func (r *Repository) ListEmbeddingCandidates(ctx context.Context, model string, limit int) ([]*domainrecommendation.Candidate, error) {
	model = strings.TrimSpace(model)
	if model == "" || limit <= 0 {
		return []*domainrecommendation.Candidate{}, nil
	}
	var models []candidateModel
	err := r.db.WithContext(ctx).
		Table("video AS v").
		Select("v.id AS video_id, v.author_id, ("+hotScoreExpression+") AS hot_score, v.published_at").
		Joins("JOIN video_embedding AS ve ON ve.video_id = v.id AND ve.model = ?", model).
		Joins("LEFT JOIN video_stat AS vs ON vs.video_id = v.id").
		Where("v.status = ? AND v.visibility = ? AND v.media_status IN ? AND v.published_at IS NOT NULL", domainvideo.StatusPublished, domainvideo.VisibilityPublic, []string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady}).
		Order("v.published_at DESC").Order("v.id DESC").Limit(limit).Scan(&models).Error
	if err != nil {
		return nil, err
	}
	return candidatesFromModels(models), nil
}

// ListVisibleCandidates is the final visibility gate for recalled IDs.
func (r *Repository) ListVisibleCandidates(ctx context.Context, videoIDs []int64) ([]*domainrecommendation.Candidate, error) {
	if len(videoIDs) == 0 {
		return []*domainrecommendation.Candidate{}, nil
	}
	return r.listPublicCandidates(ctx, len(videoIDs), nil, videoIDs, "v.published_at DESC", "v.id DESC")
}

func (r *Repository) listPublicCandidates(ctx context.Context, limit int, authorIDs []int64, videoIDs any, orders ...string) ([]*domainrecommendation.Candidate, error) {
	if limit <= 0 {
		return []*domainrecommendation.Candidate{}, nil
	}
	query := r.db.WithContext(ctx).
		Table("video AS v").
		Select("v.id AS video_id, v.author_id, ("+hotScoreExpression+") AS hot_score, v.published_at").
		Joins("LEFT JOIN video_stat AS vs ON vs.video_id = v.id").
		Where("v.status = ? AND v.visibility = ? AND v.media_status IN ? AND v.published_at IS NOT NULL", domainvideo.StatusPublished, domainvideo.VisibilityPublic, []string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady})
	if len(authorIDs) > 0 {
		query = query.Where("v.author_id IN ?", authorIDs)
	}
	if ids, ok := videoIDs.([]int64); ok && len(ids) > 0 {
		query = query.Where("v.id IN ?", ids)
	}
	for _, order := range orders {
		query = query.Order(order)
	}
	var models []candidateModel
	if err := query.Limit(limit).Scan(&models).Error; err != nil {
		return nil, err
	}
	return candidatesFromModels(models), nil
}

func candidatesFromModels(models []candidateModel) []*domainrecommendation.Candidate {
	candidates := make([]*domainrecommendation.Candidate, 0, len(models))
	for _, model := range models {
		candidates = append(candidates, domainrecommendation.RestoreCandidate(
			model.VideoID, model.AuthorID, 0, 0, model.HotScore, 0, "", model.PublishedAt,
		))
	}
	return candidates
}

func (r *Repository) LoadUserInterestVector(ctx context.Context, userID int64) ([]float64, bool, error) {
	since := time.Now().Add(-positiveEventWindow)
	rows, err := r.db.WithContext(ctx).
		Table(`(
			SELECT DISTINCT ON (
				user_id, video_id, event_type, COALESCE(playback_session_id, event_id)
			) *
			FROM recommendation_behavior_event
			WHERE user_id = ? AND occurred_at >= ? AND event_type IN ?
			ORDER BY user_id, video_id, event_type, COALESCE(playback_session_id, event_id),
				sequence DESC NULLS LAST, occurred_at DESC, event_id DESC
		) AS ev`, userID, since, positiveEventTypes()).
		Select("ve.embedding_json, ev.event_type, ev.position_ms, ev.watch_ms, ev.duration_ms, ev.completed").
		Joins("JOIN video_embedding AS ve ON ve.video_id = ev.video_id AND ve.model = ?", domainembedding.HashNgramModel).
		Order("ev.occurred_at DESC").
		Limit(200).
		Rows()
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var sum []float64
	var totalWeight float64
	for rows.Next() {
		var embeddingJSON string
		var eventType string
		var positionMs int
		var watchMs int
		var durationMs *int
		var completed bool
		if err := rows.Scan(&embeddingJSON, &eventType, &positionMs, &watchMs, &durationMs, &completed); err != nil {
			return nil, false, err
		}
		vector, err := decodeVector(embeddingJSON)
		if err != nil || len(vector) == 0 {
			continue
		}
		if len(sum) == 0 {
			sum = make([]float64, len(vector))
		}
		if len(vector) != len(sum) {
			continue
		}
		weight := eventWeight(eventType, positionMs, watchMs, durationMs, completed)
		for i := range vector {
			sum[i] += vector[i] * weight
		}
		totalWeight += weight
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(sum) == 0 || totalWeight == 0 {
		return nil, false, nil
	}
	for i := range sum {
		sum[i] = sum[i] / totalWeight
	}
	return sum, true, nil
}

func (r *Repository) LoadUserInterestProfile(ctx context.Context, userID int64) (*domainrecommendation.UserInterestProfile, bool, error) {
	if userID <= 0 {
		return nil, false, nil
	}
	var model UserInterestProfileModel
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	profile, err := profileFromModel(model)
	return profile, err == nil, err
}

// RebuildUserInterestVector is a bounded cold-start reconstruction over durable
// behavior, interaction, follow, feedback, and embedding facts. It is never
// used after a materialized profile exists.
func (r *Repository) RebuildUserInterestVector(ctx context.Context, userID int64) ([]float64, bool, error) {
	vector, found, err := r.LoadUserInterestVector(ctx, userID)
	if err != nil {
		return nil, false, err
	}
	cutoff := time.Now().UTC().Add(-positiveEventWindow)
	type row struct {
		EmbeddingJSON string
		Weight        float64
	}
	var rows []row
	err = r.db.WithContext(ctx).Table("interaction_action AS ia").
		Select("ve.embedding_json, CASE ia.action_type WHEN 'FAVORITE' THEN 1.25 ELSE 1 END AS weight").
		Joins("JOIN video_embedding AS ve ON ve.video_id = ia.video_id AND ve.model = ?", domainembedding.HashNgramModel).
		Where("ia.user_id = ? AND ia.status = ? AND ia.updated_at >= ? AND ia.action_type IN ?", userID, 1, cutoff, []string{"LIKE", "FAVORITE"}).
		Order("ia.updated_at DESC").Limit(100).Scan(&rows).Error
	if err != nil {
		return nil, false, err
	}
	for _, row := range rows {
		vector, found = mergeWeightedVector(vector, found, row.EmbeddingJSON, row.Weight)
	}
	rows = nil
	err = r.db.WithContext(ctx).Table("user_follow AS f").
		Select("ve.embedding_json, 0.75 AS weight").
		Joins("JOIN video AS v ON v.author_id = f.target_user_id").
		Joins("JOIN video_embedding AS ve ON ve.video_id = v.id AND ve.model = ?", domainembedding.HashNgramModel).
		Where("f.user_id = ? AND f.status = ? AND f.updated_at >= ?", userID, 1, cutoff).
		Order("v.published_at DESC").Limit(100).Scan(&rows).Error
	if err != nil {
		return nil, false, err
	}
	for _, row := range rows {
		vector, found = mergeWeightedVector(vector, found, row.EmbeddingJSON, row.Weight)
	}
	rows = nil
	err = r.db.WithContext(ctx).Table("recommendation_feedback AS rf").
		Select("ve.embedding_json, -1.5 AS weight").
		Joins("JOIN video_embedding AS ve ON ve.video_id = rf.video_id AND ve.model = ?", domainembedding.HashNgramModel).
		Where("rf.user_id = ? AND rf.created_at >= ?", userID, cutoff).
		Order("rf.created_at DESC").Limit(100).Scan(&rows).Error
	if err != nil {
		return nil, false, err
	}
	for _, row := range rows {
		vector, found = mergeWeightedVector(vector, found, row.EmbeddingJSON, row.Weight)
	}
	return vector, found, nil
}

func mergeWeightedVector(current []float64, found bool, encoded string, weight float64) ([]float64, bool) {
	value, err := decodeVector(encoded)
	if err != nil || len(value) == 0 {
		return current, found
	}
	if !found {
		current = make([]float64, len(value))
		found = true
	}
	if len(current) != len(value) {
		return current, found
	}
	for index := range current {
		current[index] += value[index] * weight
	}
	return current, found
}

// rebuildUserInterestProfile is deliberately tx-aware so initial projection
// can preserve bounded durable facts without recursively entering repository
// methods or taking another transaction/advisory lock.
func rebuildUserInterestProfile(ctx context.Context, tx *gorm.DB, userID int64, before time.Time) (*domainrecommendation.UserInterestProfile, bool, []AppliedProfileEventModel, error) {
	if userID <= 0 || before.IsZero() {
		return nil, false, nil, nil
	}
	before = before.UTC()
	cutoff := before.Add(-positiveEventWindow)
	accumulator := profileReconstruction{
		authors:         map[int64]float64{},
		negativeAuthors: map[int64]float64{},
	}
	applied := make([]AppliedProfileEventModel, 0, 200)

	var behavior []profileBehaviorFact
	if err := tx.WithContext(ctx).Table("recommendation_behavior_event AS ev").
		Select("ev.event_id, ev.video_id, ve.embedding_json, v.author_id, ev.event_type, ev.position_ms, ev.watch_ms, ev.duration_ms, ev.completed, ev.occurred_at").
		Joins("JOIN video_embedding AS ve ON ve.video_id = ev.video_id AND ve.model = ?", domainembedding.HashNgramModel).
		Joins("JOIN video AS v ON v.id = ev.video_id").
		Where("ev.user_id = ? AND ev.occurred_at >= ? AND ev.occurred_at < ? AND ev.event_type IN ?",
			userID, cutoff, before, []string{domainexposure.EventTypeProgress, domainexposure.EventTypeComplete, domainexposure.EventTypeSkip}).
		Order("ev.occurred_at DESC, ev.event_id DESC").Limit(200).Scan(&behavior).Error; err != nil {
		return nil, false, nil, err
	}
	for _, fact := range behavior {
		weight, negative := profileBehaviorWeight(fact)
		if weight <= 0 {
			continue
		}
		if negative {
			accumulator.addNegative(fact.EmbeddingJSON, fact.AuthorID, weight*profileFactDecay(before, fact.OccurredAt, domainrecommendation.DefaultProfileRecentHalfLife))
		} else {
			accumulator.addPositive(
				fact.EmbeddingJSON,
				fact.AuthorID,
				weight*profileFactDecay(before, fact.OccurredAt, domainrecommendation.DefaultProfileLongTermHalfLife),
				weight*profileFactDecay(before, fact.OccurredAt, domainrecommendation.DefaultProfileRecentHalfLife),
			)
		}
		appliedEvent, err := profileBehaviorAppliedEvent(userID, fact)
		if err != nil {
			return nil, false, nil, err
		}
		applied = append(applied, appliedEvent)
	}

	var actions []profileActionFact
	// Facts with an undelivered profile outbox are deliberately excluded:
	// their owning worker will apply the stable event ID after this
	// transaction. Including them here would make the later dispatch count
	// the same action twice.
	if err := tx.WithContext(ctx).Table("interaction_action AS ia").
		Select("ve.embedding_json, v.author_id, ia.action_type, ia.updated_at").
		Joins("JOIN video AS v ON v.id = ia.video_id").
		Joins("JOIN video_embedding AS ve ON ve.video_id = ia.video_id AND ve.model = ?", domainembedding.HashNgramModel).
		Where("ia.user_id = ? AND ia.status = ? AND ia.updated_at >= ? AND ia.updated_at < ? AND ia.action_type IN ?",
			userID, 1, cutoff, before, []string{"LIKE", "FAVORITE"}).
		Where(`NOT EXISTS (
			SELECT 1 FROM interaction_action_event AS iae
			WHERE iae.user_id = ia.user_id AND iae.video_id = ia.video_id AND iae.action_type = ia.action_type
				AND iae.active = TRUE AND iae.profile_projection_dispatched_at IS NULL
		)`).
		Order("ia.updated_at DESC").Limit(100).Scan(&actions).Error; err != nil {
		return nil, false, nil, err
	}
	for _, fact := range actions {
		weight := 1.0
		if fact.ActionType == "FAVORITE" {
			weight = 1.25
		}
		accumulator.addPositive(
			fact.EmbeddingJSON,
			fact.AuthorID,
			weight*profileFactDecay(before, fact.UpdatedAt, domainrecommendation.DefaultProfileLongTermHalfLife),
			weight*profileFactDecay(before, fact.UpdatedAt, domainrecommendation.DefaultProfileRecentHalfLife),
		)
	}

	var follows []profileFollowFact
	// Follow and feedback queues have the same ownership boundary as action
	// receipts. Bootstrap only reads facts whose projection queue is settled.
	if err := tx.WithContext(ctx).Table("user_follow").
		Select("target_user_id AS author_id, updated_at").
		Where("user_id = ? AND status = ? AND updated_at >= ? AND updated_at < ?", userID, 1, cutoff, before).
		Where(`NOT EXISTS (
			SELECT 1 FROM relation_profile_projection_outbox AS fo
			WHERE fo.user_id = user_follow.user_id AND fo.target_user_id = user_follow.target_user_id
				AND fo.dispatched_at IS NULL
		)`).
		Order("updated_at DESC").Limit(domainrecommendation.MaxProfileAffinityEntries).Scan(&follows).Error; err != nil {
		return nil, false, nil, err
	}
	for _, fact := range follows {
		accumulator.addAuthor(fact.AuthorID, 0.75*profileFactDecay(before, fact.UpdatedAt, domainrecommendation.DefaultProfileRecentHalfLife), false)
	}

	var feedback []profileFeedbackFact
	if err := tx.WithContext(ctx).Table("recommendation_feedback AS rf").
		Select("ve.embedding_json, v.author_id, rf.feedback_type, rf.suppression_scope, rf.suppression_scope_id, rf.created_at").
		Joins("JOIN video AS v ON v.id = rf.video_id").
		Joins("JOIN video_embedding AS ve ON ve.video_id = rf.video_id AND ve.model = ?", domainembedding.HashNgramModel).
		Where("rf.user_id = ? AND rf.created_at >= ? AND rf.created_at < ?", userID, cutoff, before).
		Where(`NOT EXISTS (
			SELECT 1 FROM recommendation_feedback_profile_outbox AS fo
			WHERE fo.feedback_id = rf.id AND fo.dispatched_at IS NULL
		)`).
		Order("rf.created_at DESC").Limit(100).Scan(&feedback).Error; err != nil {
		return nil, false, nil, err
	}
	for _, fact := range feedback {
		weight := 1.5 * profileFactDecay(before, fact.CreatedAt, domainrecommendation.DefaultProfileRecentHalfLife)
		accumulator.addFeedback(fact, weight)
	}

	if !accumulator.found {
		return nil, false, applied, nil
	}
	event, err := domainrecommendation.NewProfileEvent(domainrecommendation.ProfileEventInput{
		UserID: userID, SourceEventID: "profile-bootstrap", EventType: "bootstrap", OccurredAt: before,
		LongTermVector: accumulator.longTerm, RecentVector: accumulator.recent,
		AuthorAffinities:    accumulator.boundedAffinities(false),
		NegativeTopicVector: accumulator.negativeTopics, NegativeAuthorWeights: accumulator.boundedAffinities(true),
	})
	if err != nil {
		return nil, false, nil, err
	}
	profile, err := domainrecommendation.EmptyUserInterestProfile(userID, before).Apply(event)
	if err != nil {
		return nil, false, nil, err
	}
	return profile, true, applied, nil
}

type profileBehaviorFact struct {
	EventID       string
	VideoID       int64
	EmbeddingJSON string
	AuthorID      int64
	EventType     string
	PositionMs    int
	WatchMs       int
	DurationMs    *int
	Completed     bool
	OccurredAt    time.Time
}

type profileActionFact struct {
	EmbeddingJSON string
	AuthorID      int64
	ActionType    string
	UpdatedAt     time.Time
}

type profileFollowFact struct {
	AuthorID  int64
	UpdatedAt time.Time
}

type profileFeedbackFact struct {
	EmbeddingJSON      string
	AuthorID           int64
	FeedbackType       string
	SuppressionScope   string
	SuppressionScopeID int64
	CreatedAt          time.Time
}

// profileBehaviorWeight mirrors ProfileProjector's supported durable view
// signals. Behavior rows have no projection outbox, so bootstrap claims their
// exact source IDs below; otherwise a redelivery after reconstruction could
// apply the same view signal twice.
func profileBehaviorWeight(fact profileBehaviorFact) (float64, bool) {
	ratio := 0.0
	if fact.DurationMs != nil && *fact.DurationMs > 0 {
		ratio = math.Max(float64(fact.PositionMs), float64(fact.WatchMs)) / float64(*fact.DurationMs)
	}
	if fact.Completed || fact.EventType == domainexposure.EventTypeComplete {
		return 1, false
	}
	if fact.EventType == domainexposure.EventTypeSkip && ratio <= .2 {
		return .8, true
	}
	if fact.EventType == domainexposure.EventTypeProgress && ratio >= .5 {
		return .5, false
	}
	return 0, false
}

func profileBehaviorAppliedEvent(userID int64, fact profileBehaviorFact) (AppliedProfileEventModel, error) {
	duration := 0
	if fact.DurationMs != nil {
		duration = *fact.DurationMs
	}
	event, err := domainrecommendation.NewProfileEvent(domainrecommendation.ProfileEventInput{
		UserID: userID, SourceEventID: fact.EventID, EventType: fact.EventType, OccurredAt: fact.OccurredAt,
		SourceVideoID: fact.VideoID, SourceAction: strings.ToLower(strings.TrimSpace(fact.EventType)),
		SourceSignal: strconv.Itoa(fact.PositionMs) + "|" + strconv.Itoa(fact.WatchMs) + "|" + strconv.Itoa(duration) + "|" + strconv.FormatBool(fact.Completed),
	})
	if err != nil {
		return AppliedProfileEventModel{}, err
	}
	return AppliedProfileEventModel{
		UserID: event.UserID, SourceEventID: event.SourceEventID, PayloadHash: event.PayloadHash, AppliedAt: event.OccurredAt,
	}, nil
}

func persistBootstrapAppliedProfileEvents(tx *gorm.DB, applied []AppliedProfileEventModel) error {
	for _, event := range applied {
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			continue
		}
		var existing AppliedProfileEventModel
		if err := tx.Where("user_id = ? AND source_event_id = ?", event.UserID, event.SourceEventID).Take(&existing).Error; err != nil {
			return err
		}
		if existing.PayloadHash != event.PayloadHash {
			return domainrecommendation.ErrProfileEventConflict
		}
	}
	return nil
}

type profileReconstruction struct {
	longTerm        []float64
	recent          []float64
	negativeTopics  []float64
	authors         map[int64]float64
	negativeAuthors map[int64]float64
	found           bool
}

func (r *profileReconstruction) addPositive(encoded string, authorID int64, longTermWeight, recentWeight float64) {
	r.longTerm, r.found = addReconstructedVector(r.longTerm, r.found, encoded, longTermWeight)
	r.recent, r.found = addReconstructedVector(r.recent, r.found, encoded, recentWeight)
	r.addAuthor(authorID, recentWeight, false)
}

func (r *profileReconstruction) addNegative(encoded string, authorID int64, weight float64) {
	r.negativeTopics, r.found = addReconstructedVector(r.negativeTopics, r.found, encoded, weight)
	r.addAuthor(authorID, weight, true)
}

func (r *profileReconstruction) addFeedback(fact profileFeedbackFact, weight float64) {
	if fact.FeedbackType == domainrecommendation.FeedbackTypeReduceAuthor {
		if fact.SuppressionScope == domainrecommendation.SuppressionScopeAuthor && fact.SuppressionScopeID > 0 {
			r.addAuthor(fact.SuppressionScopeID, weight, true)
		}
		return
	}
	r.addNegative(fact.EmbeddingJSON, fact.AuthorID, weight)
	if fact.SuppressionScope == domainrecommendation.SuppressionScopeAuthor && fact.SuppressionScopeID > 0 {
		r.addAuthor(fact.SuppressionScopeID, weight, true)
	}
}

func (r *profileReconstruction) addAuthor(authorID int64, weight float64, negative bool) {
	if authorID <= 0 || weight <= 0 {
		return
	}
	target := r.authors
	if negative {
		target = r.negativeAuthors
	}
	target[authorID] = math.Min(domainrecommendation.MaxProfileComponentWeight, target[authorID]+weight)
	r.found = true
}

func (r *profileReconstruction) boundedAffinities(negative bool) map[int64]float64 {
	source := r.authors
	if negative {
		source = r.negativeAuthors
	}
	type pair struct {
		authorID int64
		weight   float64
	}
	pairs := make([]pair, 0, len(source))
	for authorID, weight := range source {
		if weight > 0 {
			pairs = append(pairs, pair{authorID: authorID, weight: weight})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].weight == pairs[j].weight {
			return pairs[i].authorID < pairs[j].authorID
		}
		return pairs[i].weight > pairs[j].weight
	})
	if len(pairs) > domainrecommendation.MaxProfileAffinityEntries {
		pairs = pairs[:domainrecommendation.MaxProfileAffinityEntries]
	}
	result := make(map[int64]float64, len(pairs))
	for _, item := range pairs {
		result[item.authorID] = item.weight
	}
	return result
}

func addReconstructedVector(current []float64, found bool, encoded string, weight float64) ([]float64, bool) {
	if weight <= 0 {
		return current, found
	}
	vector, err := decodeVector(encoded)
	if err != nil || len(vector) == 0 || len(vector) > domainrecommendation.MaxProfileVectorDimensions {
		return current, found
	}
	if len(current) == 0 {
		current = make([]float64, len(vector))
	}
	if len(current) != len(vector) {
		return current, found
	}
	for index, value := range vector {
		current[index] = math.Max(-domainrecommendation.MaxProfileComponentWeight, math.Min(domainrecommendation.MaxProfileComponentWeight, current[index]+value*weight))
	}
	return current, true
}

func profileFactDecay(before, occurredAt time.Time, halfLife time.Duration) float64 {
	if occurredAt.IsZero() || !before.After(occurredAt) || halfLife <= 0 {
		return 1
	}
	return math.Exp(-math.Ln2 * before.Sub(occurredAt).Hours() / halfLife.Hours())
}

func (r *Repository) LoadProfileFeature(ctx context.Context, videoID int64) (applicationrecommendation.ProfileFeature, bool, error) {
	if videoID <= 0 {
		return applicationrecommendation.ProfileFeature{}, false, nil
	}
	var model struct {
		AuthorID      int64
		EmbeddingJSON string
	}
	err := r.db.WithContext(ctx).Table("video AS v").Select("v.author_id, ve.embedding_json").Joins("JOIN video_embedding AS ve ON ve.video_id = v.id AND ve.model = ?", domainembedding.HashNgramModel).Where("v.id = ?", videoID).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return applicationrecommendation.ProfileFeature{}, false, nil
	}
	if err != nil {
		return applicationrecommendation.ProfileFeature{}, false, err
	}
	vector, err := decodeVector(model.EmbeddingJSON)
	if err != nil || len(vector) == 0 {
		return applicationrecommendation.ProfileFeature{}, false, err
	}
	return applicationrecommendation.ProfileFeature{Vector: vector, AuthorID: model.AuthorID}, true, nil
}

func (r *Repository) ApplyBehaviorEvent(ctx context.Context, event *applicationexposure.ViewEventRecordedEvent) (bool, error) {
	if event == nil || event.EventID == "" {
		return false, nil
	}
	model := behaviorEventModel(event)
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&model)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		return true, nil
	}
	var existing BehaviorEventModel
	err := r.db.WithContext(ctx).
		Where("(user_id = ? AND event_id = ?) OR view_event_id = ?", event.UserID, event.EventID, event.ViewEventID).
		Take(&existing).Error
	if err != nil {
		return false, err
	}
	if !sameBehaviorEvent(existing, model) {
		return false, applicationrecommendation.ErrBehaviorEventConflict
	}
	return false, nil
}

func (r *Repository) CompareBehaviorEvent(
	ctx context.Context,
	event *applicationexposure.ViewEventRecordedEvent,
) (bool, bool, error) {
	if event == nil || event.EventID == "" {
		return false, false, applicationrecommendation.ErrBehaviorEventConflict
	}
	var existing BehaviorEventModel
	err := r.db.WithContext(ctx).
		Where("(user_id = ? AND event_id = ?) OR view_event_id = ?", event.UserID, event.EventID, event.ViewEventID).
		Take(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, sameBehaviorEvent(existing, behaviorEventModel(event)), nil
}

func behaviorEventModel(event *applicationexposure.ViewEventRecordedEvent) BehaviorEventModel {
	return BehaviorEventModel{
		EventID: event.EventID, ViewEventID: event.ViewEventID, UserID: event.UserID, VideoID: event.VideoID,
		Scene: strings.ToLower(strings.TrimSpace(event.Scene)), RequestID: strings.TrimSpace(event.RequestID),
		EventType: event.EventType, PlaybackSessionID: stringPtr(event.PlaybackSessionID),
		Sequence: int64Ptr(event.Sequence), PositionMs: event.PositionMs, WatchMs: event.WatchMs,
		DurationMs: cloneInt(event.DurationMs), Completed: event.Completed,
		ExposureCount: event.ExposureCount,
		OccurredAt:    event.OccurredAt.UTC(), RecordedAt: recordedAtFromViewEvent(event),
	}
}

func sameBehaviorEvent(left, right BehaviorEventModel) bool {
	return left.EventID == right.EventID &&
		left.ViewEventID == right.ViewEventID &&
		left.UserID == right.UserID &&
		left.VideoID == right.VideoID &&
		left.Scene == right.Scene &&
		left.RequestID == right.RequestID &&
		left.EventType == right.EventType &&
		stringValue(left.PlaybackSessionID) == stringValue(right.PlaybackSessionID) &&
		int64Value(left.Sequence) == int64Value(right.Sequence) &&
		left.PositionMs == right.PositionMs &&
		left.WatchMs == right.WatchMs &&
		intValue(left.DurationMs) == intValue(right.DurationMs) &&
		(left.DurationMs == nil) == (right.DurationMs == nil) &&
		left.Completed == right.Completed &&
		left.ExposureCount == right.ExposureCount &&
		left.OccurredAt.Equal(right.OccurredAt) &&
		left.RecordedAt.Equal(right.RecordedAt)
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func (r *Repository) ClaimBehaviorProfileProjections(ctx context.Context, limit int, now, leasedUntil time.Time) ([]applicationrecommendation.BehaviorProfileProjectionItem, error) {
	if limit <= 0 {
		return []applicationrecommendation.BehaviorProfileProjectionItem{}, nil
	}
	items := make([]applicationrecommendation.BehaviorProfileProjectionItem, 0, limit)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var events []BehaviorEventModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("profile_dispatched_at IS NULL AND profile_available_at <= ? AND (profile_leased_until IS NULL OR profile_leased_until <= ?)", now, now).
			Order("profile_available_at ASC, occurred_at ASC, event_id ASC").
			Limit(limit).
			Find(&events).Error; err != nil {
			return err
		}
		for _, event := range events {
			if err := tx.Model(&BehaviorEventModel{}).
				Where("user_id = ? AND event_id = ?", event.UserID, event.EventID).
				Updates(map[string]any{
					"profile_leased_until": leasedUntil,
					"profile_attempts":     gorm.Expr("profile_attempts + 1"),
				}).Error; err != nil {
				return err
			}
			items = append(items, applicationrecommendation.BehaviorProfileProjectionItem{
				EventID: event.EventID, UserID: event.UserID, VideoID: event.VideoID, Scene: event.Scene,
				RequestID: event.RequestID, EventType: event.EventType, PlaybackSessionID: stringValue(event.PlaybackSessionID),
				Sequence: int64Value(event.Sequence), PositionMs: event.PositionMs, WatchMs: event.WatchMs,
				DurationMs: cloneInt(event.DurationMs), Completed: event.Completed, RecordedAt: behaviorRecordedAt(event), OccurredAt: event.OccurredAt,
				Attempts: event.ProfileAttempts + 1,
			})
		}
		return nil
	})
	return items, err
}

func (r *Repository) MarkBehaviorProfileProjectionDispatched(ctx context.Context, userID int64, eventID string, dispatchedAt time.Time) error {
	return r.db.WithContext(ctx).Model(&BehaviorEventModel{}).Where("user_id = ? AND event_id = ?", userID, eventID).Updates(map[string]any{
		"profile_dispatched_at": dispatchedAt.UTC(),
		"profile_leased_until":  nil,
		"profile_last_error":    "",
	}).Error
}

func (r *Repository) MarkBehaviorProfileProjectionFailed(ctx context.Context, userID int64, eventID string, availableAt time.Time, reason string) error {
	return r.db.WithContext(ctx).Model(&BehaviorEventModel{}).Where("user_id = ? AND event_id = ?", userID, eventID).Updates(map[string]any{
		"profile_available_at": availableAt.UTC(),
		"profile_leased_until": nil,
		"profile_last_error":   truncateBehaviorProfileError(reason),
	}).Error
}

func (r *Repository) LoadVideoVectors(ctx context.Context, videoIDs []int64) (map[int64][]float64, error) {
	return r.LoadVectors(ctx, videoIDs, domainembedding.HashNgramModel)
}

// LoadRankingFeatures batches relationship, exposure, and explicit-feedback
// facts for an already bounded candidate pool.
func (r *Repository) LoadRankingFeatures(ctx context.Context, userID int64, videoIDs []int64, since, now time.Time) (*domainrecommendation.RankingFeatures, error) {
	features := &domainrecommendation.RankingFeatures{
		FollowedAuthors:   map[int64]bool{},
		RecentExposures:   map[int64]*domainrecommendation.Exposure{},
		NegativeVideos:    map[int64]bool{},
		NegativeAuthors:   map[int64]bool{},
		SuppressedVideos:  map[int64]bool{},
		SuppressedAuthors: map[int64]bool{},
	}
	if userID <= 0 || len(videoIDs) == 0 {
		return features, nil
	}
	profile, found, err := r.LoadUserInterestProfile(ctx, userID)
	if err != nil {
		return nil, err
	}
	if found {
		features.Profile = profile
	}

	authorIDs := make([]int64, 0, len(videoIDs))
	if err := r.db.WithContext(ctx).Table("video").Where("id IN ?", videoIDs).Distinct().Pluck("author_id", &authorIDs).Error; err != nil {
		return nil, err
	}
	var followedIDs []int64
	if len(authorIDs) > 0 {
		if err := r.db.WithContext(ctx).Table("user_follow").
			Where("user_id = ? AND target_user_id IN ? AND status = ?", userID, authorIDs, 1).
			Pluck("target_user_id", &followedIDs).Error; err != nil {
			return nil, err
		}
	}
	for _, authorID := range followedIDs {
		features.FollowedAuthors[authorID] = true
	}

	exposures, err := r.ListRecentExposures(ctx, userID, videoIDs, since)
	if err != nil {
		return nil, err
	}
	for _, exposure := range exposures {
		if exposure != nil {
			features.RecentExposures[exposure.VideoID] = exposure
		}
	}

	type feedbackFeature struct {
		VideoID            int64
		AuthorID           int64
		FeedbackType       string
		SuppressionScope   string
		SuppressionScopeID int64
	}
	var feedback []feedbackFeature
	if err := r.db.WithContext(ctx).Table("recommendation_feedback AS rf").
		Select("rf.video_id, v.author_id, rf.feedback_type, rf.suppression_scope, rf.suppression_scope_id").
		Joins("JOIN video AS v ON v.id = rf.video_id").
		Where("rf.user_id = ? AND rf.created_at >= ? AND (rf.video_id IN ? OR v.author_id IN ?)", userID, since, videoIDs, authorIDs).
		Find(&feedback).Error; err != nil {
		return nil, err
	}
	for _, item := range feedback {
		switch item.FeedbackType {
		case domainrecommendation.FeedbackTypeNotInterested, domainrecommendation.FeedbackTypeAlreadySeen:
			features.NegativeVideos[item.VideoID] = true
		case domainrecommendation.FeedbackTypeReduceAuthor:
			features.NegativeAuthors[item.AuthorID] = true
		}
	}
	var activeSuppression []feedbackFeature
	if err := r.db.WithContext(ctx).Table("recommendation_feedback AS rf").
		Select("rf.video_id, v.author_id, rf.feedback_type, rf.suppression_scope, rf.suppression_scope_id").
		Joins("JOIN video AS v ON v.id = rf.video_id").
		Where("rf.user_id = ? AND rf.suppression_expires_at > ? AND (rf.suppression_scope_id IN ? OR v.author_id IN ?)", userID, now.UTC(), videoIDs, authorIDs).
		Find(&activeSuppression).Error; err != nil {
		return nil, err
	}
	for _, item := range activeSuppression {
		if item.SuppressionScope == domainrecommendation.SuppressionScopeAuthor {
			features.SuppressedAuthors[item.SuppressionScopeID] = true
		} else {
			features.SuppressedVideos[item.SuppressionScopeID] = true
		}
	}
	return features, nil
}

func (r *Repository) LoadVectors(ctx context.Context, videoIDs []int64, model string) (map[int64][]float64, error) {
	vectors := map[int64][]float64{}
	model = strings.TrimSpace(model)
	if len(videoIDs) == 0 || model == "" {
		return vectors, nil
	}

	var models []videoVectorModel
	err := r.db.WithContext(ctx).
		Table("video_embedding").
		Select("video_id, embedding_json").
		Where("video_id IN ? AND model = ?", videoIDs, model).
		Scan(&models).
		Error
	if err != nil {
		return nil, err
	}
	for _, model := range models {
		vector, err := decodeVector(model.EmbeddingJSON)
		if err != nil {
			continue
		}
		vectors[model.VideoID] = vector
	}
	return vectors, nil
}

func (r *Repository) ListRecentExposures(ctx context.Context, userID int64, videoIDs []int64, since time.Time) ([]*domainrecommendation.Exposure, error) {
	if len(videoIDs) == 0 {
		return []*domainrecommendation.Exposure{}, nil
	}

	var models []infraexposure.ExposureModel
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND video_id IN ? AND last_exposed_at >= ?", userID, videoIDs, since).
		Find(&models).
		Error
	if err != nil {
		return nil, err
	}
	exposures := make([]*domainrecommendation.Exposure, 0, len(models))
	for _, model := range models {
		exposures = append(exposures, restoreExposure(model))
	}
	return exposures, nil
}

func (r *Repository) SaveExposures(ctx context.Context, writes []*domainrecommendation.ExposureWrite) ([]*domainrecommendation.Exposure, error) {
	if len(writes) == 0 {
		return []*domainrecommendation.Exposure{}, nil
	}

	exposures := make([]*domainrecommendation.Exposure, 0, len(writes))
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, write := range writes {
			if write == nil {
				continue
			}
			if err := ensurePublishedVideo(tx, write.VideoID); err != nil {
				return err
			}
			event := infraexposure.ViewEventModel{
				UserID:    write.UserID,
				VideoID:   write.VideoID,
				Scene:     write.Scene,
				RequestID: stringPtr(write.RequestID),
				EventType: domainexposure.EventTypeExposed,
				WatchMs:   0,
				Completed: false,
			}
			if err := tx.Create(&event).Error; err != nil {
				return err
			}
			model := infraexposure.ExposureModel{
				UserID:         write.UserID,
				VideoID:        write.VideoID,
				FirstExposedAt: event.CreatedAt,
				LastExposedAt:  event.CreatedAt,
				ExposureCount:  1,
				LastScene:      write.Scene,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "user_id"},
					{Name: "video_id"},
				},
				DoUpdates: clause.Assignments(map[string]any{
					"last_exposed_at": gorm.Expr("EXCLUDED.last_exposed_at"),
					"exposure_count":  gorm.Expr("exposures.exposure_count + 1"),
					"last_scene":      gorm.Expr("EXCLUDED.last_scene"),
					"updated_at":      gorm.Expr("EXCLUDED.updated_at"),
				}),
			}).Create(&model).Error; err != nil {
				return err
			}

			var saved infraexposure.ExposureModel
			if err := tx.Where("user_id = ? AND video_id = ?", write.UserID, write.VideoID).Take(&saved).Error; err != nil {
				return err
			}
			exposures = append(exposures, restoreExposure(saved))
		}
		return nil
	})
	if err != nil {
		return nil, mapRecommendationError(err)
	}
	return exposures, nil
}

func (r *Repository) SaveFeedback(ctx context.Context, feedback *domainrecommendation.Feedback) (*domainrecommendation.Feedback, bool, error) {
	if feedback == nil {
		return nil, false, domainrecommendation.ErrInvalidFeedbackType
	}
	existing, err := r.FindFeedbackByUserAndIdempotencyKey(ctx, feedback.UserID, feedback.IdempotencyKey)
	if err == nil {
		return replayFeedback(feedback, existing)
	}
	if !errors.Is(err, domainrecommendation.ErrFeedbackNotFound) {
		return nil, false, err
	}
	video, err := r.GetFeedbackVideo(ctx, feedback.VideoID)
	if err != nil {
		return nil, false, err
	}

	model := FeedbackModel{
		UserID:               feedback.UserID,
		VideoID:              feedback.VideoID,
		RequestID:            feedback.RequestID,
		FeedbackType:         feedback.FeedbackType,
		IdempotencyKey:       feedback.IdempotencyKey,
		SuppressionScope:     feedback.SuppressionScope,
		SuppressionScopeID:   feedback.SuppressionScopeID,
		SuppressionExpiresAt: feedback.SuppressionExpiresAt,
		CreatedAt:            feedback.CreatedAt,
	}
	if model.SuppressionScope == "" {
		model.SuppressionScope = domainrecommendation.SuppressionScopeVideo
		if feedback.FeedbackType == domainrecommendation.FeedbackTypeReduceAuthor {
			model.SuppressionScope = domainrecommendation.SuppressionScopeAuthor
		}
	}
	if model.SuppressionScopeID <= 0 {
		if model.SuppressionScope == domainrecommendation.SuppressionScopeAuthor {
			model.SuppressionScopeID = video.AuthorID
		} else {
			model.SuppressionScopeID = feedback.VideoID
		}
	}
	if model.SuppressionExpiresAt.IsZero() {
		model.SuppressionExpiresAt = feedback.CreatedAt.Add(24 * time.Hour)
	}
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureFeedbackVideo(tx, feedback.VideoID); err != nil {
			return err
		}
		if err := tx.Create(&model).Error; err != nil {
			return err
		}
		if err := tx.Create(&FeedbackProfileOutboxModel{
			FeedbackID:  model.ID,
			AvailableAt: model.CreatedAt,
		}).Error; err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&OutcomeModel{
			ID: fmt.Sprintf("feedback:%d", model.ID), RequestID: model.RequestID, UserID: model.UserID,
			VideoID: model.VideoID, OutcomeType: model.FeedbackType, OccurredAt: model.CreatedAt, RecordedAt: model.CreatedAt,
		}).Error
	}); err != nil {
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, false, err
		}
		existing, findErr := r.FindFeedbackByUserAndIdempotencyKey(ctx, feedback.UserID, feedback.IdempotencyKey)
		if findErr != nil {
			return nil, false, findErr
		}
		return replayFeedback(feedback, existing)
	}
	return feedbackFromModel(model), false, nil
}

func (r *Repository) FindFeedbackByUserAndIdempotencyKey(ctx context.Context, userID int64, idempotencyKey string) (*domainrecommendation.Feedback, error) {
	if userID <= 0 {
		return nil, domainrecommendation.ErrInvalidUserID
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return nil, domainrecommendation.ErrIdempotencyKeyRequired
	}
	if len(idempotencyKey) > domainrecommendation.MaxIdempotencyKeyLength {
		return nil, domainrecommendation.ErrIdempotencyKeyTooLong
	}
	feedback, err := r.findFeedbackByIdempotencyKey(ctx, userID, idempotencyKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainrecommendation.ErrFeedbackNotFound
	}
	return feedback, err
}

func (r *Repository) GetFeedbackVideo(ctx context.Context, videoID int64) (*applicationrecommendation.FeedbackVideo, error) {
	if videoID <= 0 {
		return nil, domainrecommendation.ErrVideoNotFound
	}
	var video struct {
		ID       int64
		AuthorID int64
	}
	err := r.db.WithContext(ctx).Table("video").Select("id, author_id").Where("id = ?", videoID).Take(&video).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domainrecommendation.ErrVideoNotFound
	}
	if err != nil {
		return nil, err
	}
	if video.AuthorID <= 0 {
		return nil, domainrecommendation.ErrVideoNotFound
	}
	return &applicationrecommendation.FeedbackVideo{VideoID: video.ID, AuthorID: video.AuthorID}, nil
}

func (r *Repository) SaveServedCandidateEvidence(ctx context.Context, evidence *domainrecommendation.ServedCandidateEvidence) (bool, error) {
	normalized, err := normalizeServedCandidateEvidence(evidence)
	if err != nil {
		return false, err
	}
	if len(normalized.Candidates) == 0 {
		return false, nil
	}

	var replayed bool
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockServedCandidateEvidenceRequest(tx, normalized.UserID, normalized.RequestID); err != nil {
			return err
		}
		var existing []ServedCandidateEvidenceModel
		if err := tx.Where("user_id = ? AND request_id = ?", normalized.UserID, normalized.RequestID).
			Order("position ASC, video_id ASC").
			Find(&existing).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		if hasRetainedServedCandidateEvidence(existing, now) {
			firstPage := firstPageServedCandidateEvidence(existing)
			if len(firstPage) == 0 || !servedCandidateEvidenceMatches(firstPage, normalized) {
				return domainrecommendation.ErrServedCandidateEvidenceConflict
			}
			if !hasActiveServedCandidateEvidence(existing, now) {
				// Do not replace a just-expired request during delivery grace:
				// delayed outcomes still need its original served interval.
				return domainrecommendation.ErrServedCandidateEvidenceConflict
			}
			replayed = true
			return nil
		}
		if len(existing) > 0 {
			if err := tx.Where("user_id = ? AND request_id = ?", normalized.UserID, normalized.RequestID).
				Delete(&ServedCandidateEvidenceModel{}).Error; err != nil {
				return err
			}
		}
		return tx.Create(servedCandidateEvidenceModels(normalized, servedEvidenceKindFirstPage, normalized.ExpiresAt)).Error
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return false, domainrecommendation.ErrServedCandidateEvidenceConflict
	}
	return replayed, err
}

// AppendServedCandidateEvidence records candidates from a later delivered
// page without changing the first-page evidence. It shares the request's
// original expiry, so repeated cursor pages cannot extend attribution forever.
func (r *Repository) AppendServedCandidateEvidence(ctx context.Context, evidence *domainrecommendation.ServedCandidateEvidence) (bool, error) {
	normalized, err := normalizeServedCandidateEvidence(evidence)
	if err != nil {
		return false, err
	}
	if len(normalized.Candidates) == 0 {
		return true, nil
	}

	var replayed bool
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockServedCandidateEvidenceRequest(tx, normalized.UserID, normalized.RequestID); err != nil {
			return err
		}
		var existing []ServedCandidateEvidenceModel
		if err := tx.Where("user_id = ? AND request_id = ?", normalized.UserID, normalized.RequestID).
			Order("position ASC, video_id ASC").Find(&existing).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		retained := hasRetainedServedCandidateEvidence(existing, now)
		active := hasActiveServedCandidateEvidence(existing, now)
		if !retained && len(existing) > 0 {
			if err := tx.Where("user_id = ? AND request_id = ?", normalized.UserID, normalized.RequestID).
				Delete(&ServedCandidateEvidenceModel{}).Error; err != nil {
				return err
			}
			existing = nil
		}
		if retained && !active {
			return domainrecommendation.ErrServedCandidateEvidenceConflict
		}
		existingVideoIDs := make(map[int64]struct{}, len(existing))
		expiresAt := normalized.ExpiresAt
		for _, model := range existing {
			if model.ExpiresAt.After(now) {
				existingVideoIDs[model.VideoID] = struct{}{}
				if model.ExpiresAt.Before(expiresAt) {
					expiresAt = model.ExpiresAt
				}
			}
		}
		additions := make([]domainrecommendation.ServedCandidateEvidenceItem, 0, len(normalized.Candidates))
		for _, candidate := range normalized.Candidates {
			if _, exists := existingVideoIDs[candidate.VideoID]; exists {
				continue
			}
			existingVideoIDs[candidate.VideoID] = struct{}{}
			additions = append(additions, candidate)
		}
		if len(existingVideoIDs) > domainrecommendation.MaxServedCandidateEvidence {
			return domainrecommendation.ErrInvalidServedCandidateEvidence
		}
		if len(additions) == 0 {
			replayed = true
			return nil
		}
		appended := *normalized
		appended.Candidates = additions
		return tx.Create(servedCandidateEvidenceModels(&appended, servedEvidenceKindDegradedPage, expiresAt)).Error
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return false, domainrecommendation.ErrServedCandidateEvidenceConflict
	}
	return replayed, err
}

func (r *Repository) HasServedCandidateEvidence(ctx context.Context, userID int64, requestID string, videoID int64, recordedAt time.Time) (bool, error) {
	if userID <= 0 || videoID <= 0 || strings.TrimSpace(requestID) == "" || recordedAt.IsZero() {
		return false, nil
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&ServedCandidateEvidenceModel{}).
		Where("user_id = ? AND request_id = ? AND video_id = ? AND served_at <= ? AND expires_at > ?",
			userID, strings.TrimSpace(requestID), videoID, recordedAt.UTC(), recordedAt.UTC()).
		Count(&count).Error
	return count > 0, err
}

func normalizeServedCandidateEvidence(evidence *domainrecommendation.ServedCandidateEvidence) (*domainrecommendation.ServedCandidateEvidence, error) {
	if evidence == nil {
		return nil, domainrecommendation.ErrInvalidServedCandidateEvidence
	}
	return domainrecommendation.NewServedCandidateEvidence(domainrecommendation.ServedCandidateEvidenceInput{
		UserID: evidence.UserID, RequestID: evidence.RequestID, Scene: evidence.Scene, PolicyVersion: evidence.PolicyVersion,
		ServedAt: evidence.ServedAt, ExpiresAt: evidence.ExpiresAt, Candidates: evidence.Candidates,
	})
}

func lockServedCandidateEvidenceRequest(tx *gorm.DB, userID int64, requestID string) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", fmt.Sprintf("%d|%s", userID, requestID)).Error
}

func hasActiveServedCandidateEvidence(models []ServedCandidateEvidenceModel, now time.Time) bool {
	for _, model := range models {
		if model.ExpiresAt.After(now) {
			return true
		}
	}
	return false
}

func hasRetainedServedCandidateEvidence(models []ServedCandidateEvidenceModel, now time.Time) bool {
	for _, model := range models {
		if model.ExpiresAt.Add(domainrecommendation.ServedCandidateEvidenceDeliveryGrace).After(now) {
			return true
		}
	}
	return false
}

func firstPageServedCandidateEvidence(models []ServedCandidateEvidenceModel) []ServedCandidateEvidenceModel {
	firstPage := make([]ServedCandidateEvidenceModel, 0, len(models))
	for _, model := range models {
		// Empty is the migration-safe interpretation of rows created before
		// evidence_kind was introduced.
		if model.EvidenceKind == "" || model.EvidenceKind == servedEvidenceKindFirstPage {
			firstPage = append(firstPage, model)
		}
	}
	return firstPage
}

func servedCandidateEvidenceModels(evidence *domainrecommendation.ServedCandidateEvidence, kind string, expiresAt time.Time) []ServedCandidateEvidenceModel {
	models := make([]ServedCandidateEvidenceModel, 0, len(evidence.Candidates))
	for _, candidate := range evidence.Candidates {
		models = append(models, ServedCandidateEvidenceModel{
			UserID: evidence.UserID, RequestID: evidence.RequestID, VideoID: candidate.VideoID, EvidenceKind: kind,
			PolicyVersion: evidence.PolicyVersion, Position: candidate.Position,
			ServedAt: evidence.ServedAt, ExpiresAt: expiresAt.UTC(),
		})
	}
	return models
}

func servedCandidateEvidenceMatches(models []ServedCandidateEvidenceModel, evidence *domainrecommendation.ServedCandidateEvidence) bool {
	if evidence == nil || len(models) != len(evidence.Candidates) {
		return false
	}
	for index, candidate := range evidence.Candidates {
		model := models[index]
		if model.UserID != evidence.UserID || model.RequestID != evidence.RequestID ||
			model.VideoID != candidate.VideoID || model.PolicyVersion != evidence.PolicyVersion ||
			model.Position != candidate.Position {
			return false
		}
	}
	return true
}

// VerifyAndSaveOutcome treats attribution supplied with interaction and follow
// requests as untrusted until it matches a durable recommendation request.
// Invalid attribution is skipped without affecting the accepted source fact.
func (r *Repository) VerifyAndSaveOutcome(ctx context.Context, outcome *domainrecommendation.Outcome, followedTargetUserID int64) (bool, bool, error) {
	if outcome == nil {
		return false, false, domainrecommendation.ErrInvalidRequestLog
	}
	valid, err := r.HasServedCandidateEvidence(ctx, outcome.UserID, outcome.RequestID, outcome.VideoID, outcome.RecordedAt)
	if err != nil {
		return false, false, err
	}
	if !valid {
		if domainrecommendation.OutcomeAttributionPending(outcome.RecordedAt, time.Now().UTC()) {
			return false, false, domainrecommendation.ErrOutcomeAttributionPending
		}
		return false, false, nil
	}
	if outcome.OutcomeType == "follow" {
		if followedTargetUserID <= 0 {
			return false, false, nil
		}
		video, err := r.GetFeedbackVideo(ctx, outcome.VideoID)
		if errors.Is(err, domainrecommendation.ErrVideoNotFound) {
			return false, false, nil
		}
		if err != nil {
			return false, false, err
		}
		if video == nil || video.AuthorID != followedTargetUserID {
			return false, false, nil
		}
	}
	recorded, err := r.SaveOutcome(ctx, outcome)
	return recorded, true, err
}

func (r *Repository) SaveOutcome(ctx context.Context, outcome *domainrecommendation.Outcome) (bool, error) {
	if outcome == nil {
		return false, domainrecommendation.ErrInvalidRequestLog
	}
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&OutcomeModel{
		ID: outcome.ID, RequestID: outcome.RequestID, UserID: outcome.UserID, VideoID: outcome.VideoID,
		OutcomeType: outcome.OutcomeType, OccurredAt: outcome.OccurredAt, RecordedAt: outcome.RecordedAt,
	})
	return result.RowsAffected > 0, result.Error
}

func (r *Repository) CreatePolicy(ctx context.Context, policy *domainrecommendation.Policy) (*domainrecommendation.Policy, error) {
	if policy == nil {
		return nil, domainrecommendation.ErrInvalidPolicyConfiguration
	}
	createdAt := policy.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	normalized, err := domainrecommendation.NewPolicy(policy.Scene, policy.Version, policy.Enabled, policy.Config, createdAt)
	if err != nil {
		return nil, err
	}
	configJSON, err := json.Marshal(normalized.Config)
	if err != nil {
		return nil, domainrecommendation.ErrInvalidPolicyConfiguration
	}
	model := PolicyModel{
		Scene: normalized.Scene, Version: normalized.Version, Enabled: normalized.Enabled, ConfigJSON: string(configJSON),
		CreatedAt: normalized.CreatedAt, UpdatedAt: normalized.UpdatedAt,
	}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, domainrecommendation.ErrInvalidPolicyVersion
		}
		return nil, err
	}
	return policyFromModel(model)
}

func (r *Repository) ActivatePolicy(ctx context.Context, scene string, version int) (*domainrecommendation.Policy, error) {
	scene = strings.ToLower(strings.TrimSpace(scene))
	if scene == "" || version <= 0 {
		return nil, domainrecommendation.ErrInvalidPolicyVersion
	}
	var activated PolicyModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("scene = ? AND version = ?", scene, version).Take(&activated).Error; err != nil {
			return err
		}
		if _, err := policyFromModel(activated); err != nil {
			return err
		}
		if err := tx.Model(&PolicyModel{}).Where("id = ?", activated.ID).Update("enabled", true).Error; err != nil {
			return err
		}
		activated.Enabled = true
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainrecommendation.ErrPolicyNotFound
		}
		return nil, err
	}
	return policyFromModel(activated)
}

func (r *Repository) RollbackPolicy(ctx context.Context, scene string, version int) (*domainrecommendation.Policy, error) {
	scene = strings.ToLower(strings.TrimSpace(scene))
	if scene == "" || version <= 0 {
		return nil, domainrecommendation.ErrInvalidPolicyVersion
	}
	var target PolicyModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("scene = ? AND version = ?", scene, version).Take(&target).Error; err != nil {
			return err
		}
		policy, err := policyFromModel(target)
		if err != nil {
			return err
		}
		policy.Config.RolloutPercentage = 100
		configJSON, err := json.Marshal(policy.Config)
		if err != nil {
			return err
		}
		if err := tx.Model(&PolicyModel{}).Where("scene = ?", scene).Updates(map[string]any{"enabled": false}).Error; err != nil {
			return err
		}
		if err := tx.Model(&PolicyModel{}).Where("id = ?", target.ID).Updates(map[string]any{
			"enabled": true, "config_json": string(configJSON),
		}).Error; err != nil {
			return err
		}
		target.Enabled = true
		target.ConfigJSON = string(configJSON)
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainrecommendation.ErrPolicyNotFound
		}
		return nil, err
	}
	return policyFromModel(target)
}

func (r *Repository) ListEnabledPolicies(ctx context.Context, scene string) ([]*domainrecommendation.Policy, error) {
	return r.listPolicies(ctx, scene, true)
}

func (r *Repository) ListPolicies(ctx context.Context, scene string) ([]*domainrecommendation.Policy, error) {
	return r.listPolicies(ctx, scene, false)
}

func (r *Repository) listPolicies(ctx context.Context, scene string, enabledOnly bool) ([]*domainrecommendation.Policy, error) {
	scene = strings.ToLower(strings.TrimSpace(scene))
	if scene == "" {
		return nil, domainrecommendation.ErrEmptyScene
	}
	var models []PolicyModel
	query := r.db.WithContext(ctx).Where("scene = ?", scene)
	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}
	if err := query.Order("version DESC").Find(&models).Error; err != nil {
		return nil, err
	}
	policies := make([]*domainrecommendation.Policy, 0, len(models))
	for _, model := range models {
		policy, err := policyFromModel(model)
		if err != nil {
			return nil, err
		}
		policies = append(policies, policy)
	}
	return policies, nil
}

func (r *Repository) ApplyProfileEvent(ctx context.Context, event *domainrecommendation.ProfileEvent) (*domainrecommendation.UserInterestProfile, bool, error) {
	if event == nil {
		return nil, false, domainrecommendation.ErrInvalidProfileEvent
	}
	var output *domainrecommendation.UserInterestProfile
	applied := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", event.UserID).Error; err != nil {
			return err
		}
		appliedEvent := AppliedProfileEventModel{
			UserID: event.UserID, SourceEventID: event.SourceEventID, PayloadHash: event.PayloadHash, AppliedAt: event.OccurredAt,
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&appliedEvent)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var existing AppliedProfileEventModel
			if err := tx.Where("user_id = ? AND source_event_id = ?", event.UserID, event.SourceEventID).Take(&existing).Error; err != nil {
				return err
			}
			if existing.PayloadHash != event.PayloadHash {
				return domainrecommendation.ErrProfileEventConflict
			}
			var model UserInterestProfileModel
			if err := tx.Where("user_id = ?", event.UserID).Take(&model).Error; err != nil {
				return err
			}
			profile, err := profileFromModel(model)
			if err != nil {
				return err
			}
			output = profile
			return nil
		}

		var model UserInterestProfileModel
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", event.UserID).Take(&model).Error
		var profile *domainrecommendation.UserInterestProfile
		switch {
		case err == nil:
			profile, err = profileFromModel(model)
			if err != nil {
				return err
			}
		case errors.Is(err, gorm.ErrRecordNotFound):
			var reconstructed bool
			var bootstrapApplied []AppliedProfileEventModel
			profile, reconstructed, bootstrapApplied, err = rebuildUserInterestProfile(ctx, tx, event.UserID, event.OccurredAt)
			if err != nil {
				return err
			}
			if !reconstructed {
				profile = domainrecommendation.EmptyUserInterestProfile(event.UserID, event.OccurredAt)
			}
			if err := persistBootstrapAppliedProfileEvents(tx, bootstrapApplied); err != nil {
				return err
			}
		default:
			return err
		}
		profile, err = profile.ApplyWithDecay(event, event.Decay)
		if err != nil {
			return err
		}
		updated, err := profileToModel(profile)
		if err != nil {
			return err
		}
		if err := tx.Save(&updated).Error; err != nil {
			return err
		}
		output = profile
		applied = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return output, applied, nil
}

func (r *Repository) SaveRequestLog(ctx context.Context, log *domainrecommendation.RecommendationRequestLog) (*domainrecommendation.RecommendationRequestLog, bool, error) {
	if log == nil || strings.ToLower(strings.TrimSpace(log.Scene)) != domainrecommendation.RecommendationRequestLogScene {
		return nil, false, domainrecommendation.ErrInvalidRequestLog
	}
	payload, err := log.CompactPayload()
	if err != nil {
		return nil, false, err
	}
	model := RequestLogModel{
		RequestID: log.RequestID, UserID: log.UserID, Scene: log.Scene, PolicyVersion: log.PolicyVersion,
		PayloadJSON: string(payload), CreatedAt: log.CreatedAt,
	}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, false, err
		}
		var existing RequestLogModel
		if findErr := r.db.WithContext(ctx).
			Where("user_id = ? AND request_id = ?", log.UserID, log.RequestID).
			Take(&existing).Error; findErr != nil {
			return nil, false, findErr
		}
		saved, restoreErr := requestLogFromModel(existing)
		if restoreErr != nil {
			return nil, false, restoreErr
		}
		if saved.UserID != log.UserID || saved.Scene != log.Scene || saved.PolicyVersion != log.PolicyVersion || saved.Degraded != log.Degraded ||
			stringPayload(saved) != stringPayload(log) {
			return nil, false, domainrecommendation.ErrRequestLogConflict
		}
		return saved, true, nil
	}
	saved, err := requestLogFromModel(model)
	return saved, false, err
}

func (r *Repository) DeleteRequestLogsBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if cutoff.IsZero() || limit <= 0 {
		return 0, domainrecommendation.ErrInvalidRequestLog
	}
	var ids []int64
	if err := r.db.WithContext(ctx).Model(&RequestLogModel{}).Select("id").
		Where("created_at < ?", cutoff.UTC()).Order("created_at ASC, id ASC").Limit(limit).Find(&ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).Where("id IN ?", ids).Delete(&RequestLogModel{})
	return result.RowsAffected, result.Error
}

func (r *Repository) DeleteRequestLogsForPolicyBefore(ctx context.Context, scene string, policyVersion int, cutoff time.Time, limit int) (int64, error) {
	if strings.TrimSpace(scene) == "" || policyVersion <= 0 || cutoff.IsZero() || limit <= 0 {
		return 0, domainrecommendation.ErrInvalidRequestLog
	}
	var ids []int64
	if err := r.db.WithContext(ctx).Model(&RequestLogModel{}).Select("id").
		Where("scene = ? AND policy_version = ? AND created_at < ?", strings.ToLower(strings.TrimSpace(scene)), policyVersion, cutoff.UTC()).
		Order("created_at ASC, id ASC").Limit(limit).Find(&ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).Where("id IN ?", ids).Delete(&RequestLogModel{})
	return result.RowsAffected, result.Error
}

func (r *Repository) DeleteServedCandidateEvidenceBefore(ctx context.Context, cutoff time.Time, requestLimit int) (domainrecommendation.ServedCandidateEvidenceCleanupResult, error) {
	if cutoff.IsZero() || requestLimit <= 0 {
		return domainrecommendation.ServedCandidateEvidenceCleanupResult{}, domainrecommendation.ErrInvalidServedCandidateEvidence
	}
	type requestIdentity struct {
		UserID    int64
		RequestID string
	}
	result := domainrecommendation.ServedCandidateEvidenceCleanupResult{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var identities []requestIdentity
		if err := tx.Model(&ServedCandidateEvidenceModel{}).
			Select("user_id, request_id").
			Where("expires_at < ?", cutoff.UTC()).
			Group("user_id, request_id").
			Having("MAX(expires_at) < ?", cutoff.UTC()).
			Order("MAX(expires_at) ASC, user_id ASC, request_id ASC").
			Limit(requestLimit).
			Scan(&identities).Error; err != nil {
			return err
		}
		for _, identity := range identities {
			if err := lockServedCandidateEvidenceRequest(tx, identity.UserID, identity.RequestID); err != nil {
				return err
			}
			// Recheck while holding the same request lock used by writers so an
			// append or fresh reuse cannot be partly removed.
			var active int64
			if err := tx.Model(&ServedCandidateEvidenceModel{}).
				Where("user_id = ? AND request_id = ? AND expires_at >= ?", identity.UserID, identity.RequestID, cutoff.UTC()).
				Count(&active).Error; err != nil {
				return err
			}
			if active > 0 {
				continue
			}
			deletion := tx.Where("user_id = ? AND request_id = ?", identity.UserID, identity.RequestID).
				Delete(&ServedCandidateEvidenceModel{})
			if deletion.Error != nil {
				return deletion.Error
			}
			result.RequestGroups++
			result.CandidateRows += deletion.RowsAffected
		}
		return nil
	})
	return result, err
}

func policyFromModel(model PolicyModel) (*domainrecommendation.Policy, error) {
	var config domainrecommendation.PolicyConfiguration
	if err := json.Unmarshal([]byte(model.ConfigJSON), &config); err != nil {
		return nil, fmt.Errorf("%w: decode policy configuration", domainrecommendation.ErrInvalidPolicyConfiguration)
	}
	policy := domainrecommendation.RestorePolicy(model.ID, model.Scene, model.Version, model.Enabled, config, model.CreatedAt, model.UpdatedAt)
	if policy == nil {
		return nil, domainrecommendation.ErrInvalidPolicyConfiguration
	}
	return policy, nil
}

func profileToModel(profile *domainrecommendation.UserInterestProfile) (UserInterestProfileModel, error) {
	if profile == nil {
		return UserInterestProfileModel{}, domainrecommendation.ErrInvalidProfileEvent
	}
	longTerm, err := json.Marshal(profile.LongTermVector)
	if err != nil {
		return UserInterestProfileModel{}, err
	}
	recent, err := json.Marshal(profile.RecentVector)
	if err != nil {
		return UserInterestProfileModel{}, err
	}
	authors, err := json.Marshal(profile.AuthorAffinities)
	if err != nil {
		return UserInterestProfileModel{}, err
	}
	negativeTopics, err := json.Marshal(profile.NegativeTopicVector)
	if err != nil {
		return UserInterestProfileModel{}, err
	}
	negativeAuthors, err := json.Marshal(profile.NegativeAuthorAffinities)
	if err != nil {
		return UserInterestProfileModel{}, err
	}
	return UserInterestProfileModel{
		UserID: profile.UserID, LongTermVectorJSON: string(longTerm), RecentVectorJSON: string(recent),
		AuthorAffinitiesJSON: string(authors), NegativeTopicVectorJSON: string(negativeTopics),
		NegativeAuthorAffinitiesJSON: string(negativeAuthors), Version: profile.Version, UpdatedAt: profile.UpdatedAt,
	}, nil
}

func profileFromModel(model UserInterestProfileModel) (*domainrecommendation.UserInterestProfile, error) {
	var longTerm, recent, negativeTopics []float64
	var authors, negativeAuthors map[int64]float64
	if err := json.Unmarshal([]byte(model.LongTermVectorJSON), &longTerm); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(model.RecentVectorJSON), &recent); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(model.AuthorAffinitiesJSON), &authors); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(model.NegativeTopicVectorJSON), &negativeTopics); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(model.NegativeAuthorAffinitiesJSON), &negativeAuthors); err != nil {
		return nil, err
	}
	profile := domainrecommendation.RestoreUserInterestProfile(model.UserID, longTerm, recent, authors, negativeTopics, negativeAuthors, model.Version, model.UpdatedAt)
	if profile == nil {
		return nil, domainrecommendation.ErrInvalidProfileEvent
	}
	return profile, nil
}

func requestLogFromModel(model RequestLogModel) (*domainrecommendation.RecommendationRequestLog, error) {
	var payload struct {
		Context           *domainrecommendation.RecommendationContext `json:"context"`
		Candidates        []domainrecommendation.LoggedCandidate      `json:"candidates"`
		Degraded          bool                                        `json:"degraded"`
		Snapshot          bool                                        `json:"snapshot"`
		DegradedProviders []string                                    `json:"degraded_providers"`
		RecallDiagnostics []domainrecommendation.RecallDiagnostic     `json:"recall_diagnostics"`
	}
	if err := json.Unmarshal([]byte(model.PayloadJSON), &payload); err != nil {
		return nil, err
	}
	return domainrecommendation.NewRecommendationRequestLog(domainrecommendation.RequestLogInput{
		RequestID: model.RequestID, UserID: model.UserID, Scene: model.Scene, PolicyVersion: model.PolicyVersion,
		Context: payload.Context, Candidates: payload.Candidates, Degraded: payload.Degraded, Snapshot: payload.Snapshot,
		DegradedProviders: payload.DegradedProviders, RecallDiagnostics: payload.RecallDiagnostics, CreatedAt: model.CreatedAt,
	})
}

func stringPayload(log *domainrecommendation.RecommendationRequestLog) string {
	payload, err := log.CompactPayload()
	if err != nil {
		return ""
	}
	return string(payload)
}
func (r *Repository) findFeedbackByIdempotencyKey(ctx context.Context, userID int64, idempotencyKey string) (*domainrecommendation.Feedback, error) {
	var model FeedbackModel
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND idempotency_key = ?", userID, idempotencyKey).
		Take(&model).Error; err != nil {
		return nil, err
	}
	return feedbackFromModel(model), nil
}

func replayFeedback(input *domainrecommendation.Feedback, existing *domainrecommendation.Feedback) (*domainrecommendation.Feedback, bool, error) {
	if !existing.SameNormalizedPayload(input) {
		return nil, false, domainrecommendation.ErrFeedbackIdempotencyConflict
	}
	return existing, true, nil
}

func feedbackFromModel(model FeedbackModel) *domainrecommendation.Feedback {
	feedback := domainrecommendation.RestoreFeedback(
		model.ID,
		model.UserID,
		model.VideoID,
		model.RequestID,
		model.FeedbackType,
		model.IdempotencyKey,
		model.CreatedAt,
	)
	if feedback != nil {
		_ = feedback.SetSuppression(model.SuppressionScope, model.SuppressionScopeID, model.SuppressionExpiresAt)
	}
	return feedback
}

func decodeVector(content string) ([]float64, error) {
	var vector []float64
	if err := json.Unmarshal([]byte(content), &vector); err != nil {
		return nil, err
	}
	return vector, nil
}

func positiveEventTypes() []string {
	return []string{
		domainexposure.EventTypePlay,
		domainexposure.EventTypeProgress,
		domainexposure.EventTypeComplete,
	}
}

func eventWeight(eventType string, positionMs int, watchMs int, durationMs *int, completed bool) float64 {
	switch eventType {
	case domainexposure.EventTypeComplete:
		return 3
	case domainexposure.EventTypeProgress:
		progress := float64(maxInt(positionMs, watchMs)) / 30000
		if durationMs != nil && *durationMs > 0 {
			progress = float64(positionMs) / float64(*durationMs)
		}
		if progress < 0.25 {
			progress = 0.25
		}
		if progress > 1.5 {
			progress = 1.5
		}
		return progress
	case domainexposure.EventTypePlay:
		weight := 1 + float64(watchMs)/30000
		if weight > 2 {
			weight = 2
		}
		if completed {
			weight += 1
		}
		return weight
	default:
		return 1
	}
}

func ensurePublishedVideo(tx *gorm.DB, videoID int64) error {
	var item struct {
		ID int64
	}
	err := tx.Table("video").
		Select("id").
		Where("id = ? AND status = ? AND visibility = ? AND media_status IN ?", videoID, domainvideo.StatusPublished, domainvideo.VisibilityPublic, []string{domainmedia.MediaStatusLegacyReady, domainmedia.MediaStatusReady}).
		Take(&item).
		Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domainrecommendation.ErrVideoNotFound
		}
		return err
	}
	return nil
}

func ensureFeedbackVideo(tx *gorm.DB, videoID int64) error {
	var item struct {
		ID int64
	}
	err := tx.Table("video").Select("id").Where("id = ?", videoID).Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domainrecommendation.ErrVideoNotFound
	}
	return err
}

func restoreExposure(model infraexposure.ExposureModel) *domainrecommendation.Exposure {
	return domainrecommendation.RestoreExposure(
		model.ID,
		model.UserID,
		model.VideoID,
		model.FirstExposedAt,
		model.LastExposedAt,
		model.ExposureCount,
		model.LastScene,
	)
}

func mapRecommendationError(err error) error {
	if errors.Is(err, domainrecommendation.ErrVideoNotFound) {
		return err
	}
	return err
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func int64Ptr(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func recordedAtFromViewEvent(event *applicationexposure.ViewEventRecordedEvent) time.Time {
	if event != nil && !event.RecordedAt.IsZero() {
		return event.RecordedAt.UTC()
	}
	return time.Now().UTC()
}

func behaviorRecordedAt(event BehaviorEventModel) time.Time {
	if !event.RecordedAt.IsZero() {
		return event.RecordedAt.UTC()
	}
	if !event.CreatedAt.IsZero() {
		return event.CreatedAt.UTC()
	}
	return time.Now().UTC()
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func truncateBehaviorProfileError(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) > 1024 {
		return reason[:1024]
	}
	return reason
}

func EnsureBehaviorEvents(db *gorm.DB) error {
	return db.Exec(`
		INSERT INTO recommendation_behavior_event (
			event_id, view_event_id, user_id, video_id, event_type, playback_session_id,
			sequence, position_ms, watch_ms, duration_ms, completed, occurred_at, recorded_at, created_at
		)
		SELECT event_id, id, user_id, video_id, event_type, playback_session_id,
			sequence, position_ms, watch_ms, duration_ms, completed, occurred_at, created_at, NOW()
		FROM video_view_events
		WHERE event_type IN ('play', 'progress', 'complete')
		ON CONFLICT (user_id, event_id) DO NOTHING
	`).Error
}
