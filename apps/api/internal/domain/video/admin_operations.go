package domainvideo

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
)

const (
	MaxAdminQueryLimit        = 100
	MaxAdminKeywordLength     = 128
	MaxEnforcementNoteRunes   = 1000
	MaxAdminQueryWindow       = 366 * 24 * time.Hour
	AdminIntentStatePending   = "pending"
	AdminIntentStateDelivered = "delivered"
	EnforcementReasonManual   = "manual_enforcement"
	EnforcementReasonPolicy   = "policy_violation"
	RestorationReasonAllowed  = "compliance_restored"
)

type AdminVideoCursor struct {
	CreatedAt time.Time
	VideoID   int64
}

type AdminVideoQuery struct {
	Status      int
	AuthorID    int64
	VideoID     int64
	Keyword     string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Cursor      *AdminVideoCursor
	Limit       int
}

type AdminTransitionCommand struct {
	VideoID         int64
	ActorID         int64
	ExpectedVersion int
	Transition      LifecycleTransition
	ReasonCode      string
	Note            string
	OccurredAt      time.Time
}

type AdminTransitionResult struct {
	Video          *Video
	PreviousStatus int
}

type AdminTransitionIntent struct {
	ID       int64
	EventID  string
	VideoID  int64
	Attempts int
}

type AdminRepository interface {
	ListAdminVideos(ctx context.Context, query AdminVideoQuery) ([]*Video, error)
	CommitAdminTransition(
		ctx context.Context,
		command AdminTransitionCommand,
		auditFact *domainadminaudit.Fact,
	) (*AdminTransitionResult, error)
}

func NormalizeAdminVideoQuery(query AdminVideoQuery) (AdminVideoQuery, error) {
	if query.Status != 0 && (!ValidStatus(query.Status) || query.Status == StatusDeleted) {
		return AdminVideoQuery{}, ErrInvalidStatus
	}
	if query.AuthorID < 0 {
		return AdminVideoQuery{}, ErrInvalidAuthorID
	}
	if query.VideoID < 0 {
		return AdminVideoQuery{}, ErrInvalidVideoID
	}
	query.Keyword = strings.TrimSpace(query.Keyword)
	if len(query.Keyword) > MaxAdminKeywordLength {
		return AdminVideoQuery{}, ErrAdminQueryInvalid
	}
	if (query.CreatedFrom == nil) != (query.CreatedTo == nil) {
		return AdminVideoQuery{}, ErrInvalidDateRange
	}
	if query.CreatedFrom != nil {
		from := query.CreatedFrom.UTC()
		to := query.CreatedTo.UTC()
		if from.After(to) || to.Sub(from) > MaxAdminQueryWindow {
			return AdminVideoQuery{}, ErrInvalidDateRange
		}
		query.CreatedFrom, query.CreatedTo = &from, &to
	}
	if query.Limit == 0 {
		query.Limit = 20
	}
	if query.Limit < 1 || query.Limit > MaxAdminQueryLimit+1 {
		return AdminVideoQuery{}, ErrInvalidLimit
	}
	if query.Cursor != nil &&
		(query.Cursor.VideoID <= 0 || query.Cursor.CreatedAt.IsZero()) {
		return AdminVideoQuery{}, ErrInvalidCursor
	}
	return query, nil
}

func NormalizeAdminTransition(command AdminTransitionCommand) (AdminTransitionCommand, error) {
	if command.VideoID <= 0 {
		return AdminTransitionCommand{}, ErrInvalidVideoID
	}
	if command.ActorID <= 0 {
		return AdminTransitionCommand{}, ErrInvalidAuthorID
	}
	if command.ExpectedVersion <= 0 {
		return AdminTransitionCommand{}, ErrInvalidExpectedVersion
	}
	command.ReasonCode = strings.TrimSpace(command.ReasonCode)
	command.Note = strings.TrimSpace(command.Note)
	if !utf8.ValidString(command.Note) || utf8.RuneCountInString(command.Note) > MaxEnforcementNoteRunes {
		return AdminTransitionCommand{}, ErrEnforcementNoteTooLong
	}
	switch command.Transition {
	case LifecycleTakeOffline:
		if command.ReasonCode != EnforcementReasonManual &&
			command.ReasonCode != EnforcementReasonPolicy {
			return AdminTransitionCommand{}, ErrInvalidEnforcementReason
		}
	case LifecycleRestore:
		if command.ReasonCode != RestorationReasonAllowed {
			return AdminTransitionCommand{}, ErrInvalidEnforcementReason
		}
	default:
		return AdminTransitionCommand{}, ErrVideoStateNotAllowed
	}
	if command.OccurredAt.IsZero() {
		command.OccurredAt = time.Now().UTC()
	} else {
		command.OccurredAt = command.OccurredAt.UTC()
	}
	return command, nil
}
