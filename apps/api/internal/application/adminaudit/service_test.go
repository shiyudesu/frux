package applicationadminaudit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	domainaccount "github.com/shiyudesu/frux/internal/domain/account"
	domainadminaudit "github.com/shiyudesu/frux/internal/domain/adminaudit"
)

const applicationAuditTestRequestID = "audit-0123456789abcdef0123456789abcdef"

type auditRepositoryStub struct {
	items     []*domainadminaudit.Fact
	appendErr error
	appended  []*domainadminaudit.Fact
	queries   []domainadminaudit.Query
}

func (r *auditRepositoryStub) Append(_ context.Context, fact *domainadminaudit.Fact) error {
	if r.appendErr != nil {
		return r.appendErr
	}
	r.appended = append(r.appended, fact)
	return nil
}

func (r *auditRepositoryStub) List(_ context.Context, query domainadminaudit.Query) ([]*domainadminaudit.Fact, error) {
	r.queries = append(r.queries, query)
	return append([]*domainadminaudit.Fact(nil), r.items...), nil
}

type auditLoggerStub struct {
	lines []string
}

func (l *auditLoggerStub) Printf(format string, values ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, values...))
}

func TestAuditFactBuildersSetExplicitOutcomes(t *testing.T) {
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	input := BuildInput{
		ActorID: 9, Permission: domainaccount.PermissionContentEnforce,
		Action: domainadminaudit.ActionContentEnforce, TargetType: domainadminaudit.TargetVideo,
		TargetID: "video-9", RequestID: applicationAuditTestRequestID,
		Detail: map[string]string{
			"http_method": "POST", "route": "/api/admin/videos/:videoId/enforcement",
			"reason_code": "policy_violation", "previous_status": "published", "new_status": "offline",
		},
	}
	success, err := BuildSuccessFact(input, now)
	if err != nil {
		t.Fatalf("BuildSuccessFact() error = %v", err)
	}
	deniedInput := input
	deniedInput.Detail = map[string]string{
		"http_method": "POST",
		"reason_code": "permission_denied",
		"route":       "/api/admin/videos/:videoId/enforcement",
	}
	denied, err := BuildDeniedAttemptFact(deniedInput, now)
	if err != nil {
		t.Fatalf("BuildDeniedAttemptFact() error = %v", err)
	}
	if success.Outcome() != domainadminaudit.OutcomeSuccess ||
		denied.Outcome() != domainadminaudit.OutcomeDenied {
		t.Fatalf("unexpected outcomes: success=%q denied=%q", success.Outcome(), denied.Outcome())
	}
}

func TestRecordDeniedAttemptIsBestEffortAndLogged(t *testing.T) {
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	logger := &auditLoggerStub{}
	repository := &auditRepositoryStub{appendErr: errors.New("write failed")}
	service := New(repository, WithClock(func() time.Time { return now }), WithLogger(logger))
	service.RecordDeniedAttempt(context.Background(), BuildInput{
		ActorID: 4, Permission: domainaccount.PermissionAuditRead,
		Action: domainadminaudit.ActionAuditQuery, TargetType: domainadminaudit.TargetAuditTrail,
		TargetID: "events", RequestID: applicationAuditTestRequestID,
		Detail: map[string]string{
			"http_method": "GET", "reason_code": "permission_denied", "route": "/api/admin/audit-events",
		},
	})
	if len(repository.appended) != 0 {
		t.Fatal("failed denied attempt must not look persisted")
	}
	if len(logger.lines) != 1 || !strings.Contains(logger.lines[0], "write failed") {
		t.Fatalf("unexpected log lines: %#v", logger.lines)
	}
}

func TestQueryBindsCursorToFilters(t *testing.T) {
	base := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	repository := &auditRepositoryStub{items: []*domainadminaudit.Fact{
		mustApplicationAuditFact(t, 3, base.Add(3*time.Minute)),
		mustApplicationAuditFact(t, 2, base.Add(2*time.Minute)),
		mustApplicationAuditFact(t, 1, base.Add(time.Minute)),
	}}
	service := New(repository)
	request := QueryRequest{
		ActorID: 7, Action: string(domainadminaudit.ActionContentEnforce),
		TargetType: string(domainadminaudit.TargetVideo), Outcome: string(domainadminaudit.OutcomeSuccess),
		From: base, To: base.Add(time.Hour), Limit: 2,
	}
	page, err := service.Query(context.Background(), request)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if !page.HasMore || len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatalf("unexpected page: %+v", page)
	}
	request.Cursor = page.NextCursor
	if _, err := service.Query(context.Background(), request); err != nil {
		t.Fatalf("Query() next page error = %v", err)
	}
	if len(repository.queries) != 2 || repository.queries[1].Cursor == nil ||
		repository.queries[1].Cursor.EventID != page.Items[1].ID() {
		t.Fatalf("cursor not forwarded: %#v", repository.queries)
	}

	request.ActorID = 8
	if _, err := service.Query(context.Background(), request); !errors.Is(err, domainadminaudit.ErrInvalidCursor) {
		t.Fatalf("changed-filter cursor error = %v, want ErrInvalidCursor", err)
	}
}

func TestQueryRejectsUnboundedOrInvalidFilters(t *testing.T) {
	base := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	service := New(&auditRepositoryStub{})
	tests := []struct {
		name    string
		request QueryRequest
		err     error
	}{
		{name: "missing range", request: QueryRequest{}, err: domainadminaudit.ErrInvalidTimeRange},
		{name: "reversed range", request: QueryRequest{From: base.Add(time.Hour), To: base}, err: domainadminaudit.ErrInvalidTimeRange},
		{name: "range too large", request: QueryRequest{From: base, To: base.Add(domainadminaudit.MaxQueryRange + time.Second)}, err: domainadminaudit.ErrTimeRangeTooLarge},
		{name: "invalid action", request: QueryRequest{From: base, To: base.Add(time.Hour), Action: "custom"}, err: domainadminaudit.ErrInvalidAction},
		{name: "invalid limit", request: QueryRequest{From: base, To: base.Add(time.Hour), Limit: domainadminaudit.MaxQueryLimit + 1}, err: domainadminaudit.ErrInvalidLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := service.Query(context.Background(), tt.request); !errors.Is(err, tt.err) {
				t.Fatalf("Query() error = %v, want %v", err, tt.err)
			}
		})
	}
}

func mustApplicationAuditFact(t *testing.T, id int64, createdAt time.Time) *domainadminaudit.Fact {
	t.Helper()
	fact, err := domainadminaudit.RestoreFact(id, domainadminaudit.FactInput{
		ActorID: 7, Permission: domainaccount.PermissionContentEnforce,
		Action: domainadminaudit.ActionContentEnforce, TargetType: domainadminaudit.TargetVideo,
		TargetID: fmt.Sprintf("video-%d", id), Outcome: domainadminaudit.OutcomeSuccess,
		RequestID: applicationAuditTestRequestID,
		Detail: map[string]string{
			"http_method": "POST", "route": "/api/admin/videos/:videoId/enforcement",
			"reason_code": "policy_violation", "previous_status": "published", "new_status": "offline",
		},
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("restore audit fact: %v", err)
	}
	return fact
}
