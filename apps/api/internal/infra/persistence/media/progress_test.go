package inframedia

import (
	"testing"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

func TestHistoricalProcessingJobRestoresTruthfulStep(t *testing.T) {
	completed := processingJobFromModel(ProcessingJobModel{
		ID: 1, AssetID: 2, ProfileVersion: "v1",
		State: domainmedia.JobStateCompleted,
	})
	if completed.ProcessingStep != domainmedia.ProcessingStepCompleted ||
		completed.ProgressBPS != nil {
		t.Fatalf("completed=%+v", completed)
	}
	failed := processingJobFromModel(ProcessingJobModel{
		ID: 2, AssetID: 3, ProfileVersion: "v1",
		State: domainmedia.JobStateFailed,
	})
	if failed.ProcessingStep != domainmedia.ProcessingStepFailed ||
		failed.ProgressBPS != nil {
		t.Fatalf("failed=%+v", failed)
	}
}
