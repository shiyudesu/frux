package domainvideo

import (
	"context"
	"time"
)

// Repository 定义视频领域需要的持久化能力。
type Repository interface {
	// Save 保存视频和初始统计数据。
	Save(ctx context.Context, video *Video) error
	// FindByID 查询已发布视频，用于公开详情。
	FindByID(ctx context.Context, id int64) (*Video, error)
	// FindByIDAnyStatus 查询任意状态视频，用于作者删除等内部场景。
	FindByIDAnyStatus(ctx context.Context, id int64) (*Video, error)
	// FindByAuthorAndIdempotencyKey 用于发布接口的幂等重放。
	FindByAuthorAndIdempotencyKey(ctx context.Context, authorID int64, key string) (*Video, error)
	// ListByAuthor 查询作者已发布视频列表。
	ListByAuthor(ctx context.Context, authorID int64, limit, offset int) ([]*Video, error)
	// ListByOwner 查询作者自己的非删除视频，包括待审和拒绝状态。
	ListByOwner(ctx context.Context, authorID int64, limit, offset int) ([]*Video, error)
	// UpdateStatus 更新视频状态，例如软删除。
	UpdateStatus(ctx context.Context, video *Video) (bool, error)
	// ApplyLifecycleTransition 在数据库锁内执行具体审核生命周期转换。
	ApplyLifecycleTransition(ctx context.Context, videoID int64, transition LifecycleTransition, at time.Time) (bool, error)
}
