package test

import (
	domaininteraction "github.com/shiyudesu/frux/internal/domain/interaction"
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
	replayedRoot := createThreadedCommentForTest(t, router, "/api/videos/1001/comments", `{"content":" root one "}`, rootAuthorToken, "thread-root-1")
	if replayedRoot.ID != root.ID || replayedRoot.CommentCount != secondRoot.CommentCount {
		t.Fatalf("compatible root replay changed the result: original=%+v replay=%+v", root, replayedRoot)
	}
	rootConflict := performVideoJSONRequest(
		router, http.MethodPost, "/api/videos/1001/comments", `{"content":"changed root"}`, rootAuthorToken, "thread-root-1",
	)
	requireStatus(t, rootConflict, http.StatusConflict)
	reply1 := createThreadedCommentForTest(t, router, "/api/videos/1001/comments/1/replies", `{"content":"reply one"}`, otherToken, "thread-reply-1")
	reply2 := createThreadedCommentForTest(t, router, "/api/videos/1001/comments/3/replies", `{"content":"reply to reply"}`, videoAuthorToken, "thread-reply-2")
	reply3 := createThreadedCommentForTest(t, router, "/api/videos/1001/comments/1/replies", `{"content":"reply three"}`, otherToken, "thread-reply-3")
	reply4 := createThreadedCommentForTest(t, router, "/api/videos/1001/comments/1/replies", `{"content":"reply four"}`, videoAuthorToken, "thread-reply-4")
	_ = createThreadedCommentForTest(t, router, "/api/videos/1001/comments/2/replies", `{"content":"second thread reply"}`, rootAuthorToken, "thread-reply-5")

	if reply1.RootCommentID != root.ID || reply1.ReplyToCommentID != root.ID {
		t.Fatalf("root reply metadata is incorrect: %+v", reply1)
	}
	if reply2.RootCommentID != root.ID || reply2.ReplyToCommentID != reply1.ID || reply2.ReplyToUserID != reply1.UserID {
		t.Fatalf("reply-to-reply was not flattened with its direct target: %+v", reply2)
	}
	if !reply2.IsVideoAuthor || reply2.UserAccount != memoryInteractionAccount(42) ||
		reply2.ReplyToUserAccount != memoryInteractionAccount(reply1.UserID) {
		t.Fatalf("video author reply identity markers are incorrect: %+v", reply2)
	}

	likeResponse := performVideoJSONRequest(router, http.MethodPut, "/api/comments/1/like", "", videoAuthorToken, "comment-like-1")
	requireStatus(t, likeResponse, http.StatusOK)
	var liked interactionCommentLikeAPIResponse
	decodeJSON(t, likeResponse, &liked)
	if !liked.Liked || liked.LikeCount != 1 || liked.RootCommentID != root.ID ||
		!liked.LikedByVideoAuthor {
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
		hotPage.Items[0].ReplyPreviews[0].ID != reply1.ID ||
		hotPage.Items[0].HotScore != 23 || !hotPage.Items[0].Liked || !hotPage.Items[0].CanDelete ||
		hotPage.Items[0].UserAccount != memoryInteractionAccount(root.UserID) ||
		!hotPage.Items[0].LikedByVideoAuthor ||
		!hotPage.HasMore || hotPage.NextCursor == "" {
		t.Fatalf("unexpected hydrated hot page: %+v", hotPage)
	}
	hotNextResponse := performJSONRequest(
		router, http.MethodGet, "/api/videos/1001/comments?sort=hot&limit=1&cursor="+hotPage.NextCursor, "", "",
	)
	requireStatus(t, hotNextResponse, http.StatusOK)
	var hotNext interactionCommentListAPIResponse
	decodeJSON(t, hotNextResponse, &hotNext)
	if len(hotNext.Items) != 1 || hotNext.Items[0].ID != secondRoot.ID {
		t.Fatalf("hot cursor did not advance independently: %+v", hotNext)
	}

	anonymousResponse := performJSONRequest(router, http.MethodGet, "/api/videos/1001/comments?sort=hot&limit=1", "", "")
	requireStatus(t, anonymousResponse, http.StatusOK)
	var anonymousPage interactionCommentListAPIResponse
	decodeJSON(t, anonymousResponse, &anonymousPage)
	if anonymousPage.Items[0].Liked || anonymousPage.Items[0].CanDelete ||
		!anonymousPage.Items[0].LikedByVideoAuthor {
		t.Fatalf("anonymous viewer received private state: %+v", anonymousPage.Items[0])
	}
	rootAuthorResponse := performJSONRequest(router, http.MethodGet, "/api/videos/1001/comments?sort=hot&limit=1", "", rootAuthorToken)
	requireStatus(t, rootAuthorResponse, http.StatusOK)
	var rootAuthorPage interactionCommentListAPIResponse
	decodeJSON(t, rootAuthorResponse, &rootAuthorPage)
	if rootAuthorPage.Items[0].Liked || !rootAuthorPage.Items[0].CanDelete {
		t.Fatalf("comment author permissions were not returned: %+v", rootAuthorPage.Items[0])
	}
	otherViewerResponse := performJSONRequest(router, http.MethodGet, "/api/videos/1001/comments?sort=hot&limit=1", "", otherToken)
	requireStatus(t, otherViewerResponse, http.StatusOK)
	var otherViewerPage interactionCommentListAPIResponse
	decodeJSON(t, otherViewerResponse, &otherViewerPage)
	if otherViewerPage.Items[0].CanDelete {
		t.Fatalf("ordinary viewer received delete permission: %+v", otherViewerPage.Items[0])
	}

	latestResponse := performJSONRequest(router, http.MethodGet, "/api/videos/1001/comments?limit=1", "", "")
	requireStatus(t, latestResponse, http.StatusOK)
	var latestPage interactionCommentListAPIResponse
	decodeJSON(t, latestResponse, &latestPage)
	if latestPage.Sort != domaininteraction.CommentSortLatest || latestPage.Items[0].ID != secondRoot.ID || latestPage.NextCursor == "" {
		t.Fatalf("latest compatibility default is incorrect: %+v", latestPage)
	}
	latestNextResponse := performJSONRequest(
		router, http.MethodGet, "/api/videos/1001/comments?sort=latest&limit=1&cursor="+latestPage.NextCursor, "", "",
	)
	requireStatus(t, latestNextResponse, http.StatusOK)
	var latestNext interactionCommentListAPIResponse
	decodeJSON(t, latestNextResponse, &latestNext)
	if len(latestNext.Items) != 1 || latestNext.Items[0].ID != root.ID ||
		latestNext.Items[0].ID == latestPage.Items[0].ID || latestNext.HasMore {
		t.Fatalf("latest cursor pages were not exact and disjoint: first=%+v next=%+v", latestPage, latestNext)
	}
	crossSort := performJSONRequest(
		router, http.MethodGet, "/api/videos/1001/comments?sort=hot&cursor="+latestPage.NextCursor, "", "",
	)
	requireStatus(t, crossSort, http.StatusBadRequest)
	reverseCrossSort := performJSONRequest(
		router, http.MethodGet, "/api/videos/1001/comments?sort=latest&cursor="+hotPage.NextCursor, "", "",
	)
	requireStatus(t, reverseCrossSort, http.StatusBadRequest)

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
	if len(secondReplies.Items) != 2 ||
		secondReplies.Items[0].ID != reply3.ID ||
		secondReplies.Items[1].ID != reply4.ID ||
		secondReplies.HasMore ||
		secondReplies.Items[0].ID == firstReplies.Items[0].ID ||
		secondReplies.Items[0].ID == firstReplies.Items[1].ID ||
		secondReplies.Items[1].ID == firstReplies.Items[0].ID ||
		secondReplies.Items[1].ID == firstReplies.Items[1].ID {
		t.Fatalf("reply cursor pages were not exact, ordered, and disjoint: first=%+v next=%+v", firstReplies, secondReplies)
	}

	threadResponse := performJSONRequest(router, http.MethodGet, "/api/comments/4/thread?limit=2", "", videoAuthorToken)
	requireStatus(t, threadResponse, http.StatusOK)
	var thread interactionThreadContextAPIResponse
	decodeJSON(t, threadResponse, &thread)
	if thread.Root.ID != root.ID || thread.Target.ID != reply2.ID || thread.Target.ReplyToCommentID != reply1.ID {
		t.Fatalf("direct thread context is incorrect: %+v", thread)
	}

	unlikeResponse := performVideoJSONRequest(router, http.MethodDelete, "/api/comments/1/like", "", videoAuthorToken, "comment-unlike-1")
	requireStatus(t, unlikeResponse, http.StatusOK)
	var unliked interactionCommentLikeAPIResponse
	decodeJSON(t, unlikeResponse, &unliked)
	if unliked.Liked || unliked.LikeCount != 0 || unliked.LikedByVideoAuthor {
		t.Fatalf("unexpected comment unlike response: %+v", unliked)
	}
	replayOriginalLikeAfterUnlike := performVideoJSONRequest(
		router, http.MethodPut, "/api/comments/1/like", "",
		videoAuthorToken, "comment-like-1",
	)
	requireStatus(t, replayOriginalLikeAfterUnlike, http.StatusOK)
	var replayedOriginalLike interactionCommentLikeAPIResponse
	decodeJSON(t, replayOriginalLikeAfterUnlike, &replayedOriginalLike)
	if !replayedOriginalLike.Liked || replayedOriginalLike.LikeCount != 1 ||
		!replayedOriginalLike.LikedByVideoAuthor {
		t.Fatalf("original like replay changed its response: %+v", replayedOriginalLike)
	}
	replayUnlike := performVideoJSONRequest(router, http.MethodDelete, "/api/comments/1/like", "", videoAuthorToken, "comment-unlike-1")
	requireStatus(t, replayUnlike, http.StatusOK)
	conflictingUnlike := performVideoJSONRequest(router, http.MethodPut, "/api/comments/1/like", "", videoAuthorToken, "comment-unlike-1")
	requireStatus(t, conflictingUnlike, http.StatusConflict)

	forbiddenDelete := performJSONRequest(router, http.MethodDelete, "/api/comments/1", "", otherToken)
	requireStatus(t, forbiddenDelete, http.StatusForbidden)

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
	for _, item := range tombstonePage.Items {
		if item.ID == root.ID && (item.UserID != 0 || item.UserAccount != "" ||
			item.IsVideoAuthor || item.LikedByVideoAuthor) {
			t.Fatalf("tombstone leaked identity markers: %+v", item)
		}
	}
	if tombstonePage.Items[0].ID != root.ID || tombstonePage.Items[0].UserID != 0 ||
		tombstonePage.Items[0].Content != "" || !tombstonePage.Items[0].Deleted {
		t.Fatalf("self-deleted root did not project as a safe tombstone: %+v", tombstonePage.Items[0])
	}

	repo.setVideoVisibilityForTest(1001, "private")
	hiddenList := performJSONRequest(router, http.MethodGet, "/api/videos/1001/comments", "", "")
	requireStatus(t, hiddenList, http.StatusNotFound)
	hiddenReply := performVideoJSONRequest(router, http.MethodPost, "/api/videos/1001/comments/3/replies", `{"content":"blocked"}`, rootAuthorToken, "hidden-reply")
	requireStatus(t, hiddenReply, http.StatusNotFound)
	hiddenRoot := performVideoJSONRequest(router, http.MethodPost, "/api/videos/1001/comments", `{"content":"blocked root"}`, rootAuthorToken, "hidden-root")
	requireStatus(t, hiddenRoot, http.StatusNotFound)
	hiddenLike := performVideoJSONRequest(router, http.MethodPut, "/api/comments/3/like", "", rootAuthorToken, "hidden-like")
	requireStatus(t, hiddenLike, http.StatusNotFound)
	hiddenThread := performJSONRequest(router, http.MethodGet, "/api/comments/4/thread", "", rootAuthorToken)
	requireStatus(t, hiddenThread, http.StatusNotFound)

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

func TestThreadedCommentDeletionAPIFlow(t *testing.T) {
	router, jwtManager, _ := newInteractionRouterWithRepo(t)
	videoAuthorToken := signTestToken(t, jwtManager, 42)
	rootAuthorToken := signTestToken(t, jwtManager, 77)
	replyAuthorToken := signTestToken(t, jwtManager, 99)
	otherToken := signTestToken(t, jwtManager, 55)

	rootWithoutReplies := createThreadedCommentForTest(
		t, router, "/api/videos/1001/comments", `{"content":"single root"}`, rootAuthorToken, "delete-single-root",
	)
	selfDeletedRoot := performJSONRequest(router, http.MethodDelete, "/api/comments/1", "", rootAuthorToken)
	requireStatus(t, selfDeletedRoot, http.StatusOK)
	var singleDeletion interactionDeleteCommentAPIResponse
	decodeJSON(t, selfDeletedRoot, &singleDeletion)
	if singleDeletion.CommentID != rootWithoutReplies.ID ||
		singleDeletion.Status != domaininteraction.CommentStatusSelfDeleted ||
		singleDeletion.Tombstone || singleDeletion.ThreadHidden || singleDeletion.CommentCount != 0 {
		t.Fatalf("unexpected root-without-replies deletion: %+v", singleDeletion)
	}
	replayedSingleDeletion := performJSONRequest(router, http.MethodDelete, "/api/comments/1", "", rootAuthorToken)
	requireStatus(t, replayedSingleDeletion, http.StatusOK)
	var replayedSingle interactionDeleteCommentAPIResponse
	decodeJSON(t, replayedSingleDeletion, &replayedSingle)
	if replayedSingle.CommentCount != 0 || replayedSingle.DeletedCount != 0 {
		t.Fatalf("repeated root deletion changed counters: %+v", replayedSingle)
	}
	emptyListResponse := performJSONRequest(router, http.MethodGet, "/api/videos/1001/comments", "", "")
	requireStatus(t, emptyListResponse, http.StatusOK)
	var emptyList interactionCommentListAPIResponse
	decodeJSON(t, emptyListResponse, &emptyList)
	if len(emptyList.Items) != 0 {
		t.Fatalf("self-deleted root without replies remained visible: %+v", emptyList)
	}

	rootWithReplies := createThreadedCommentForTest(
		t, router, "/api/videos/1001/comments", `{"content":"thread root"}`, rootAuthorToken, "delete-thread-root",
	)
	firstReply := createThreadedCommentForTest(
		t, router, "/api/videos/1001/comments/2/replies", `{"content":"first reply"}`, replyAuthorToken, "delete-first-reply",
	)
	_ = createThreadedCommentForTest(
		t, router, "/api/videos/1001/comments/2/replies", `{"content":"second reply"}`, otherToken, "delete-second-reply",
	)
	replyDeletion := performJSONRequest(router, http.MethodDelete, "/api/comments/3", "", replyAuthorToken)
	requireStatus(t, replyDeletion, http.StatusOK)
	var deletedReply interactionDeleteCommentAPIResponse
	decodeJSON(t, replyDeletion, &deletedReply)
	if deletedReply.CommentID != firstReply.ID ||
		deletedReply.Status != domaininteraction.CommentStatusSelfDeleted ||
		deletedReply.ThreadHidden || deletedReply.RootReplyCount != 1 || deletedReply.CommentCount != 2 {
		t.Fatalf("reply self-deletion affected the wrong scope: %+v", deletedReply)
	}

	tombstoneResponse := performJSONRequest(router, http.MethodDelete, "/api/comments/2", "", rootAuthorToken)
	requireStatus(t, tombstoneResponse, http.StatusOK)
	var tombstone interactionDeleteCommentAPIResponse
	decodeJSON(t, tombstoneResponse, &tombstone)
	if tombstone.CommentID != rootWithReplies.ID || !tombstone.Tombstone ||
		tombstone.ThreadHidden || tombstone.RootReplyCount != 1 || tombstone.CommentCount != 1 {
		t.Fatalf("root self-deletion did not preserve the active reply: %+v", tombstone)
	}

	cascadeResponse := performJSONRequest(router, http.MethodDelete, "/api/comments/2", "", videoAuthorToken)
	requireStatus(t, cascadeResponse, http.StatusOK)
	var cascade interactionDeleteCommentAPIResponse
	decodeJSON(t, cascadeResponse, &cascade)
	if cascade.Status != domaininteraction.CommentStatusModerated || !cascade.ThreadHidden ||
		cascade.Tombstone || cascade.DeletedCount != 1 || cascade.CommentCount != 0 {
		t.Fatalf("moderation of a tombstoned root did not cascade: %+v", cascade)
	}

	moderatedReplyRoot := createThreadedCommentForTest(
		t, router, "/api/videos/1001/comments", `{"content":"moderated reply root"}`, rootAuthorToken, "moderate-reply-root",
	)
	moderatedReply := createThreadedCommentForTest(
		t, router, "/api/videos/1001/comments/5/replies", `{"content":"moderated reply"}`, replyAuthorToken, "moderate-reply",
	)
	forbidden := performJSONRequest(router, http.MethodDelete, "/api/comments/5", "", otherToken)
	requireStatus(t, forbidden, http.StatusForbidden)
	moderatedReplyResponse := performJSONRequest(router, http.MethodDelete, "/api/comments/6", "", videoAuthorToken)
	requireStatus(t, moderatedReplyResponse, http.StatusOK)
	var moderatedReplyDeletion interactionDeleteCommentAPIResponse
	decodeJSON(t, moderatedReplyResponse, &moderatedReplyDeletion)
	if moderatedReplyDeletion.CommentID != moderatedReply.ID ||
		moderatedReplyDeletion.Status != domaininteraction.CommentStatusModerated ||
		moderatedReplyDeletion.ThreadHidden || moderatedReplyDeletion.RootReplyCount != 0 ||
		moderatedReplyDeletion.CommentCount != 1 {
		t.Fatalf("moderator reply deletion hid the root thread: %+v", moderatedReplyDeletion)
	}
	rootListResponse := performJSONRequest(router, http.MethodGet, "/api/videos/1001/comments", "", "")
	requireStatus(t, rootListResponse, http.StatusOK)
	var rootList interactionCommentListAPIResponse
	decodeJSON(t, rootListResponse, &rootList)
	if len(rootList.Items) != 1 || rootList.Items[0].ID != moderatedReplyRoot.ID ||
		rootList.Items[0].ReplyCount != 0 {
		t.Fatalf("root did not remain after isolated reply moderation: %+v", rootList)
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
