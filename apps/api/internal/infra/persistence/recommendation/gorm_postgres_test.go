package infrarecommendation

import (
	applicationexposure "github.com/shiyudesu/frux/internal/application/exposure"
	domainembedding "github.com/shiyudesu/frux/internal/domain/embedding"
	domainexposure "github.com/shiyudesu/frux/internal/domain/exposure"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPolicyProfileAndRequestLogRepositoryPostgreSQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set; skipping real PostgreSQL integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schema := fmt.Sprintf("frux_recommendation_policy_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		_ = admin.Close()
	})
	sqlDB, err := sql.Open("pgx", recommendationPostgresDSNWithSchema(dsn, schema))
	if err != nil {
		t.Fatalf("open schema PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open GORM: %v", err)
	}
	if err := db.AutoMigrate(&PolicyModel{}, &UserInterestProfileModel{}, &AppliedProfileEventModel{}, &BehaviorEventModel{}, &RequestLogModel{}, &ServedCandidateEvidenceModel{}, &FeedbackModel{}, &FeedbackProfileOutboxModel{}); err != nil {
		t.Fatalf("migrate recommendation storage: %v", err)
	}
	prepareProfileFactTables(t, db)

	now := time.Now().UTC().Truncate(time.Microsecond)
	repo := New(db)
	baseline, err := domainrecommendation.NewPolicy("feed", 1, true, postgresPolicyConfig(100), now)
	if err != nil {
		t.Fatalf("new baseline policy: %v", err)
	}
	canary, err := domainrecommendation.NewPolicy("feed", 2, false, postgresPolicyConfig(25), now)
	if err != nil {
		t.Fatalf("new canary policy: %v", err)
	}
	if _, err := repo.CreatePolicy(context.Background(), baseline); err != nil {
		t.Fatalf("save baseline policy: %v", err)
	}
	if _, err := repo.CreatePolicy(context.Background(), canary); err != nil {
		t.Fatalf("save canary policy: %v", err)
	}
	if _, err := repo.ActivatePolicy(context.Background(), "feed", 2); err != nil {
		t.Fatalf("activate canary policy: %v", err)
	}
	policies, err := repo.ListEnabledPolicies(context.Background(), "feed")
	if err != nil || len(policies) != 2 {
		t.Fatalf("enabled policies = %#v, %v", policies, err)
	}
	if selected := domainrecommendation.SelectPolicy(policies, 9, "stable-request"); selected == nil {
		t.Fatal("expected selected staged policy")
	}
	rolledBack, err := repo.RollbackPolicy(context.Background(), "feed", 1)
	if err != nil || !rolledBack.Enabled || rolledBack.Config.RolloutPercentage != 100 {
		t.Fatalf("rollback = %#v, %v", rolledBack, err)
	}
	policies, err = repo.ListEnabledPolicies(context.Background(), "feed")
	if err != nil || len(policies) != 1 || policies[0].Version != 1 {
		t.Fatalf("policies after rollback = %#v, %v", policies, err)
	}
	allPolicies, err := repo.ListPolicies(context.Background(), "feed")
	if err != nil || len(allPolicies) != 2 || allPolicies[0].Version != 2 || allPolicies[0].Enabled || allPolicies[0].Config.RetentionDays != 25 {
		t.Fatalf("persisted policies after rollback = %#v, %v", allPolicies, err)
	}

	event, err := domainrecommendation.NewProfileEvent(domainrecommendation.ProfileEventInput{
		UserID: 9, SourceEventID: "profile-event-1", EventType: "complete", OccurredAt: now,
		LongTermVector: []float64{1, 0}, RecentVector: []float64{0, 1},
		AuthorAffinities: map[int64]float64{3: 0.5}, NegativeAuthorWeights: map[int64]float64{4: 0.2},
	})
	if err != nil {
		t.Fatalf("new profile event: %v", err)
	}
	profile, applied, err := repo.ApplyProfileEvent(context.Background(), event)
	if err != nil || !applied || profile.Version != 1 {
		t.Fatalf("apply profile event = %#v applied=%v err=%v", profile, applied, err)
	}
	if !profile.UpdatedAt.Equal(event.OccurredAt) {
		t.Fatalf("profile materialization time changed in memory: got %s want %s", profile.UpdatedAt, event.OccurredAt)
	}
	var persistedProfile UserInterestProfileModel
	if err := db.Where("user_id = ?", event.UserID).Take(&persistedProfile).Error; err != nil {
		t.Fatalf("load persisted profile timestamp: %v", err)
	}
	if !persistedProfile.UpdatedAt.Equal(event.OccurredAt) {
		t.Fatalf("GORM overwrote profile materialization time: got %s want %s", persistedProfile.UpdatedAt, event.OccurredAt)
	}
	profile, applied, err = repo.ApplyProfileEvent(context.Background(), event)
	if err != nil || applied || profile.Version != 1 {
		t.Fatalf("duplicate profile event = %#v applied=%v err=%v", profile, applied, err)
	}
	if err := db.Exec("INSERT INTO video (id, author_id) VALUES (?, ?)", 101, 17).Error; err != nil {
		t.Fatalf("insert historical profile video: %v", err)
	}
	if err := db.Exec("INSERT INTO video_embedding (video_id, model, embedding_json) VALUES (?, ?, ?)", 101, domainembedding.HashNgramModel, `[1,0]`).Error; err != nil {
		t.Fatalf("insert historical profile vector: %v", err)
	}
	if err := db.Create(&BehaviorEventModel{
		EventID: "historical-interest", ViewEventID: 101, UserID: 10, VideoID: 101,
		EventType: domainexposure.EventTypeComplete, OccurredAt: now.Add(-time.Hour),
	}).Error; err != nil {
		t.Fatalf("insert historical profile behavior: %v", err)
	}
	firstProjection, err := domainrecommendation.NewProfileEvent(domainrecommendation.ProfileEventInput{
		UserID: 10, SourceEventID: "first-projection", EventType: "complete", OccurredAt: now,
		LongTermVector: []float64{0, 1}, RecentVector: []float64{0, 1},
	})
	if err != nil {
		t.Fatalf("new first projected event: %v", err)
	}
	profile, applied, err = repo.ApplyProfileEvent(context.Background(), firstProjection)
	if err != nil || !applied || len(profile.LongTermVector) != 2 || profile.LongTermVector[0] <= 0 || profile.RecentVector[0] <= 0 {
		t.Fatalf("first projection discarded durable historical interest: %#v applied=%v err=%v", profile, applied, err)
	}

	// Pending action/follow/feedback facts must stay out of first-profile
	// reconstruction. Their owning outboxes later apply exactly their stable
	// source events; behavior facts have no such queue and are claimed by the
	// bootstrap transaction itself.
	pendingAt := now.Add(-2 * time.Hour)
	if err := db.Exec("INSERT INTO interaction_action (user_id, video_id, action_type, status, updated_at) VALUES (?, ?, ?, ?, ?)", 11, 101, "LIKE", 1, pendingAt).Error; err != nil {
		t.Fatalf("insert pending action fact: %v", err)
	}
	if err := db.Exec("INSERT INTO interaction_action_event (event_id, user_id, video_id, action_type, active) VALUES (?, ?, ?, ?, ?)", "pending-action", 11, 101, "LIKE", true).Error; err != nil {
		t.Fatalf("insert pending action outbox: %v", err)
	}
	if err := db.Exec("INSERT INTO user_follow (user_id, target_user_id, status, updated_at) VALUES (?, ?, ?, ?)", 11, 33, 1, pendingAt).Error; err != nil {
		t.Fatalf("insert pending follow fact: %v", err)
	}
	if err := db.Exec("INSERT INTO relation_profile_projection_outbox (event_id, user_id, target_user_id) VALUES (?, ?, ?)", "pending-follow", 11, 33).Error; err != nil {
		t.Fatalf("insert pending follow outbox: %v", err)
	}
	if err := db.Create(&FeedbackModel{
		ID: 900, UserID: 11, VideoID: 101, RequestID: "pending-feedback-request",
		FeedbackType: domainrecommendation.FeedbackTypeNotInterested, IdempotencyKey: "pending-feedback",
		SuppressionScope: domainrecommendation.SuppressionScopeVideo, SuppressionScopeID: 101,
		SuppressionExpiresAt: now.Add(24 * time.Hour), CreatedAt: pendingAt,
	}).Error; err != nil {
		t.Fatalf("insert pending feedback fact: %v", err)
	}
	if err := db.Create(&FeedbackProfileOutboxModel{FeedbackID: 900, AvailableAt: pendingAt}).Error; err != nil {
		t.Fatalf("insert pending feedback outbox: %v", err)
	}
	if err := db.Create(&BehaviorEventModel{
		EventID: "bootstrap-behavior", ViewEventID: 102, UserID: 11, VideoID: 101,
		EventType: domainexposure.EventTypeComplete, OccurredAt: pendingAt,
	}).Error; err != nil {
		t.Fatalf("insert bootstrap behavior fact: %v", err)
	}
	firstPendingProjection, err := domainrecommendation.NewProfileEvent(domainrecommendation.ProfileEventInput{
		UserID: 11, SourceEventID: "first-pending-projection", EventType: "complete", OccurredAt: now,
		LongTermVector: []float64{0, 1}, RecentVector: []float64{0, 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, applied, err = repo.ApplyProfileEvent(context.Background(), firstPendingProjection)
	if err != nil || !applied || profile.Version != 2 || profile.LongTermVector[0] <= 0 || profile.LongTermVector[0] >= 2 {
		t.Fatalf("bootstrap included pending profile facts: profile=%#v applied=%v err=%v", profile, applied, err)
	}
	pendingEvents := []*domainrecommendation.ProfileEvent{}
	for _, input := range []domainrecommendation.ProfileEventInput{
		{
			UserID: 11, SourceEventID: "pending-action", EventType: "LIKE", OccurredAt: pendingAt,
			SourceVideoID: 101, SourceAction: "like", SourceSignal: "true",
			LongTermVector: []float64{1, 0}, RecentVector: []float64{1, 0}, AuthorAffinities: map[int64]float64{17: 1},
		},
		{
			UserID: 11, SourceEventID: "pending-follow", EventType: "follow", OccurredAt: pendingAt,
			SourceAuthorID: 33, SourceAction: "follow", SourceSignal: "true", AuthorAffinities: map[int64]float64{33: .75},
		},
		{
			UserID: 11, SourceEventID: "feedback:900", EventType: domainrecommendation.FeedbackTypeNotInterested, OccurredAt: pendingAt,
			SourceVideoID: 101, SourceAction: domainrecommendation.FeedbackTypeNotInterested, SourceSignal: domainrecommendation.FeedbackTypeNotInterested,
			NegativeTopicVector: []float64{1.5, 0}, NegativeAuthorWeights: map[int64]float64{17: 1.5},
		},
	} {
		event, eventErr := domainrecommendation.NewProfileEvent(input)
		if eventErr != nil {
			t.Fatal(eventErr)
		}
		pendingEvents = append(pendingEvents, event)
	}
	for _, event := range pendingEvents {
		var saved *domainrecommendation.UserInterestProfile
		saved, applied, err = repo.ApplyProfileEvent(context.Background(), event)
		if err != nil || !applied {
			t.Fatalf("pending outbox event did not apply exactly once: source=%s profile=%#v applied=%v err=%v", event.SourceEventID, saved, applied, err)
		}
		profile = saved
	}
	version, longTerm, negative, followAffinity := profile.Version, profile.LongTermVector[0], profile.NegativeTopicVector[0], profile.AuthorAffinities[33]
	for _, event := range pendingEvents {
		saved, duplicate, duplicateErr := repo.ApplyProfileEvent(context.Background(), event)
		if duplicateErr != nil || duplicate || saved.Version != version || saved.LongTermVector[0] != longTerm ||
			saved.NegativeTopicVector[0] != negative || saved.AuthorAffinities[33] != followAffinity {
			t.Fatalf("pending source was applied twice: source=%s profile=%#v applied=%v err=%v", event.SourceEventID, saved, duplicate, duplicateErr)
		}
	}

	for _, feedback := range []FeedbackModel{
		{
			UserID: 12, VideoID: 101, RequestID: "reduce-author-rebuild", FeedbackType: domainrecommendation.FeedbackTypeReduceAuthor,
			IdempotencyKey: "reduce-author-rebuild", SuppressionScope: domainrecommendation.SuppressionScopeAuthor, SuppressionScopeID: 17,
			SuppressionExpiresAt: now.Add(14 * 24 * time.Hour), CreatedAt: pendingAt,
		},
		{
			UserID: 13, VideoID: 101, RequestID: "not-interested-rebuild", FeedbackType: domainrecommendation.FeedbackTypeNotInterested,
			IdempotencyKey: "not-interested-rebuild", SuppressionScope: domainrecommendation.SuppressionScopeVideo, SuppressionScopeID: 101,
			SuppressionExpiresAt: now.Add(14 * 24 * time.Hour), CreatedAt: pendingAt,
		},
	} {
		if err := db.Create(&feedback).Error; err != nil {
			t.Fatalf("insert reconstruction feedback: %v", err)
		}
	}
	rebuild := func(userID int64) *domainrecommendation.UserInterestProfile {
		t.Helper()
		var rebuilt *domainrecommendation.UserInterestProfile
		err := db.Transaction(func(tx *gorm.DB) error {
			var found bool
			var rebuildErr error
			rebuilt, found, _, rebuildErr = rebuildUserInterestProfile(context.Background(), tx, userID, now)
			if rebuildErr != nil {
				return rebuildErr
			}
			if !found {
				t.Fatalf("expected reconstructed profile for user %d", userID)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("rebuild user %d: %v", userID, err)
		}
		return rebuilt
	}
	reducedAuthor := rebuild(12)
	if len(reducedAuthor.NegativeTopicVector) != 0 || reducedAuthor.NegativeAuthorAffinities[17] <= 0 {
		t.Fatalf("reduce-author reconstruction used its video embedding: %#v", reducedAuthor)
	}
	notInterested := rebuild(13)
	if len(notInterested.NegativeTopicVector) != 2 || notInterested.NegativeTopicVector[0] <= 0 ||
		notInterested.NegativeAuthorAffinities[17] <= 0 {
		t.Fatalf("not-interested reconstruction lost negative video/topic feedback: %#v", notInterested)
	}

	for _, item := range []struct {
		feedbackType string
		scope        string
		scopeID      int64
		expiresAt    time.Time
	}{
		{domainrecommendation.FeedbackTypeNotInterested, domainrecommendation.SuppressionScopeVideo, 101, now.AddDate(0, 0, 30)},
		{domainrecommendation.FeedbackTypeReduceAuthor, domainrecommendation.SuppressionScopeAuthor, 202, now.AddDate(0, 0, 14)},
		{domainrecommendation.FeedbackTypeAlreadySeen, domainrecommendation.SuppressionScopeVideo, 303, now.AddDate(0, 0, 7)},
	} {
		feedback, err := domainrecommendation.NewFeedback(9, item.scopeID, "feedback-request", item.feedbackType, "feedback-"+item.feedbackType, now)
		if err != nil {
			t.Fatalf("new feedback: %v", err)
		}
		if err := feedback.SetSuppression(item.scope, item.scopeID, item.expiresAt); err != nil {
			t.Fatalf("set policy suppression: %v", err)
		}
		saved, replayed, err := repo.SaveFeedback(context.Background(), feedback)
		if err != nil || replayed || !saved.SuppressionExpiresAt.Equal(item.expiresAt) {
			t.Fatalf("save policy suppression = %#v replayed=%v err=%v", saved, replayed, err)
		}
		var model FeedbackModel
		if err := db.Where("id = ?", saved.ID).Take(&model).Error; err != nil || !model.SuppressionExpiresAt.Equal(item.expiresAt) {
			t.Fatalf("persisted policy expiry = %#v err=%v", model, err)
		}
	}

	log, err := domainrecommendation.NewRecommendationRequestLog(domainrecommendation.RequestLogInput{
		RequestID: "sampled-request", UserID: 9, Scene: domainrecommendation.RecommendationRequestLogScene, PolicyVersion: 1, CreatedAt: now.Add(-48 * time.Hour),
		Candidates: []domainrecommendation.LoggedCandidate{{VideoID: 1, Reasons: []string{"fresh"}}},
	})
	if err != nil {
		t.Fatalf("new request log: %v", err)
	}
	if _, replayed, err := repo.SaveRequestLog(context.Background(), log); err != nil || replayed {
		t.Fatalf("save request log: replayed=%v err=%v", replayed, err)
	}
	if _, replayed, err := repo.SaveRequestLog(context.Background(), log); err != nil || !replayed {
		t.Fatalf("replay request log: replayed=%v err=%v", replayed, err)
	}
	sameIDOtherUser, err := domainrecommendation.NewRecommendationRequestLog(domainrecommendation.RequestLogInput{
		RequestID: "sampled-request", UserID: 10, Scene: domainrecommendation.RecommendationRequestLogScene, PolicyVersion: 1, CreatedAt: now,
		Candidates: []domainrecommendation.LoggedCandidate{{VideoID: 1, Reasons: []string{"fresh"}}},
	})
	if err != nil {
		t.Fatalf("new second-user request log: %v", err)
	}
	if _, replayed, err := repo.SaveRequestLog(context.Background(), sameIDOtherUser); err != nil || replayed {
		t.Fatalf("same request ID for a different user was not independently saved: replayed=%v err=%v", replayed, err)
	}
	conflicting, err := domainrecommendation.NewRecommendationRequestLog(domainrecommendation.RequestLogInput{
		RequestID: "sampled-request", UserID: 9, Scene: domainrecommendation.RecommendationRequestLogScene, PolicyVersion: 1, CreatedAt: now,
		Candidates: []domainrecommendation.LoggedCandidate{{VideoID: 2, Reasons: []string{"hot"}}},
	})
	if err != nil {
		t.Fatalf("new conflicting request log: %v", err)
	}
	if _, _, err := repo.SaveRequestLog(context.Background(), conflicting); !errors.Is(err, domainrecommendation.ErrRequestLogConflict) {
		t.Fatalf("same-user request log payload conflict = %v", err)
	}
	deleted, err := repo.DeleteRequestLogsBefore(context.Background(), now.Add(-24*time.Hour), 10)
	if err != nil || deleted != 1 {
		t.Fatalf("cleanup request logs: deleted=%d err=%v", deleted, err)
	}

	evidence, err := domainrecommendation.NewServedCandidateEvidence(domainrecommendation.ServedCandidateEvidenceInput{
		UserID: 9, RequestID: "served-request", Scene: domainrecommendation.RecommendationRequestLogScene,
		PolicyVersion: 1, ServedAt: now, ExpiresAt: now.Add(2 * time.Minute),
		Candidates: []domainrecommendation.ServedCandidateEvidenceItem{{VideoID: 1, Position: 0}, {VideoID: 2, Position: 1}},
	})
	if err != nil {
		t.Fatalf("new served-candidate evidence: %v", err)
	}
	if replayed, err := repo.SaveServedCandidateEvidence(context.Background(), evidence); err != nil || replayed {
		t.Fatalf("save served-candidate evidence: replayed=%v err=%v", replayed, err)
	}
	if replayed, err := repo.SaveServedCandidateEvidence(context.Background(), evidence); err != nil || !replayed {
		t.Fatalf("replay served-candidate evidence: replayed=%v err=%v", replayed, err)
	}
	if valid, err := repo.HasServedCandidateEvidence(context.Background(), 9, "served-request", 2, now); err != nil || !valid {
		t.Fatalf("served membership check: valid=%v err=%v", valid, err)
	}
	if deleted, err := repo.DeleteServedCandidateEvidenceBefore(context.Background(), now.Add(3*time.Minute), 10); err != nil || deleted.RequestGroups != 1 || deleted.CandidateRows != 2 {
		t.Fatalf("cleanup served-candidate evidence: deleted=%+v err=%v", deleted, err)
	}

	delayedServedAt := now.Add(-10 * time.Minute)
	delayedEvidence, err := domainrecommendation.NewServedCandidateEvidence(domainrecommendation.ServedCandidateEvidenceInput{
		UserID: 9, RequestID: "delayed-outcome", Scene: domainrecommendation.RecommendationRequestLogScene,
		PolicyVersion: 1, ServedAt: delayedServedAt, ExpiresAt: delayedServedAt.Add(time.Minute),
		Candidates: []domainrecommendation.ServedCandidateEvidenceItem{{VideoID: 3, Position: 0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveServedCandidateEvidence(context.Background(), delayedEvidence); err != nil {
		t.Fatalf("save delayed evidence: %v", err)
	}
	validOutcome, err := domainrecommendation.NewOutcomeWithRecordedAt(
		"delayed-valid",
		"delayed-outcome",
		9,
		3,
		"complete",
		delayedServedAt.Add(-24*time.Hour),
		delayedServedAt.Add(30*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if recorded, attributed, err := repo.VerifyAndSaveOutcome(context.Background(), validOutcome, 0); err != nil || !recorded || !attributed {
		t.Fatalf("delayed valid outcome was rejected: recorded=%v attributed=%v err=%v", recorded, attributed, err)
	}
	for _, recordedAt := range []time.Time{delayedServedAt.Add(-time.Nanosecond), delayedServedAt.Add(time.Minute)} {
		outcome, outcomeErr := domainrecommendation.NewOutcomeWithRecordedAt(
			"delayed-invalid-"+strconv.FormatInt(recordedAt.UnixNano(), 10),
			"delayed-outcome",
			9,
			3,
			"complete",
			delayedServedAt.Add(30*time.Second),
			recordedAt,
		)
		if outcomeErr != nil {
			t.Fatal(outcomeErr)
		}
		if recorded, attributed, saveErr := repo.VerifyAndSaveOutcome(context.Background(), outcome, 0); saveErr != nil || recorded || attributed {
			t.Fatalf("outcome outside served interval was accepted: recorded_at=%s recorded=%v attributed=%v err=%v", recordedAt, recorded, attributed, saveErr)
		}
	}

	cutoff := domainrecommendation.ServedCandidateEvidenceCleanupCutoff(now)
	if deleted, cleanupErr := repo.DeleteServedCandidateEvidenceBefore(context.Background(), cutoff, 10); cleanupErr != nil || deleted.RequestGroups != 1 {
		t.Fatalf("clean delayed attribution evidence after grace: deleted=%+v err=%v", deleted, cleanupErr)
	}
	newEvidence := func(requestID string, expiresAt time.Time, candidates []domainrecommendation.ServedCandidateEvidenceItem) {
		evidence, evidenceErr := domainrecommendation.NewServedCandidateEvidence(domainrecommendation.ServedCandidateEvidenceInput{
			UserID: 9, RequestID: requestID, Scene: domainrecommendation.RecommendationRequestLogScene,
			PolicyVersion: 1, ServedAt: expiresAt.Add(-time.Minute), ExpiresAt: expiresAt, Candidates: candidates,
		})
		if evidenceErr != nil {
			t.Fatal(evidenceErr)
		}
		if _, saveErr := repo.SaveServedCandidateEvidence(context.Background(), evidence); saveErr != nil {
			t.Fatalf("save evidence %s: %v", requestID, saveErr)
		}
	}
	newEvidence("within-grace", cutoff.Add(time.Second), []domainrecommendation.ServedCandidateEvidenceItem{{VideoID: 4, Position: 0}})
	if deleted, cleanupErr := repo.DeleteServedCandidateEvidenceBefore(context.Background(), cutoff, 1); cleanupErr != nil || deleted.RequestGroups != 0 {
		t.Fatalf("delivery grace evidence was cleaned early: deleted=%+v err=%v", deleted, cleanupErr)
	}
	largeCandidates := make([]domainrecommendation.ServedCandidateEvidenceItem, domainrecommendation.MaxServedCandidateEvidence)
	for index := range largeCandidates {
		largeCandidates[index] = domainrecommendation.ServedCandidateEvidenceItem{VideoID: int64(10_000 + index), Position: index}
	}
	newEvidence("large-expired-group", cutoff.Add(-2*time.Second), largeCandidates)
	newEvidence("second-expired-group", cutoff.Add(-time.Second), []domainrecommendation.ServedCandidateEvidenceItem{{VideoID: 20_000, Position: 0}})
	if deleted, cleanupErr := repo.DeleteServedCandidateEvidenceBefore(context.Background(), cutoff, 1); cleanupErr != nil || deleted.RequestGroups != 1 || deleted.CandidateRows != int64(len(largeCandidates)) {
		t.Fatalf("large request group cleanup was row-limited: deleted=%+v err=%v", deleted, cleanupErr)
	}
	if deleted, cleanupErr := repo.DeleteServedCandidateEvidenceBefore(context.Background(), cutoff, 1); cleanupErr != nil || deleted.RequestGroups != 1 || deleted.CandidateRows != 1 {
		t.Fatalf("second request group cleanup batch failed: deleted=%+v err=%v", deleted, cleanupErr)
	}
}

func TestServedCandidateEvidenceReusesExpiredRequestAndSerializesConcurrentWritesPostgreSQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set; skipping real PostgreSQL integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schema := fmt.Sprintf("frux_recommendation_evidence_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		_ = admin.Close()
	})
	sqlDB, err := sql.Open("pgx", recommendationPostgresDSNWithSchema(dsn, schema))
	if err != nil {
		t.Fatalf("open schema PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open GORM: %v", err)
	}
	if err := db.AutoMigrate(&ServedCandidateEvidenceModel{}); err != nil {
		t.Fatalf("migrate served evidence: %v", err)
	}
	repo := New(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	newEvidence := func(requestID string, videoIDs ...int64) *domainrecommendation.ServedCandidateEvidence {
		items := make([]domainrecommendation.ServedCandidateEvidenceItem, 0, len(videoIDs))
		for position, videoID := range videoIDs {
			items = append(items, domainrecommendation.ServedCandidateEvidenceItem{VideoID: videoID, Position: position})
		}
		evidence, evidenceErr := domainrecommendation.NewServedCandidateEvidence(domainrecommendation.ServedCandidateEvidenceInput{
			UserID: 9, RequestID: requestID, Scene: domainrecommendation.RecommendationRequestLogScene,
			PolicyVersion: 1, ServedAt: now, ExpiresAt: now.Add(5 * time.Minute), Candidates: items,
		})
		if evidenceErr != nil {
			t.Fatal(evidenceErr)
		}
		return evidence
	}

	if replayed, err := repo.SaveServedCandidateEvidence(context.Background(), newEvidence("reused", 1, 2)); err != nil || replayed {
		t.Fatalf("save active evidence: replayed=%v err=%v", replayed, err)
	}
	if _, err := repo.SaveServedCandidateEvidence(context.Background(), newEvidence("reused", 3, 4)); !errors.Is(err, domainrecommendation.ErrServedCandidateEvidenceConflict) {
		t.Fatalf("active changed membership did not conflict: %v", err)
	}
	if err := db.Model(&ServedCandidateEvidenceModel{}).Where("user_id = ? AND request_id = ?", 9, "reused").
		Update("expires_at", now.Add(-domainrecommendation.ServedCandidateEvidenceDeliveryGrace-time.Second)).Error; err != nil {
		t.Fatalf("expire evidence without cleanup: %v", err)
	}
	if replayed, err := repo.SaveServedCandidateEvidence(context.Background(), newEvidence("reused", 3, 4)); err != nil || replayed {
		t.Fatalf("expired request ID was not atomically refreshed: replayed=%v err=%v", replayed, err)
	}
	if valid, err := repo.HasServedCandidateEvidence(context.Background(), 9, "reused", 1, now); err != nil || valid {
		t.Fatalf("expired membership survived refresh: valid=%v err=%v", valid, err)
	}
	if valid, err := repo.HasServedCandidateEvidence(context.Background(), 9, "reused", 4, now); err != nil || !valid {
		t.Fatalf("refreshed membership missing: valid=%v err=%v", valid, err)
	}

	concurrent := []*domainrecommendation.ServedCandidateEvidence{
		newEvidence("concurrent", 7),
		newEvidence("concurrent", 8),
	}
	start := make(chan struct{})
	results := make(chan struct {
		replayed bool
		err      error
	}, 2)
	var group sync.WaitGroup
	for _, evidence := range concurrent {
		group.Add(1)
		go func(evidence *domainrecommendation.ServedCandidateEvidence) {
			defer group.Done()
			<-start
			replayed, saveErr := repo.SaveServedCandidateEvidence(context.Background(), evidence)
			results <- struct {
				replayed bool
				err      error
			}{replayed: replayed, err: saveErr}
		}(evidence)
	}
	close(start)
	group.Wait()
	close(results)
	created, conflicted := 0, 0
	for result := range results {
		if errors.Is(result.err, domainrecommendation.ErrServedCandidateEvidenceConflict) {
			conflicted++
			continue
		}
		if result.err != nil || result.replayed {
			t.Fatalf("concurrent save result = replayed:%v err:%v", result.replayed, result.err)
		}
		created++
	}
	if created != 1 || conflicted != 1 {
		t.Fatalf("concurrent changed memberships = created:%d conflicted:%d", created, conflicted)
	}
	var count int64
	if err := db.Model(&ServedCandidateEvidenceModel{}).Where("user_id = ? AND request_id = ?", 9, "concurrent").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("concurrent save membership count=%d err=%v", count, err)
	}
}

func prepareProfileFactTables(t testing.TB, db *gorm.DB) {
	t.Helper()
	for _, statement := range []string{
		`CREATE TABLE video (id BIGINT PRIMARY KEY, author_id BIGINT NOT NULL)`,
		`CREATE TABLE video_embedding (video_id BIGINT NOT NULL, model TEXT NOT NULL, embedding_json TEXT NOT NULL)`,
		`CREATE TABLE interaction_action (user_id BIGINT NOT NULL, video_id BIGINT NOT NULL, action_type TEXT NOT NULL, status INTEGER NOT NULL, updated_at TIMESTAMPTZ NOT NULL)`,
		`CREATE TABLE interaction_action_event (event_id TEXT PRIMARY KEY, user_id BIGINT NOT NULL, video_id BIGINT NOT NULL, action_type TEXT NOT NULL, active BOOLEAN NOT NULL, profile_projection_dispatched_at TIMESTAMPTZ)`,
		`CREATE TABLE user_follow (user_id BIGINT NOT NULL, target_user_id BIGINT NOT NULL, status INTEGER NOT NULL, updated_at TIMESTAMPTZ NOT NULL)`,
		`CREATE TABLE relation_profile_projection_outbox (event_id TEXT PRIMARY KEY, user_id BIGINT NOT NULL, target_user_id BIGINT NOT NULL, dispatched_at TIMESTAMPTZ)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("prepare profile fact table: %v", err)
		}
	}
}

func postgresPolicyConfig(rollout int) domainrecommendation.PolicyConfiguration {
	return domainrecommendation.PolicyConfiguration{
		FeatureWeights:         map[string]float64{domainrecommendation.FeatureHotness: 0.5},
		RecallBudgets:          map[string]int{domainrecommendation.RecallProviderFresh: 20},
		ProviderDeadlinesMS:    map[string]int{domainrecommendation.RecallProviderFresh: 100},
		FreshnessHalfLifeHours: 48, ExposureWindowHours: 168,
		Diversity:         domainrecommendation.DiversityRules{MaxPerAuthor: 2, MinAuthorGap: 1},
		RolloutPercentage: rollout, SnapshotTTLSeconds: 300, SamplingRatePPM: 1, RetentionDays: 7,
	}
}

func TestApplyBehaviorEventDeduplicatesLegacyQueuedMessageByViewEventID(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set; skipping real PostgreSQL integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schema := fmt.Sprintf("frux_recommendation_behavior_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		_ = admin.Close()
	})

	sqlDB, err := sql.Open("pgx", recommendationPostgresDSNWithSchema(dsn, schema))
	if err != nil {
		t.Fatalf("open schema PostgreSQL: %v", err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open GORM: %v", err)
	}
	if err := db.AutoMigrate(&BehaviorEventModel{}); err != nil {
		t.Fatalf("migrate behavior events: %v", err)
	}

	occurredAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := db.Create(&BehaviorEventModel{
		EventID: "legacy-55", ViewEventID: 55, UserID: 9, VideoID: 1001,
		EventType: "play", OccurredAt: occurredAt,
	}).Error; err != nil {
		t.Fatalf("create backfilled behavior event: %v", err)
	}

	repo := New(db)
	applied, err := repo.ApplyBehaviorEvent(context.Background(), &applicationexposure.ViewEventRecordedEvent{
		EventID: "old-random-message-id", ViewEventID: 55, UserID: 9, VideoID: 1001,
		EventType: "play", OccurredAt: occurredAt,
	})
	if err != nil || applied {
		t.Fatalf("legacy queued duplicate: applied=%v err=%v", applied, err)
	}
	var count int64
	if err := db.Model(&BehaviorEventModel{}).Count(&count).Error; err != nil {
		t.Fatalf("count behavior events: %v", err)
	}
	if count != 1 {
		t.Fatalf("legacy queued duplicate inserted another row: count=%d", count)
	}
}

func TestBehaviorProjectionAndAppliedEventsScopeSharedEventIDByUser(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUX_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("FRUX_POSTGRES_TEST_DSN is not set; skipping real PostgreSQL integration test")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schema := fmt.Sprintf("frux_recommendation_identity_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		_ = admin.Close()
	})

	sqlDB, err := sql.Open("pgx", recommendationPostgresDSNWithSchema(dsn, schema))
	if err != nil {
		t.Fatalf("open schema PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open GORM: %v", err)
	}
	if err := db.AutoMigrate(&BehaviorEventModel{}, &UserInterestProfileModel{}, &AppliedProfileEventModel{}); err != nil {
		t.Fatalf("migrate recommendation identity storage: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	repo := New(db)
	for _, event := range []BehaviorEventModel{
		{
			EventID: "shared-client-view", ViewEventID: 701, UserID: 71, VideoID: 1001,
			EventType: domainexposure.EventTypeComplete, OccurredAt: now, ProfileAvailableAt: now.Add(-time.Second),
		},
		{
			EventID: "shared-client-view", ViewEventID: 702, UserID: 72, VideoID: 1002,
			EventType: domainexposure.EventTypeComplete, OccurredAt: now, ProfileAvailableAt: now.Add(-time.Second),
		},
	} {
		if err := db.Create(&event).Error; err != nil {
			t.Fatalf("create shared behavior event: %v", err)
		}
	}
	claimed, err := repo.ClaimBehaviorProfileProjections(context.Background(), 10, now, now.Add(time.Minute))
	if err != nil || len(claimed) != 2 {
		t.Fatalf("claim shared behavior events = %#v, %v", claimed, err)
	}
	if err := repo.MarkBehaviorProfileProjectionDispatched(context.Background(), 71, "shared-client-view", now); err != nil {
		t.Fatalf("mark first user dispatched: %v", err)
	}
	retryAt := now.Add(2 * time.Minute)
	if err := repo.MarkBehaviorProfileProjectionFailed(context.Background(), 72, "shared-client-view", retryAt, "embedding unavailable"); err != nil {
		t.Fatalf("mark second user failed: %v", err)
	}
	var behavior []BehaviorEventModel
	if err := db.Order("user_id").Find(&behavior).Error; err != nil || len(behavior) != 2 {
		t.Fatalf("load scoped behavior events = %#v, %v", behavior, err)
	}
	if behavior[0].UserID != 71 || behavior[0].ProfileDispatchedAt == nil || behavior[1].UserID != 72 ||
		behavior[1].ProfileDispatchedAt != nil || !behavior[1].ProfileAvailableAt.Equal(retryAt) {
		t.Fatalf("marking one user altered the other shared event: %#v", behavior)
	}

	for _, userID := range []int64{71, 72} {
		model, modelErr := profileToModel(domainrecommendation.EmptyUserInterestProfile(userID, now))
		if modelErr != nil {
			t.Fatalf("create empty profile model: %v", modelErr)
		}
		if createErr := db.Create(&model).Error; createErr != nil {
			t.Fatalf("create empty user profile: %v", createErr)
		}
		event, eventErr := domainrecommendation.NewProfileEvent(domainrecommendation.ProfileEventInput{
			UserID: userID, SourceEventID: "shared-client-view", EventType: "complete", OccurredAt: now,
		})
		if eventErr != nil {
			t.Fatalf("new profile event: %v", eventErr)
		}
		if _, applied, applyErr := repo.ApplyProfileEvent(context.Background(), event); applyErr != nil || !applied {
			t.Fatalf("apply profile event for user %d: applied=%v err=%v", userID, applied, applyErr)
		}
	}
	var appliedCount int64
	if err := db.Model(&AppliedProfileEventModel{}).Where("source_event_id = ?", "shared-client-view").Count(&appliedCount).Error; err != nil || appliedCount != 2 {
		t.Fatalf("applied events did not retain composite identity: count=%d err=%v", appliedCount, err)
	}
}

func recommendationPostgresDSNWithSchema(dsn, schema string) string {
	if strings.Contains(dsn, "://") {
		parsed, err := url.Parse(dsn)
		if err == nil {
			query := parsed.Query()
			query.Set("search_path", schema)
			query.Set("TimeZone", "UTC")
			parsed.RawQuery = query.Encode()
			return parsed.String()
		}
	}
	return strings.TrimSpace(dsn) + " search_path=" + schema + " TimeZone=UTC"
}
