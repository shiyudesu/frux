// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fetchPublicProfile, fetchVideo } from "../api/account";
import { ApiError } from "../api/client";
import { fetchMessages, fetchUnreadStat, markMessagesRead } from "../api/messages";
import {
  fetchCommentReplies,
  fetchComments,
  fetchCommentThread
} from "../api/social";
import { emptyProfile, TOKEN_KEY, USER_KEY } from "../constants";
import { RouterProvider, useRoute, useVideoDiscussionRoute } from "../router";
import { SessionProvider } from "../session";
import type { Message } from "../types";
import { MessagesPage } from "./MessagesPage";
import { VideoDetailPage } from "./VideoDetailPage";

vi.mock("../api/account", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/account")>()),
  fetchPublicProfile: vi.fn(),
  fetchVideo: vi.fn()
}));

vi.mock("../api/messages", () => ({
  fetchMessages: vi.fn(),
  fetchUnreadStat: vi.fn(),
  markMessagesRead: vi.fn()
}));

vi.mock("../api/social", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/social")>()),
  fetchCommentReplies: vi.fn(),
  fetchComments: vi.fn(),
  fetchCommentThread: vi.fn()
}));

describe("comment message navigation", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    localStorage.setItem(TOKEN_KEY, "message-token");
    localStorage.setItem(USER_KEY, JSON.stringify({ ...emptyProfile, id: 2, nickname: "收件人", role: "user" }));
    window.history.replaceState({}, "", "/messages");
    vi.mocked(fetchUnreadStat).mockResolvedValue({ unread_count: 1 });
    vi.mocked(markMessagesRead).mockResolvedValue({ updated_count: 1 });
    vi.mocked(fetchVideo).mockResolvedValue({
      id: 3,
      author_id: 4,
      title: "仍可观看的视频",
      description: "视频仍然公开",
      media_url: "/video.mp4",
      cover_url: "/cover.jpg",
      status: 2,
      visibility: "public",
      like_count: 0,
      comment_count: 0,
      favorite_count: 0,
      created_at: "2026-08-03T00:00:00Z",
      updated_at: "2026-08-03T00:00:00Z"
    });
    vi.mocked(fetchPublicProfile).mockResolvedValue({
      id: 4,
      account: "author",
      nickname: "视频作者",
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
    vi.mocked(fetchComments).mockResolvedValue({
      items: [],
      next_cursor: "",
      has_more: false,
      comment_count: 0,
      sort: "hot"
    });
    vi.mocked(fetchCommentReplies).mockResolvedValue({
      root_comment_id: 7,
      items: [],
      next_cursor: "",
      has_more: false,
      comment_count: 0
    });
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    localStorage.clear();
    vi.clearAllMocks();
  });

  it("renders a typed reply message, marks it read, and navigates to its discussion", async () => {
    vi.mocked(fetchMessages).mockResolvedValue(messagePage([targetedMessage()]));
    await renderMessages();
    expect(container.textContent).toContain("新回复");
    expect(container.textContent).toContain("查看讨论");

    await clickAsync(required<HTMLButtonElement>(".message-item"));
    expect(markMessagesRead).toHaveBeenCalledWith("message-token", [1]);
    expect(window.location.pathname).toBe("/videos/3");
    expect(window.location.search).toBe("?comment=7&highlight=9");
    expect(required<HTMLElement>('[data-testid="destination"]').textContent).toBe(
      "/videos/3?comment=7&highlight=9"
    );
  });

  it("marks a valid removed target read, navigates, and keeps readable video context safe", async () => {
    vi.mocked(fetchMessages).mockResolvedValue(messagePage([targetedMessage()]));
    vi.mocked(fetchCommentThread).mockRejectedValue(new ApiError("评论不存在", 404));

    await renderMessages(true);
    await clickAsync(required<HTMLButtonElement>(".message-item"));

    expect(markMessagesRead).toHaveBeenCalledWith("message-token", [1]);
    expect(window.location.pathname).toBe("/videos/3");
    expect(window.location.search).toBe("?comment=7&highlight=9");
    expect(fetchCommentThread).toHaveBeenCalledWith(9, 20, "message-token");
    expect(fetchVideo).toHaveBeenCalledWith(3);
    expect(vi.mocked(markMessagesRead).mock.invocationCallOrder[0]).toBeLessThan(
      vi.mocked(fetchVideo).mock.invocationCallOrder[0]!
    );
    expect(required<HTMLElement>('[data-ui="video-detail-page"]')).toBeTruthy();
    expect(container.textContent).toContain("仍可观看的视频");
    expect(container.textContent).toContain("该讨论已不可用");
    expect(container.textContent).not.toContain("回复内容");
    expect(container.querySelector('[data-comment-id="9"]')).toBeNull();
  });

  it("marks a legacy target read without authoring an invalid route", async () => {
    const legacy = {
      ...targetedMessage(),
      id: 3,
      type: "COMMENT" as const,
      video_id: undefined,
      root_comment_id: undefined,
      comment_id: undefined
    };
    vi.mocked(fetchMessages).mockResolvedValue(messagePage([legacy]));
    await renderMessages();
    const items = [...container.querySelectorAll<HTMLButtonElement>(".message-item")];
    expect(items).toHaveLength(1);
    expect(items.every((item) => item.classList.contains("read-only"))).toBe(true);

    await clickAsync(items[0]!);
    expect(markMessagesRead).toHaveBeenCalledWith("message-token", [3]);
    expect(window.location.pathname).toBe("/messages");
    expect(container.textContent).toContain("已读");
  });

  async function renderMessages(renderVideoDetail = false) {
    await act(async () => {
      root.render(
        <RouterProvider>
          <SessionProvider>
            <MessagesRouteHarness renderVideoDetail={renderVideoDetail} />
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

function MessagesRouteHarness({ renderVideoDetail }: { renderVideoDetail: boolean }) {
  const route = useRoute();
  const discussion = useVideoDiscussionRoute();
  if (route === "/messages") {
    return <MessagesPage />;
  }
  if (renderVideoDetail && discussion) {
    return <VideoDetailPage {...discussion} />;
  }
  return <output data-testid="destination">{route}{window.location.search}</output>;
}

function targetedMessage(): Message {
  return {
    id: 1,
    user_id: 2,
    type: "COMMENT_REPLY",
    title: "有人回复了你",
    content: "回复内容",
    video_id: 3,
    root_comment_id: 7,
    comment_id: 9,
    is_read: false,
    created_at: "2026-08-03T00:00:00Z"
  };
}

function messagePage(items: Message[]) {
  return {
    items,
    next_cursor: "",
    has_more: false
  };
}

async function clickAsync(element: HTMLElement) {
  await act(async () => {
    element.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await flushPromises();
  });
}

async function flushPromises() {
  for (let index = 0; index < 10; index += 1) {
    await Promise.resolve();
  }
}
