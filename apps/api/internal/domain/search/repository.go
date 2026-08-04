package domainsearch

import "context"

type VideoSearchIndex interface {
	SearchVideos(ctx context.Context, query string, cursor *VideoCursor, limit int) ([]*VideoIndexItem, error)
}

type UserSearchIndex interface {
	SearchUsers(ctx context.Context, query string, cursor *UserCursor, limit int) ([]*UserIndexItem, error)
}
