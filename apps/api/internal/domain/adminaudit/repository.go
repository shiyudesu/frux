package domainadminaudit

import (
	"context"
	"time"
)

type Cursor struct {
	CreatedAt time.Time
	EventID   int64
}

type Query struct {
	ActorID    int64
	Action     Action
	TargetType TargetType
	Outcome    Outcome
	From       time.Time
	To         time.Time
	Cursor     *Cursor
	Limit      int
}

type Repository interface {
	Append(ctx context.Context, fact *Fact) error
	List(ctx context.Context, query Query) ([]*Fact, error)
}
