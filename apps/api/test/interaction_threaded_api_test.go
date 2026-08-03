package test

import (
	domaininteraction "GCFeed/internal/domain/interaction"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
)

func TestThreadedCommentAPIFlow(t *testing.T) {
	router, jwtManager, repo := newInteractionRouterWithRepo(t)
	videoAuthorToken := signTestToken(t, jwtManager, 42)
	rootAuthorToken := signTestToken(t, jwtManager, 77)
	otherToken := signTestToken(t, jwtManager, 99)

	root := createThreadedCommentForTest(t, router, "/api/videos/1001/comments", `{"content":"root one"}`, rootAuthorToken, "thread-root-1")
	secondRoot := createThreadedCommentForTest(t, router, "/api/videos/1001/comments", `{"content":"root two"}`, otherToken, "thread-root-2")
	reply1 := createThreadedCommentForTest(t, router, "/api/videos/1001/comments/1/replies", `{"content":"reply one"}`, otherToken, "thread-reply-1")
	reply2 := createThreadedCommentForTest(t, router, "/api/videos/1001/comments/3/replies", `{"content":"reply to reply"}`, videoAuthorToken, "thread-reply-2")
	_ = createThreadedCommentForTest(t, router, "/api/videos/1001/comments/1/replies", `{"content":"reply three"}`, otherToken, "thread-reply-3")
	_ = createThreadedCommentForTest(t, router, "/api/videos/1001/comments/1/replies", `{"content":"reply four"}`, videoAuthorToken, "thread-reply-4")
	_ = createThreadedCommentForTest(t, router, "/api/videos/1001/comments/2/replies", `{"content":"second thread reply"}`, rootAuthorToken, "thread-reply-5")

	if reply1.RootCommentID != root.ID || reply1.ReplyToCommentID != root.ID {
		t.Fatalf("root reply metadata is incorrect: %+v", reply1)
	}
	if reply2.RootCommentID != root.ID || reply2.ReplyToCommentID != reply1.ID || reply2.ReplyToUserID != reply1.UserID {
		t.Fatalf("reply-to-reply was not flattened with its direct target: %+v", reply2)
	}

	likeResponse := performVideoJSONRequest(router, http.MethodPut, "/api/comments/1/like", "", videoAuthorToken, "comment-like-1")
	requireStatus(t, likeResponse, http.StatusOK)
	var liked interactionCommentLikeAPIResponse
	decodeJSON(t, likeResponse, &liked)
	if !liked.Liked || liked.LikeCount != 1 || liked.RootCommentID != root.ID {
		t.Fatalf("unexpected comment like response: %+v", liked)
	}
	replayLike := performVideoJSONRequest(router, http.MethodPut, "/api/comments/1/like", "", videoAuthorToken, "comment-like-1")
	requireStatus(t, replayLike, http.StatusOK)
	conflictingLike := performVideoJSONRequest(router, http.MethodDelete, "/api/comments/1/like", "", videoAuthorToken, "comment-like-1")
	requireStatus(t, conflictingLike, http.StatusConflict)

	hotResponse := performJSONRequest(router, http.MethodGet, "/api/videos/1001/comments?sort=hot&limit=1", "", videoAuthorToken)
	requireStatus(t, hotResponse, http.StatusOK)
	var hotPage interactionCommentListAPIResponse
	decodeJSON(t, hotResponse, &hotPage)
	if hotPage.Sort != domaininteraction.CommentSortHot || len(hotPage.Items) != 1 ||
		hotPage.Items[0].ID != root.ID || hotPage.Items[0].ReplyCount != 4 ||
		len(hotPage.Items[0].ReplyPreviews) != domaininteraction.ReplyPreviewLimit ||
		hotPage.Items[0].HotScore != 23 || !hotPage.Items[0].Liked || !hotPage.Items[0].CanDelete {
		t.Fatalf("unexpected hydrated hot page: %+v", hotPage)
	}

	anonymousResponse := performJSONRequest(router, http.MethodGet, "/api/videos/1001/comments?sort=hot&limit=1", "", "")
	requireStatus(t, anonymousResponse, http.StatusOK)
	var anonymousPage interactionCommentListAPIResponse
	decodeJSON(t, anonymousResponse, &anonymousPage)
	if anonymousPage.Items[0].Liked || anonymousPage.Items[0].CanDelete {
		t.Fatalf("anonymous viewer received private state: %+v", anonymousPage.Items[0])
	}

	latestResponse := performJSONRequest(router, http.MethodGet, "/api/videos/1001/comments?limit=1", "", "")
	requireStatus(t, latestResponse, http.StatusOK)
	var latestPage interactionCommentListAPIResponse
	decodeJSON(t, latestResponse, &latestPage)
	if latestPage.Sort != domaininteraction.CommentSortLatest || latestPage.Items[0].ID != secondRoot.ID || latestPage.NextCursor == "" {
		t.Fatalf("latest compatibility default is incorrect: %+v", latestPage)
	}
	crossSort := performJSONRequest(
		router, http.MethodGet, "/api/videos/1001/comments?sort=hot&cursor="+latestPage.NextCursor, "", "",
	)
	requireStatus(t, crossSort, http.StatusBadRequest)

	firstRepliesResponse := performJSONRequest(router, http.MethodGet, "/api/comments/1/replies?limit=2", "", videoAuthorToken)
	requireStatus(t, firstRepliesResponse, http.StatusOK)
	var firstReplies interactionReplyListAPIResponse
	decodeJSON(t, firstRepliesResponse, &firstReplies)
	if len(firstReplies.Items) != 2 || firstReplies.Items[0].ID != reply1.ID ||
		firstReplies.Items[1].ID != reply2.ID || !firstReplies.HasMore || firstReplies.NextCursor == "" {
		t.Fatalf("unexpected first reply page: %+v", firstReplies)
	}
	secondRepliesResponse := performJSONRequest(
		router, http.MethodGet, "/api/comments/1/replies?limit=2&cursor="+firstReplies.NextCursor, "", videoAuthorToken,
	)
	requireStatus(t, secondRepliesResponse, http.StatusOK)
	var secondReplies interactionReplyListAPIResponse
	decodeJSON(t, secondRepliesResponse, &secondReplies)
	if len(secondReplies.Items) != 2 || secondReplies.Items[0].ID == firstReplies.Items[0].ID {
		t.Fatalf("reply cursor did not advance without duplicates: %+v", secondReplies)
	}

	threadResponse := performJSONRequest(router, http.MethodGet, "/api/comments/4/thread?limit=2", "", videoAuthorToken)
	requireStatus(t, threadResponse, http.StatusOK)
	var thread interactionThreadContextAPIResponse
	decodeJSON(t, threadResponse, &thread)
	if thread.Root.ID != root.ID || thread.Target.ID != reply2.ID || thread.Target.ReplyToCommentID != reply1.ID {
		t.Fatalf("direct thread context is incorrect: %+v", thread)
	}

	selfDelete := performJSONRequest(router, http.MethodDelete, "/api/comments/1", "", rootAuthorToken)
	requireStatus(t, selfDelete, http.StatusOK)
	var deleted interactionDeleteCommentAPIResponse
	decodeJSON(t, selfDelete, &deleted)
	if deleted.Status != domaininteraction.CommentStatusSelfDeleted || !deleted.Tombstone || deleted.CommentCount != 6 {
		t.Fatalf("unexpected root self-deletion result: %+v", deleted)
	}
	tombstoneResponse := performJSONRequest(router, http.MethodGet, "/api/videos/1001/comments?sort=hot", "", "")
	requireStatus(t, tombstoneResponse, http.StatusOK)
	var tombstonePage interactionCommentListAPIResponse
	decodeJSON(t, tombstoneResponse, &tombstonePage)
	if tombstonePage.Items[0].ID != root.ID || tombstonePage.Items[0].UserID != 0 ||
		tombstonePage.Items[0].Content != "" || !tombstonePage.Items[0].Deleted {
		t.Fatalf("self-deleted root did not project as a safe tombstone: %+v", tombstonePage.Items[0])
	}

	repo.setVideoVisibilityForTest(1001, "private")
	hiddenList := performJSONRequest(router, http.MethodGet, "/api/videos/1001/comments", "", "")
	requireStatus(t, hiddenList, http.StatusNotFound)
	hiddenReply := performVideoJSONRequest(router, http.MethodPost, "/api/videos/1001/comments/3/replies", `{"content":"blocked"}`, rootAuthorToken, "hidden-reply")
	requireStatus(t, hiddenReply, http.StatusNotFound)
	hiddenLike := performVideoJSONRequest(router, http.MethodPut, "/api/comments/3/like", "", rootAuthorToken, "hidden-like")
	requireStatus(t, hiddenLike, http.StatusNotFound)

	deleteAfterPrivacy := performJSONRequest(router, http.MethodDelete, "/api/comments/3", "", otherToken)
	requireStatus(t, deleteAfterPrivacy, http.StatusOK)
	moderateAfterPrivacy := performJSONRequest(router, http.MethodDelete, "/api/comments/2", "", videoAuthorToken)
	requireStatus(t, moderateAfterPrivacy, http.StatusOK)
	var moderated interactionDeleteCommentAPIResponse
	decodeJSON(t, moderateAfterPrivacy, &moderated)
	if moderated.Status != domaininteraction.CommentStatusModerated || !moderated.ThreadHidden || moderated.DeletedCount != 2 {
		t.Fatalf("moderation did not cascade after privacy change: %+v", moderated)
	}
}

func createThreadedCommentForTest(t *testing.T, router *server.Hertz, path string, body string, token string, key string) interactionCommentAPIResponse {
	t.Helper()
	response := performVideoJSONRequest(router, http.MethodPost, path, body, token, key)
	requireStatus(t, response, http.StatusCreated)
	var comment interactionCommentAPIResponse
	decodeJSON(t, response, &comment)
	return comment
}
