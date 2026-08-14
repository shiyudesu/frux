// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SearchUserPage, SearchVideoPage, Video } from "../types";
import { ApiError, NetworkError } from "../api/client";
import { searchErrorMessage, useSearch } from "./useSearch";

const searchAPI = vi.hoisted(() => ({
  searchUsers: vi.fn(),
  searchVideos: vi.fn()
}));

vi.mock("../api/search", () => searchAPI);

describe("global search state", () => {
  let container: HTMLDivElement;
  let root: Root;
  let current: ReturnType<typeof useSearch>;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    searchAPI.searchVideos.mockReset();
    searchAPI.searchUsers.mockReset();
    searchAPI.searchVideos.mockResolvedValue({ items: [], next_cursor: "", has_more: false });
    searchAPI.searchUsers.mockResolvedValue({ items: [], next_cursor: "", has_more: false });
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  it("retains independently loaded video and user tabs", async () => {
    render("猫");
    searchAPI.searchVideos.mockResolvedValueOnce({
      items: [video(1)],
      next_cursor: "",
      has_more: false
    } satisfies SearchVideoPage);
    searchAPI.searchUsers.mockResolvedValueOnce({
      items: [user(2)],
      next_cursor: "",
      has_more: false
    } satisfies SearchUserPage);
    await act(async () => {
      await current.load("videos", true);
      await current.load("users", true);
    });
    expect(current.videos.items.map((item) => item.id)).toEqual([1]);
    expect(current.users.items.map((item) => item.id)).toEqual([2]);
  });

  it("continues video search with the returned cursor and appends unique results", async () => {
    render("猫");
    searchAPI.searchVideos
      .mockResolvedValueOnce({
        items: [video(1), video(2)],
        next_cursor: "video-next",
        has_more: true
      } satisfies SearchVideoPage)
      .mockResolvedValueOnce({
        items: [video(2), video(3)],
        next_cursor: "",
        has_more: false
      } satisfies SearchVideoPage);

    await act(async () => {
      await current.load("videos", true);
    });
    expect(current.videos).toMatchObject({
      nextCursor: "video-next",
      hasMore: true,
      state: "ready"
    });

    await act(async () => {
      await current.load("videos");
    });
    expect(searchAPI.searchVideos).toHaveBeenNthCalledWith(1, "猫", "");
    expect(searchAPI.searchVideos).toHaveBeenNthCalledWith(2, "猫", "video-next");
    expect(current.videos.items.map((item) => item.id)).toEqual([1, 2, 3]);
    expect(current.videos.nextCursor).toBe("");
    expect(current.videos.hasMore).toBe(false);
  });

  it("continues user search without changing the independently loaded video page", async () => {
    render("猫");
    searchAPI.searchVideos.mockResolvedValueOnce({
      items: [video(1)],
      next_cursor: "video-next",
      has_more: true
    } satisfies SearchVideoPage);
    searchAPI.searchUsers
      .mockResolvedValueOnce({
        items: [user(10), user(11)],
        next_cursor: "user-next",
        has_more: true
      } satisfies SearchUserPage)
      .mockResolvedValueOnce({
        items: [user(11), user(12)],
        next_cursor: "",
        has_more: false
      } satisfies SearchUserPage);

    await act(async () => {
      await current.load("videos", true);
      await current.load("users", true);
    });
    await act(async () => {
      await current.load("users");
    });

    expect(searchAPI.searchUsers).toHaveBeenNthCalledWith(1, "猫", "");
    expect(searchAPI.searchUsers).toHaveBeenNthCalledWith(2, "猫", "user-next");
    expect(current.users.items.map((item) => item.id)).toEqual([10, 11, 12]);
    expect(current.users.hasMore).toBe(false);
    expect(current.videos.items.map((item) => item.id)).toEqual([1]);
    expect(current.videos.nextCursor).toBe("video-next");
    expect(current.videos.hasMore).toBe(true);
  });

  it("ignores a stale response after the route query changes", async () => {
    const oldRequest = deferred<SearchVideoPage>();
    const newRequest = deferred<SearchVideoPage>();
    searchAPI.searchVideos
      .mockReturnValueOnce(oldRequest.promise)
      .mockReturnValueOnce(newRequest.promise);
    render("旧");
    act(() => {
      void current.load("videos", true);
    });
    render("新");
    act(() => {
      void current.load("videos", true);
    });
    await act(async () => {
      newRequest.resolve({ items: [video(2)], next_cursor: "", has_more: false });
      await newRequest.promise;
    });
    await act(async () => {
      oldRequest.resolve({ items: [video(1)], next_cursor: "", has_more: false });
      await oldRequest.promise;
    });
    expect(current.videos.items.map((item) => item.id)).toEqual([2]);
  });

  it("turns infrastructure and network failures into understandable messages", () => {
    expect(searchErrorMessage(new ApiError("search failed", 500, "SEARCH_SERVICE_UNAVAILABLE")))
      .toBe("搜索服务暂时不可用，请稍后重试");
    expect(searchErrorMessage(new NetworkError()))
      .toBe("网络连接失败，请检查网络后重试");
    expect(searchErrorMessage(new ApiError("search query is required", 400, "SEARCH_QUERY_REQUIRED")))
      .toBe("请输入搜索关键词");
  });

  function render(query: string) {
    act(() => root.render(<Harness query={query} onValue={(value) => { current = value; }} />));
  }
});

function Harness({ query, onValue }: { query: string; onValue: (value: ReturnType<typeof useSearch>) => void }) {
  const value = useSearch(query);
  onValue(value);
  return null;
}

function video(id: number): Video {
  const time = "2026-08-04T00:00:00Z";
  return {
    id,
    author_id: 3,
    title: `视频 ${id}`,
    description: "",
    media_url: `/video-${id}.mp4`,
    cover_url: `/cover-${id}.jpg`,
    status: 2,
    visibility: "public" as const,
    like_count: 0,
    comment_count: 0,
    favorite_count: 0,
    published_at: time,
    created_at: time,
    updated_at: time
  };
}

function user(id: number) {
  return { id, nickname: `用户 ${id}`, avatar_url: "", bio: "" };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}
