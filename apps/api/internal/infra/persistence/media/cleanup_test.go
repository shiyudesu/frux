package inframedia

import (
	"testing"
	"time"

	domainmedia "github.com/shiyudesu/frux/internal/domain/media"
)

func TestCleanupTaskModelsFromDomainDeduplicatesSharedObject(t *testing.T) {
	now := time.Now().UTC()
	tasks := []*domainmedia.CleanupTask{
		{
			AssetID: 7, StorageBackend: domainmedia.StorageBackendS3,
			ObjectKey: "uploads/7/cover/source.jpg", NotBefore: now.Add(time.Hour),
			MaxAttempts: 3,
		},
		{
			AssetID: 7, StorageBackend: domainmedia.StorageBackendS3,
			ObjectKey: "uploads/7/cover/source.jpg", NotBefore: now,
			MaxAttempts: 5,
		},
	}
	models := cleanupTaskModelsFromDomain(tasks)
	if len(models) != 1 {
		t.Fatalf("models = %+v", models)
	}
	if !models[0].NotBefore.Equal(now) || models[0].MaxAttempts != 5 {
		t.Fatalf("deduplicated model = %+v", models[0])
	}
}
