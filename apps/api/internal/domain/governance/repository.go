package domaingovernance

import (
	"context"

	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
)

type Repository interface {
	ListActive(ctx context.Context) ([]*Revision, error)
	ListRevisions(ctx context.Context, key Key, limit int) ([]*Revision, error)
	FindRevision(ctx context.Context, key Key, revision int64) (*Revision, error)
	CommitRevision(
		ctx context.Context,
		expectedRevision int64,
		revision *Revision,
		auditFact *domainadminaudit.Fact,
	) error
}
