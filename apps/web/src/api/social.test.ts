import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createComment,
  createCommentReply,
  deleteComment,
  fetchCommentReplies,
  fetchComments,
  fetchCommentThread,
  relationListPath,
  setCommentLike
} from "./social";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("threaded comment API", () => {
  it("builds typed root, reply, and direct-thread read requests", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ items: [], next_cursor: "", has_more: false, comment_count: 0, sort: "hot" }))
      .mockResolvedValueOnce(jsonResponse({ root_comment_id: 7, items: [], next_cursor: "", has_more: false, comment_count: 0 }))
      .mockResolvedValueOnce(jsonResponse({
        root: comment(7),
        replies: [],
        target: comment(7),
        next_cursor: "",
        has_more: false,
        comment_count: 1
      }));
    vi.stubGlobal("fetch", fetchMock);

    await fetchComments(3, "hot", "root-cursor", 20, "token");
    await fetchCommentReplies(7, "reply-cursor", 10, "token");
    await fetchCommentThread(9, 15, "token");

    expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/videos/3/comments?sort=hot&limit=20&cursor=root-cursor");
    expect(fetchMock.mock.calls[1]?.[0]).toBe("/api/comments/7/replies?limit=10&cursor=reply-cursor");
    expect(fetchMock.mock.calls[2]?.[0]).toBe("/api/comments/9/thread?limit=15");
  });

  describe("relation list API", () => {
    it("binds optional following search without changing existing callers", () => {
      expect(relationListPath("following", "cursor", 50, " maker "))
        .toBe("/api/users/me/following?limit=50&cursor=cursor&q=maker");
      expect(relationListPath("followers"))
        .toBe("/api/users/me/followers?limit=20");
    });
  });

  it("sends retry-safe create, like, unlike, and delete mutations", async () => {
    const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(jsonResponse(comment(11))));
    vi.stubGlobal("fetch", fetchMock);

    await createComment("token", 3, "root", "root-key");
    await createCommentReply("token", 3, 11, "reply", "reply-key");
    await setCommentLike("token", 11, true, "like-key");
    await setCommentLike("token", 11, false, "unlike-key");
    await deleteComment("token", 11, "delete-key");

    expect(request(fetchMock, 0)).toMatchObject({
      method: "POST",
      headers: expect.objectContaining({ "Idempotency-Key": "root-key" }),
      body: JSON.stringify({ content: "root" })
    });
    expect(fetchMock.mock.calls[1]?.[0]).toBe("/api/videos/3/comments/11/replies");
    expect(request(fetchMock, 1).headers).toEqual(expect.objectContaining({ "Idempotency-Key": "reply-key" }));
    expect(request(fetchMock, 2)).toMatchObject({ method: "PUT" });
    expect(request(fetchMock, 3)).toMatchObject({ method: "DELETE" });
    expect(request(fetchMock, 4)).toMatchObject({
      method: "DELETE",
      headers: expect.objectContaining({ "Idempotency-Key": "delete-key" })
    });
  });
});

function request(mock: ReturnType<typeof vi.fn>, index: number): RequestInit {
  return mock.mock.calls[index]?.[1] as RequestInit;
}

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), {
    status: 200,
    headers: { "Content-Type": "application/json" }
  });
}

function comment(id: number) {
  return {
    id,
    video_id: 3,
    user_id: 2,
    user_nickname: "作者",
    user_avatar_url: "",
    root_comment_id: 0,
    reply_to_comment_id: 0,
    reply_to_user_id: 0,
    reply_to_user_nickname: "",
    reply_to_user_avatar_url: "",
    content: "内容",
    status: 1,
    deleted: false,
    reply_count: 0,
    reply_previews: [],
    like_count: 0,
    liked: false,
    can_delete: false,
    hot_score: 0,
    created_at: "2026-08-03T00:00:00Z"
  };
}
