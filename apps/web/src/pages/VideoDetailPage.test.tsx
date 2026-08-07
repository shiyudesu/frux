// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fetchPublicProfile, fetchVideo } from "../api/account";
import { fetchUnreadStat } from "../api/messages";
import {
  fetchCommentReplies,
  fetchComments,
  fetchCommentThread
} from "../api/social";
import { RouterProvider } from "../router";
import { SessionProvider } from "../session";
import type { Comment } from "../types";
import { VideoDetailPage } from "./VideoDetailPage";

vi.mock("../api/account", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/account")>()),
  fetchPublicProfile: vi.fn(),
  fetchVideo: vi.fn()
}));

vi.mock("../api/messages", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/messages")>()),
  fetchUnreadStat: vi.fn()
}));

vi.mock("../api/social", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/social")>()),
  fetchCommentReplies: vi.fn(),
  fetchComments: vi.fn(),
  fetchCommentThread: vi.fn()
}));

describe("video discussion deep links", () => {
  let container: HTMLDivElement;
  let root: Root;
  let scrollIntoView: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    window.history.replaceState({}, "", "/videos/3?comment=7&highlight=9");
    localStorage.clear();
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: vi.fn((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: () => {},
        removeListener: () => {},
        addEventListener: () => {},
        removeEventListener: () => {},
        dispatchEvent: () => true
      }))
    });
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      callback(0);
      return 1;
    });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
    scrollIntoView = vi.fn();
    Object.defineProperty(window.HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: scrollIntoView
    });
    vi.mocked(fetchUnreadStat).mockResolvedValue({ unread_count: 0 });
    vi.mocked(fetchVideo).mockResolvedValue({
      id: 3,
      author_id: 2,
      title: "目标视频",
      description: "说明",
      media_url: "/cover.jpg",
      cover_url: "/cover.jpg",
      status: 2,
      visibility: "public",
      like_count: 1,
      comment_count: 2,
      favorite_count: 0,
      created_at: "2026-08-03T00:00:00Z",
      updated_at: "2026-08-03T00:00:00Z"
    });
    vi.mocked(fetchPublicProfile).mockResolvedValue({
      id: 2,
      account: "author",
      nickname: "作者",
      avatar_url: "",
      bio: "",
      following_count: 0,
      follower_count: 0,
      work_count: 1,
      gender: 0,
      public_work_count: 1,
      received_like_count: 0,
      collection_count: 0,
      liked_videos_public: false
    });
    vi.mocked(fetchCommentReplies).mockResolvedValue({
      root_comment_id: 7,
      items: [],
      next_cursor: "",
      has_more: false,
      comment_count: 2
    });
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.clearAllMocks();
    vi.restoreAllMocks();
  });

  it.each([
    { name: "root", highlightID: 7 },
    { name: "reply", highlightID: 9 }
  ])("loads and highlights a focused $name target", async ({ highlightID }) => {
    const rootComment = comment(7);
    const reply = comment(9, {
      root_comment_id: 7,
      reply_to_comment_id: 7,
      reply_to_user_id: 2,
      reply_to_user_nickname: "作者",
      content: "目标回复"
    });
    const target = highlightID === rootComment.id ? rootComment : reply;
    vi.mocked(fetchComments).mockResolvedValue({
      items: [rootComment],
      next_cursor: "",
      has_more: false,
      comment_count: 2,
      sort: "hot"
    });
    vi.mocked(fetchCommentThread).mockResolvedValue({
      root: rootComment,
      replies: [reply],
      target,
      next_cursor: "",
      has_more: false,
      comment_count: 2
    });

    await renderDetail(7, highlightID, false);
    const focused = required<HTMLElement>(`[data-comment-id="${highlightID}"]`);
    expect(focused.classList.contains("focused")).toBe(true);
    expect(container.textContent).toContain("目标回复");
    expect(scrollIntoView).toHaveBeenCalledWith({ block: "center", behavior: "smooth" });
    expect(fetchCommentThread).toHaveBeenCalledWith(highlightID, 20, "");
  });

  it("keeps the video rendered and shows unavailable feedback for a removed target", async () => {
    const rootComment = comment(7);
    vi.mocked(fetchComments).mockResolvedValue({
      items: [rootComment],
      next_cursor: "",
      has_more: false,
      comment_count: 1,
      sort: "hot"
    });
    vi.mocked(fetchCommentThread).mockRejectedValue(new Error("removed"));

    await renderDetail(7, 9, false);
    expect(required<HTMLElement>('[data-ui="video-detail-page"]')).toBeTruthy();
    expect(container.textContent).toContain("该讨论已不可用");
    expect(container.textContent).toContain("目标视频");
    expect(container.querySelector('[data-comment-id="9"]')).toBeNull();
  });

  it("renders malformed search feedback without requesting a hidden target", async () => {
    const rootComment = comment(7);
    vi.mocked(fetchComments).mockResolvedValue({
      items: [rootComment],
      next_cursor: "",
      has_more: false,
      comment_count: 1,
      sort: "hot"
    });

    await renderDetail(7, 9, true);
    expect(container.textContent).toContain("评论链接参数无效，已显示可用讨论。");
    expect(fetchCommentThread).not.toHaveBeenCalled();
  });

  async function renderDetail(commentID: number, highlightID: number, invalidFocus: boolean) {
    await act(async () => {
      root.render(
        <RouterProvider>
          <SessionProvider>
            <VideoDetailPage
              videoID={3}
              commentID={commentID}
              highlightID={highlightID}
              invalidFocus={invalidFocus}
            />
          </SessionProvider>
        </RouterProvider>
      );
      await flushPromises();
    });
  }

  function required<T extends Element>(selector: string): T {
    const element = container.querySelector<T>(selector);
    if (!element) throw new Error(`missing element: ${selector}`);
    return element;
  }
});

function comment(id: number, patch: Partial<Comment> = {}): Comment {
  return {
    id,
    video_id: 3,
    user_id: 2,
    user_account: "author",
    user_nickname: "作者",
    user_avatar_url: "",
    root_comment_id: 0,
    reply_to_comment_id: 0,
    reply_to_user_id: 0,
    reply_to_user_account: "",
    reply_to_user_nickname: "",
    reply_to_user_avatar_url: "",
    content: "根评论",
    status: 1,
    deleted: false,
    reply_count: 1,
    reply_previews: [],
    like_count: 0,
    liked: false,
    can_delete: false,
    is_video_author: false,
    liked_by_video_author: false,
    hot_score: 5,
    created_at: "2026-08-03T00:00:00Z",
    ...patch
  };
}

async function flushPromises() {
  for (let index = 0; index < 8; index += 1) {
    await Promise.resolve();
  }
}
