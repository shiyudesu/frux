import { describe, expect, it } from "vitest";
import type { Comment, DeleteCommentResponse } from "../types";
import {
  applyCommentLikeRollback,
  applyConfirmedCommentLike,
  applyCommentViewerTransition,
  applyCreatedComment,
  applyDeletedComment,
  applyOptimisticCommentLike,
  beginCommentRequest,
  createCommentsStore,
  createVideoCommentsState,
  isCurrentCommentRequest,
  isValidCommentThreadContext,
  mergeCommentIDs,
  setCommentDraftState,
  setCommentExpandedState,
  setCommentReplyTargetState,
  setCommentSortState,
  unicodeLength
} from "./useComments";
import type { CommentsStore } from "./useComments";

describe("threaded comment state", () => {
  it("deduplicates page merges and isolates sorting, drafts, targets, and expansion by video", () => {
    expect(mergeCommentIDs([1, 2], [2, 3])).toEqual([1, 2, 3]);
    let store = createCommentsStore();
    store = setCommentSortState(store, 1, "latest");
    store = setCommentDraftState(store, 1, "视频一");
    store = setCommentDraftState(store, 2, "视频二");
    store = setCommentReplyTargetState(store, 1, 9);
    store = setCommentExpandedState(store, 1, 7, true);
    store = setCommentExpandedState(store, 1, 7, true);

    expect(store.videos[1]?.sort).toBe("latest");
    expect(store.videos[1]?.draft).toBe("视频一");
    expect(store.videos[2]?.draft).toBe("视频二");
    expect(store.videos[1]?.replyTargetID).toBe(9);
    expect(store.videos[1]?.expandedRootIDs).toEqual([7]);
  });

  it("merges root and reply creation without duplicate counters", () => {
    const root = comment(1);
    let store: CommentsStore = {
      ...createCommentsStore(),
      entities: { 1: root },
      videos: { 8: createVideoCommentsState() }
    };
    store = applyCreatedComment(store, 8, root);
    store = applyCreatedComment(store, 8, root);
    expect(store.videos[8]?.roots.hot.ids).toEqual([]);
    expect(store.videos[8]?.roots.latest.ids).toEqual([1]);

    const reply = comment(2, { root_comment_id: 1, reply_to_comment_id: 1, content: "回复" });
    store = applyCreatedComment(store, 8, reply);
    expect(store.videos[8]?.replies[1]?.ids).toEqual([]);
    expect(store.videos[8]?.pendingReplyIDs[1]).toEqual([2]);
    expect(store.videos[8]?.expandedRootIDs).toEqual([1]);
    expect(store.entities[1]?.reply_count).toBe(1);
  });

  it("rolls optimistic likes back exactly and clears viewer permissions on logout", () => {
    const root = comment(1, { liked: false, like_count: 4, can_delete: true });
    let store: CommentsStore = {
      ...createCommentsStore(),
      entities: { 1: root },
      videos: { 8: createVideoCommentsState() }
    };
    store = applyOptimisticCommentLike(store, 8, 1, true);
    expect(store.entities[1]).toMatchObject({ liked: true, like_count: 5 });
    store = applyCommentLikeRollback(store, 8, 1, false, 4, "失败");
    expect(store.entities[1]).toMatchObject({ liked: false, like_count: 4 });
    expect(store.videos[8]?.likes[1]).toEqual({ busy: false, error: "失败" });

    store = applyCommentViewerTransition({
      ...store,
      entities: { 1: { ...root, liked: true } },
      videos: {
        8: {
          ...store.videos[8]!,
          roots: {
            ...store.videos[8]!.roots,
            hot: {
              ids: [1],
              nextCursor: "next",
              hasMore: true,
              state: "ready",
              error: ""
            }
          }
        }
      }
    });

    expect(store.entities[1]).toMatchObject({ liked: false, can_delete: false });
    expect(store.videos[8]?.roots.hot).toMatchObject({ ids: [], state: "idle" });
    expect(store.videos[8]?.draft).toBe("");
    expect(store.videos[8]?.replyTargetID).toBe(0);
  });

  it("applies the confirmed video-author like marker only to the target comment", () => {
    const first = comment(1);
    const second = comment(2, { liked_by_video_author: true });
    const store: CommentsStore = {
      ...createCommentsStore(),
      entities: { 1: first, 2: second },
      videos: { 8: createVideoCommentsState() }
    };
    const updated = applyConfirmedCommentLike(store, 8, 1, {
      comment_id: 1,
      root_comment_id: 1,
      liked: true,
      like_count: 1,
      liked_by_video_author: true
    });
    expect(updated.entities[1]).toMatchObject({
      liked: true,
      like_count: 1,
      liked_by_video_author: true
    });
    expect(updated.entities[2]).toBe(second);
    expect(updated.videos[8]?.likes[1]).toEqual({ busy: false, error: "" });
  });

  it("invalidates request identities across authentication generations", () => {
    const generations: Record<string, number> = {};
    const authenticatedRequest = beginCommentRequest(generations, "roots:8:hot", 0);
    expect(isCurrentCommentRequest(generations, authenticatedRequest, 0)).toBe(true);
    expect(isCurrentCommentRequest(generations, authenticatedRequest, 1)).toBe(false);

    const anonymousRequest = beginCommentRequest(generations, "roots:8:hot", 1);
    expect(isCurrentCommentRequest(generations, authenticatedRequest, 1)).toBe(false);
    expect(isCurrentCommentRequest(generations, anonymousRequest, 1)).toBe(true);
  });

  it("accepts only route-consistent direct thread contexts", () => {
    const root = comment(7);
    const target = comment(9, { root_comment_id: 7 });
    const context = {
      root,
      replies: [comment(8, { root_comment_id: 7 })],
      target,
      next_cursor: "next",
      has_more: true,
      comment_count: 3
    };
    expect(isValidCommentThreadContext(context, 8, 9, 7)).toBe(true);
    expect(isValidCommentThreadContext(context, 99, 9, 7)).toBe(false);
    expect(isValidCommentThreadContext(context, 8, 9, 6)).toBe(false);
    expect(isValidCommentThreadContext({
      ...context,
      target: { ...target, root_comment_id: 6 }
    }, 8, 9, 7)).toBe(false);
  });

  it("preserves server ordering when locally creating roots and replies", () => {
    const oldRoot = comment(1);
    const newRoot = comment(2, { created_at: "2026-08-04T00:00:00Z" });
    let store: CommentsStore = {
      entities: { 1: oldRoot },
      videos: {
        8: {
          ...createVideoCommentsState(),
          roots: {
            hot: { ids: [1], nextCursor: "hot-next", hasMore: true, state: "ready", error: "" },
            latest: { ids: [1], nextCursor: "", hasMore: false, state: "ready", error: "" }
          },
          replies: {
            1: { ids: [3], nextCursor: "reply-next", hasMore: true, state: "ready", error: "" }
          }
        }
      }
    };
    store = applyCreatedComment(store, 8, newRoot);
    expect(store.videos[8]?.roots.latest.ids).toEqual([2, 1]);
    expect(store.videos[8]?.roots.hot).toMatchObject({ ids: [], state: "idle" });

    const newReply = comment(4, { root_comment_id: 1, created_at: "2026-08-05T00:00:00Z" });
    store = applyCreatedComment(store, 8, newReply);
    expect(store.videos[8]?.replies[1]?.ids).toEqual([3]);
    expect(store.videos[8]?.pendingReplyIDs[1]).toEqual([4]);

    const idleStore = applyCreatedComment(
      {
        entities: { 1: oldRoot },
        videos: { 8: createVideoCommentsState() }
      },
      8,
      newReply
    );
    expect(idleStore.videos[8]?.replies[1]?.state).toBe("idle");
    expect(idleStore.videos[8]?.pendingReplyIDs[1]).toEqual([4]);
  });

  it("applies reply deletion, root tombstones, and moderator thread removal", () => {
    const root = comment(1, { reply_count: 1 });
    const reply = comment(2, { root_comment_id: 1 });
    let store: CommentsStore = {
      entities: { 1: root, 2: reply },
      videos: {
        8: {
          ...createVideoCommentsState(),
          roots: {
            hot: { ids: [1], nextCursor: "", hasMore: false, state: "ready" as const, error: "" },
            latest: { ids: [1], nextCursor: "", hasMore: false, state: "ready" as const, error: "" }
          },
          replies: {
            1: { ids: [2], nextCursor: "", hasMore: false, state: "ready" as const, error: "" }
          }
        }
      }
    };
    store = applyDeletedComment(store, 8, reply, deletion({ comment_id: 2, root_reply_count: 0 }));
    expect(store.entities[2]).toBeUndefined();
    expect(store.entities[1]?.reply_count).toBe(0);

    store = applyDeletedComment(store, 8, root, deletion({ tombstone: true, status: 2 }));
    expect(store.entities[1]).toMatchObject({ deleted: true, content: "", user_id: 0 });

    const moderatedStore = applyDeletedComment(
      {
        entities: { 1: root, 2: reply },
        videos: store.videos
      },
      8,
      root,
      deletion({ thread_hidden: true, deleted_count: 2, status: 3 })
    );
    expect(moderatedStore.entities[1]).toBeUndefined();
    expect(moderatedStore.entities[2]).toBeUndefined();
  });

  it("scrubs tombstoned root identity from retained direct replies", () => {
    const reply = comment(2, {
      root_comment_id: 1,
      reply_to_comment_id: 1,
      reply_to_user_id: 7,
      reply_to_user_account: "deleted-author",
      reply_to_user_nickname: "被删除作者",
      reply_to_user_avatar_url: "/deleted.jpg"
    });
    const root = comment(1, {
      user_account: "deleted-author",
      is_video_author: true,
      liked_by_video_author: true,
      reply_count: 1,
      reply_previews: [reply]
    });
    const store: CommentsStore = {
      entities: { 1: root, 2: reply },
      videos: {
        8: {
          ...createVideoCommentsState(),
          replyTargetID: 1,
          roots: {
            hot: { ids: [1], nextCursor: "", hasMore: false, state: "ready", error: "" },
            latest: { ids: [1], nextCursor: "", hasMore: false, state: "ready", error: "" }
          },
          replies: {
            1: { ids: [2], nextCursor: "", hasMore: false, state: "ready", error: "" }
          }
        }
      }
    };
    const updated = applyDeletedComment(
      store, 8, root, deletion({ tombstone: true, status: 2, root_reply_count: 1 })
    );
    expect(updated.entities[1]).toMatchObject({
      user_id: 0,
      user_account: "",
      is_video_author: false,
      liked_by_video_author: false
    });
    expect(updated.entities[1]?.reply_previews[0]).toMatchObject({
      reply_to_user_id: 0,
      reply_to_user_account: "",
      reply_to_user_nickname: "",
      reply_to_user_avatar_url: ""
    });
    expect(updated.entities[2]).toMatchObject({
      reply_to_user_id: 0,
      reply_to_user_account: "",
      reply_to_user_nickname: "",
      reply_to_user_avatar_url: ""
    });
    expect(updated.videos[8]?.replyTargetID).toBe(0);
  });

  it("scrubs a deleted reply identity from descendant replies and previews", () => {
    const deletedReply = comment(2, { root_comment_id: 1, reply_to_comment_id: 1 });
    const descendant = comment(3, {
      root_comment_id: 1,
      reply_to_comment_id: 2,
      reply_to_user_id: 8,
      reply_to_user_account: "deleted-reply",
      reply_to_user_nickname: "被删回复者",
      reply_to_user_avatar_url: "/deleted-reply.jpg"
    });
    const root = comment(1, {
      reply_count: 2,
      reply_previews: [deletedReply, descendant]
    });
    const store: CommentsStore = {
      entities: { 1: root, 2: deletedReply, 3: descendant },
      videos: {
        8: {
          ...createVideoCommentsState(),
          roots: {
            hot: { ids: [1], nextCursor: "", hasMore: false, state: "ready", error: "" },
            latest: { ids: [1], nextCursor: "", hasMore: false, state: "ready", error: "" }
          },
          replies: {
            1: { ids: [2, 3], nextCursor: "", hasMore: false, state: "ready", error: "" }
          }
        }
      }
    };
    const updated = applyDeletedComment(
      store, 8, deletedReply, deletion({ comment_id: 2, root_reply_count: 1 })
    );
    expect(updated.entities[2]).toBeUndefined();
    expect(updated.entities[3]).toMatchObject({
      reply_to_user_id: 0,
      reply_to_user_account: "",
      reply_to_user_nickname: "",
      reply_to_user_avatar_url: ""
    });
    expect(updated.entities[1]?.reply_previews.find((item) => item.id === 3)).toMatchObject({
      reply_to_user_id: 0,
      reply_to_user_account: "",
      reply_to_user_nickname: "",
      reply_to_user_avatar_url: ""
    });
  });

  it("counts Unicode code points rather than UTF-8 bytes", () => {
    expect(unicodeLength("评论🙂")).toBe(3);
  });
});

function comment(id: number, patch: Partial<Comment> = {}): Comment {
  return {
    id,
    video_id: 8,
    user_id: 2,
    user_account: "user",
    user_nickname: "用户",
    user_avatar_url: "",
    root_comment_id: 0,
    reply_to_comment_id: 0,
    reply_to_user_id: 0,
    reply_to_user_account: "",
    reply_to_user_nickname: "",
    reply_to_user_avatar_url: "",
    content: "内容",
    status: 1,
    deleted: false,
    reply_count: 0,
    reply_previews: [],
    like_count: 0,
    liked: false,
    can_delete: true,
    is_video_author: false,
    liked_by_video_author: false,
    hot_score: 0,
    created_at: "2026-08-03T00:00:00Z",
    ...patch
  };
}

function deletion(patch: Partial<DeleteCommentResponse>): DeleteCommentResponse {
  return {
    comment_id: 1,
    status: 2,
    comment_count: 0,
    root_reply_count: 0,
    deleted_count: 1,
    thread_hidden: false,
    tombstone: false,
    ...patch
  };
}
