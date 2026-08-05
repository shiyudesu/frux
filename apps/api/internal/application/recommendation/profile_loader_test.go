package applicationrecommendation

import (
	domainrecommendation "github.com/shiyudesu/frux/internal/domain/recommendation"
	"context"
	"testing"
	"time"
)

type profileLoaderStub struct {
	profile *domainrecommendation.UserInterestProfile
	found   bool
	rebuilt bool
}

func (s *profileLoaderStub) ListCandidatePool(context.Context, int64, int) ([]*domainrecommendation.Candidate, error) {
	return nil, nil
}
func (s *profileLoaderStub) LoadUserInterestVector(context.Context, int64) ([]float64, bool, error) {
	return nil, false, nil
}
func (s *profileLoaderStub) LoadVideoVectors(context.Context, []int64) (map[int64][]float64, error) {
	return nil, nil
}
func (s *profileLoaderStub) ListRecentExposures(context.Context, int64, []int64, time.Time) ([]*domainrecommendation.Exposure, error) {
	return nil, nil
}
func (s *profileLoaderStub) SaveExposures(context.Context, []*domainrecommendation.ExposureWrite) ([]*domainrecommendation.Exposure, error) {
	return nil, nil
}
func (*profileLoaderStub) FindFeedbackByUserAndIdempotencyKey(context.Context, int64, string) (*domainrecommendation.Feedback, error) {
	return nil, domainrecommendation.ErrFeedbackNotFound
}
func (s *profileLoaderStub) SaveFeedback(context.Context, *domainrecommendation.Feedback) (*domainrecommendation.Feedback, bool, error) {
	return nil, false, nil
}
func (s *profileLoaderStub) LoadUserInterestProfile(context.Context, int64) (*domainrecommendation.UserInterestProfile, bool, error) {
	return s.profile, s.found, nil
}
func (s *profileLoaderStub) RebuildUserInterestVector(context.Context, int64) ([]float64, bool, error) {
	s.rebuilt = true
	return []float64{2}, true, nil
}
func TestProfileLoaderUsesDurableFallbackOnlyWhenAbsent(t *testing.T) {
	now := time.Now()
	repo := &profileLoaderStub{profile: domainrecommendation.RestoreUserInterestProfile(1, []float64{1}, []float64{3}, nil, nil, nil, 1, now), found: true}
	vector, found, err := loadProfileInterestVector(context.Background(), repo, 1)
	if err != nil || !found || vector[0] != 3 || repo.rebuilt {
		t.Fatalf("materialized profile was not preferred: %v %v %v", vector, found, repo.rebuilt)
	}
	repo.found = false
	vector, found, err = loadProfileInterestVector(context.Background(), repo, 1)
	if err != nil || !found || vector[0] != 2 || !repo.rebuilt {
		t.Fatalf("fallback was not used: %v %v %v", vector, found, repo.rebuilt)
	}
}
