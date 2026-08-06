package domainreview

import "context"

type Repository interface {
	CreateOrGetCase(ctx context.Context, videoID int64) (*ReviewCase, bool, error)
	ProcessMachineResult(ctx context.Context, result *MachineResult) (*ProcessingResult, error)
	ListReviewableVideoIDsWithoutCase(ctx context.Context, limit int) ([]int64, error)
}
