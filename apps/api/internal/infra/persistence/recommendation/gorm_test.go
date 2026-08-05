package infrarecommendation

import (
	domainexposure "github.com/shiyudesu/frux/internal/domain/exposure"
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestProgressEventWeightUsesBoundedPlaybackProgress(t *testing.T) {
	duration := 60_000
	early := eventWeight(domainexposure.EventTypeProgress, 5_000, 4_000, &duration, false)
	late := eventWeight(domainexposure.EventTypeProgress, 45_000, 40_000, &duration, false)
	if early != 0.25 {
		t.Fatalf("early progress weight = %v, want floor 0.25", early)
	}
	if late <= early || late > 1.5 {
		t.Fatalf("late progress weight is not bounded and increasing: early=%v late=%v", early, late)
	}
	if complete := eventWeight(domainexposure.EventTypeComplete, 60_000, 55_000, &duration, true); complete <= late {
		t.Fatalf("complete weight %v should exceed progress weight %v", complete, late)
	}
}

func TestProfileReconstructionTreatsReduceAuthorAsAuthorOnlyFeedback(t *testing.T) {
	accumulator := profileReconstruction{
		authors:         map[int64]float64{},
		negativeAuthors: map[int64]float64{},
	}
	accumulator.addFeedback(profileFeedbackFact{
		EmbeddingJSON:      `[1,0]`,
		AuthorID:           7,
		FeedbackType:       domainrecommendation.FeedbackTypeReduceAuthor,
		SuppressionScope:   domainrecommendation.SuppressionScopeAuthor,
		SuppressionScopeID: 9,
	}, 1.5)
	if len(accumulator.negativeTopics) != 0 || accumulator.negativeAuthors[9] != 1.5 {
		t.Fatalf("reduce-author feedback used video topics: %#v", accumulator)
	}

	accumulator.addFeedback(profileFeedbackFact{
		EmbeddingJSON:      `[1,0]`,
		AuthorID:           7,
		FeedbackType:       domainrecommendation.FeedbackTypeNotInterested,
		SuppressionScope:   domainrecommendation.SuppressionScopeVideo,
		SuppressionScopeID: 101,
	}, 1.5)
	if len(accumulator.negativeTopics) != 2 || accumulator.negativeTopics[0] != 1.5 || accumulator.negativeAuthors[7] != 1.5 {
		t.Fatalf("not-interested feedback did not retain video/topic negativity: %#v", accumulator)
	}
}

func TestProfileModelPreservesMaterializedUpdatedAt(t *testing.T) {
	materializedAt := time.Date(2026, 7, 27, 4, 0, 0, 123000000, time.UTC)
	profile := domainrecommendation.RestoreUserInterestProfile(
		7, []float64{1}, []float64{1}, map[int64]float64{}, []float64{1}, map[int64]float64{}, 2, materializedAt,
	)
	model, err := profileToModel(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !model.UpdatedAt.Equal(materializedAt) {
		t.Fatalf("profile materialization timestamp was changed: got %s want %s", model.UpdatedAt, materializedAt)
	}
	field, ok := reflect.TypeOf(UserInterestProfileModel{}).FieldByName("UpdatedAt")
	if !ok || field.Tag.Get("gorm") == "" || strings.Contains(field.Tag.Get("gorm"), "autoUpdateTime") {
		t.Fatalf("profile UpdatedAt must be explicitly persisted, tag=%q", field.Tag.Get("gorm"))
	}
}
