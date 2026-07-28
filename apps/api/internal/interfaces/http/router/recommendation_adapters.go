package interfaceshttprouter

import (
	domainrelation "GCFeed/internal/domain/relation"
	"context"
)

// followedAuthorRecallAdapter keeps recommendation application code independent
// of the relation repository implementation.
type followedAuthorRecallAdapter struct {
	source domainrelation.Repository
}

func (a followedAuthorRecallAdapter) ListFollowedAuthorIDs(ctx context.Context, userID int64, limit int) ([]int64, error) {
	if a.source == nil || limit <= 0 {
		return []int64{}, nil
	}
	items, err := a.source.ListFollowing(ctx, userID, nil, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		if item != nil && item.UserID > 0 {
			ids = append(ids, item.UserID)
		}
	}
	return ids, nil
}
