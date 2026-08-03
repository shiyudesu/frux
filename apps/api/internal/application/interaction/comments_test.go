package applicationinteraction

import (
	domaininteraction "GCFeed/internal/domain/interaction"
	"context"
	"errors"
	"testing"
	"time"
)

type threadedCommentRepositoryStub struct {
	synchronousActionRepositoryStub
	createThreadedComment    func(*domaininteraction.Comment) (*domaininteraction.CommentMutationResult, error)
	listCommentRoots         func(domaininteraction.CommentRootQuery) (*domaininteraction.CommentPage, error)
	listCommentReplies       func(domaininteraction.CommentReplyQuery) (*domaininteraction.CommentPage, error)
	getCommentThreadContext  func(int64, domaininteraction.CommentViewer, int) (*domaininteraction.CommentThreadContext, error)
	setCommentLike           func(int64, int64, bool, string) (*domaininteraction.CommentLikeResult, error)
	deleteThreadedComment    func(int64, int64, string) (*domaininteraction.CommentDeletionResult, error)
	reconcileCommentCounters func() error
}

func (r *threadedCommentRepositoryStub) CreateThreadedComment(_ context.Context, comment *domaininteraction.Comment) (*domaininteraction.CommentMutationResult, error) {
	return r.createThreadedComment(comment)
}

func (r *threadedCommentRepositoryStub) ListCommentRoots(_ context.Context, query domaininteraction.CommentRootQuery) (*domaininteraction.CommentPage, error) {
	if r.listCommentRoots == nil {
		return &domaininteraction.CommentPage{}, nil
	}
	return r.listCommentRoots(query)
}

func (r *threadedCommentRepositoryStub) ListCommentReplies(_ context.Context, query domaininteraction.CommentReplyQuery) (*domaininteraction.CommentPage, error) {
	if r.listCommentReplies == nil {
		return &domaininteraction.CommentPage{}, nil
	}
	return r.listCommentReplies(query)
}

func (r *threadedCommentRepositoryStub) GetCommentThreadContext(_ context.Context, targetCommentID int64, viewer domaininteraction.CommentViewer, limit int) (*domaininteraction.CommentThreadContext, error) {
	if r.getCommentThreadContext == nil {
		return &domaininteraction.CommentThreadContext{}, nil
	}
	return r.getCommentThreadContext(targetCommentID, viewer, limit)
}

func (r *threadedCommentRepositoryStub) SetCommentLike(_ context.Context, commentID int64, userID int64, active bool, key string) (*domaininteraction.CommentLikeResult, error) {
	if r.setCommentLike == nil {
		return &domaininteraction.CommentLikeResult{}, nil
	}
	return r.setCommentLike(commentID, userID, active, key)
}

func (r *threadedCommentRepositoryStub) DeleteThreadedComment(_ context.Context, commentID int64, userID int64, role string) (*domaininteraction.CommentDeletionResult, error) {
	return r.deleteThreadedComment(commentID, userID, role)
}

func (r *threadedCommentRepositoryStub) ReconcileCommentCounters(context.Context) error {
	if r.reconcileCommentCounters == nil {
		return nil
	}
	return r.reconcileCommentCounters()
}

type commentHotScoreRecorder struct {
	deltas []int
}

func (r *commentHotScoreRecorder) AddHotScore(_ context.Context, _ int64, delta int, _ time.Time) error {
	r.deltas = append(r.deltas, delta)
	return nil
}

func TestRootCursorRejectsCrossSortReuse(t *testing.T) {
	cursor := encodeCommentCursor(&domaininteraction.CommentCursor{
		Version:   domaininteraction.CommentCursorVersion,
		Sort:      domaininteraction.CommentSortLatest,
		CreatedAt: time.Now().UTC(),
		CommentID: 10,
	})
	if _, err := parseRootCommentCursor(cursor, domaininteraction.CommentSortHot); !errors.Is(err, domaininteraction.ErrInvalidCursor) {
		t.Fatalf("expected cross-sort cursor rejection, got %v", err)
	}
	if parsed, err := parseRootCommentCursor(cursor, domaininteraction.CommentSortLatest); err != nil ||
		parsed.Sort != domaininteraction.CommentSortLatest || parsed.CommentID != 10 {
		t.Fatalf("latest cursor did not round trip: parsed=%+v err=%v", parsed, err)
	}
}

func TestApplicationResolvesAndFlattensReplies(t *testing.T) {
	now := time.Now().UTC()
	comments := map[int64]*domaininteraction.Comment{
		10: domaininteraction.RestoreThreadedComment(
			10, 7, 20, "root", "", 0, 0, 0, "", "", "root",
			domaininteraction.CommentStatusNormal, 1, 0, 5, "", "", false, true, now, now,
		),
		11: domaininteraction.RestoreThreadedComment(
			11, 7, 21, "reply", "", 10, 10, 20, "root", "", "first reply",
			domaininteraction.CommentStatusNormal, 0, 0, 0, "", "", false, true, now, now,
		),
	}
	nextID := int64(12)
	repo := &threadedCommentRepositoryStub{}
	repo.createThreadedComment = func(input *domaininteraction.Comment) (*domaininteraction.CommentMutationResult, error) {
		target := comments[input.ReplyToCommentID]
		if target == nil || target.Status != domaininteraction.CommentStatusNormal {
			return nil, domaininteraction.ErrReplyTargetUnavailable
		}
		rootID := target.EffectiveRootCommentID()
		resolved := *input
		resolved.ID = nextID
		nextID++
		resolved.RootCommentID = rootID
		resolved.ReplyToUserID = target.UserID
		resolved.RequestFingerprint = domaininteraction.CommentRequestFingerprint(
			input.VideoID, rootID, input.ReplyToCommentID, input.Content,
		)
		resolved.CreatedAt = now
		resolved.UpdatedAt = now
		comments[resolved.ID] = &resolved
		return &domaininteraction.CommentMutationResult{
			Comment: &resolved, CommentCount: len(comments), VideoDelta: 1, RootReplyDelta: 1,
		}, nil
	}
	recorder := &commentHotScoreRecorder{}
	service := New(repo, WithHotScoreRecorder(recorder))

	rootReply, err := service.CreateReply(context.Background(), 22, 7, 10, " first ", "reply-root")
	if err != nil {
		t.Fatalf("reply to root: %v", err)
	}
	if rootReply.Comment.RootCommentID != 10 || rootReply.Comment.ReplyToCommentID != 10 ||
		rootReply.Comment.ReplyToUserID != 20 {
		t.Fatalf("root reply was not resolved: %+v", rootReply.Comment)
	}
	if rootReply.Comment.RequestFingerprint != domaininteraction.CommentRequestFingerprint(7, 10, 10, "first") {
		t.Fatalf("resolved root was not included in the stored fingerprint: %q", rootReply.Comment.RequestFingerprint)
	}

	nestedReply, err := service.CreateReply(context.Background(), 23, 7, 11, "nested", "reply-nested")
	if err != nil {
		t.Fatalf("reply to reply: %v", err)
	}
	if nestedReply.Comment.RootCommentID != 10 || nestedReply.Comment.ReplyToCommentID != 11 ||
		nestedReply.Comment.ReplyToUserID != 21 {
		t.Fatalf("reply-to-reply was not flattened: %+v", nestedReply.Comment)
	}
	if len(recorder.deltas) != 2 || recorder.deltas[0] != 5 || recorder.deltas[1] != 5 {
		t.Fatalf("reply hot-score deltas = %v, want [5 5]", recorder.deltas)
	}
}

func TestApplicationMapsAllThreadedDeletionModesAndHotScoreDeltas(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name       string
		deletion   *domaininteraction.CommentDeletionResult
		wantDelta  int
		wantStatus int
		tombstone  bool
		hidden     bool
	}{
		{
			name:      "self root with replies",
			deletion:  deletionResult(1, 0, domaininteraction.CommentStatusSelfDeleted, 4, 2, -1, 1, true, false, now),
			wantDelta: -5, wantStatus: domaininteraction.CommentStatusSelfDeleted, tombstone: true,
		},
		{
			name:      "self root without replies",
			deletion:  deletionResult(2, 0, domaininteraction.CommentStatusSelfDeleted, 3, 0, -1, 1, false, false, now),
			wantDelta: -5, wantStatus: domaininteraction.CommentStatusSelfDeleted,
		},
		{
			name:      "moderator root cascade",
			deletion:  deletionResult(3, 0, domaininteraction.CommentStatusModerated, 0, 0, -3, 3, false, true, now),
			wantDelta: -15, wantStatus: domaininteraction.CommentStatusModerated, hidden: true,
		},
		{
			name:      "author deletes reply",
			deletion:  deletionResult(4, 1, domaininteraction.CommentStatusSelfDeleted, 2, 1, -1, 1, false, false, now),
			wantDelta: -5, wantStatus: domaininteraction.CommentStatusSelfDeleted,
		},
		{
			name:      "moderator deletes reply",
			deletion:  deletionResult(5, 1, domaininteraction.CommentStatusModerated, 1, 0, -1, 1, false, false, now),
			wantDelta: -5, wantStatus: domaininteraction.CommentStatusModerated,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &threadedCommentRepositoryStub{
				deleteThreadedComment: func(int64, int64, string) (*domaininteraction.CommentDeletionResult, error) {
					return test.deletion, nil
				},
			}
			recorder := &commentHotScoreRecorder{}
			service := New(repo, WithHotScoreRecorder(recorder))
			result, err := service.DeleteComment(context.Background(), test.deletion.Comment.ID, 99, "user")
			if err != nil {
				t.Fatalf("delete comment: %v", err)
			}
			if result.Status != test.wantStatus || result.Tombstone != test.tombstone ||
				result.ThreadHidden != test.hidden || result.DeletedCount != test.deletion.DeletedCount ||
				result.RootReplyCount != test.deletion.RootReplyCount {
				t.Fatalf("unexpected application deletion result: %+v", result)
			}
			if len(recorder.deltas) != 1 || recorder.deltas[0] != test.wantDelta {
				t.Fatalf("deletion hot-score deltas = %v, want [%d]", recorder.deltas, test.wantDelta)
			}
		})
	}
}

func deletionResult(
	id int64,
	rootID int64,
	status int,
	commentCount int,
	rootReplyCount int,
	videoDelta int,
	deletedCount int,
	tombstone bool,
	hidden bool,
	now time.Time,
) *domaininteraction.CommentDeletionResult {
	return &domaininteraction.CommentDeletionResult{
		Comment: domaininteraction.RestoreThreadedComment(
			id, 7, 20, "", "", rootID, rootID, 0, "", "", "comment", status,
			rootReplyCount, 0, 0, "", "", false, false, now, now,
		),
		CommentCount: commentCount, RootReplyCount: rootReplyCount, VideoDelta: videoDelta,
		DeletedCount: deletedCount, Tombstone: tombstone, ThreadHidden: hidden,
	}
}
