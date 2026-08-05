// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NetworkError } from "../api/client";
import type { LibraryVideoItem, LibraryVideoPage, WatchLaterStateResponse } from "../types";
import {
  appendLibraryItems,
  restoreLibraryItem,
  upsertLibraryItem,
  useProfileLibrary
} from "./useProfileLibrary";

const libraryAPI = vi.hoisted(() => ({
  clearWatchHistory: vi.fn(),
  deleteWatchHistoryItem: vi.fn(),
  fetchFavoriteVideos: vi.fn(),
  fetchLikedVideos: vi.fn(),
  fetchWatchHistory: vi.fn(),
  fetchWatchLater: vi.fn(),
  setWatchLater: vi.fn()
}));

vi.mock("../api/library", () => libraryAPI);

describe("profile library state", () => {
  let container: HTMLDivElement;
  let root: Root;
  let current: ReturnType<typeof useProfileLibrary>;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    Object.values(libraryAPI).forEach((mock) => mock.mockReset());
    libraryAPI.fetchLikedVideos.mockResolvedValue(emptyPage());
    libraryAPI.fetchFavoriteVideos.mockResolvedValue(emptyPage());
    libraryAPI.fetchWatchHistory.mockResolvedValue(emptyPage());
    libraryAPI.fetchWatchLater.mockResolvedValue(emptyPage());
    libraryAPI.clearWatchHistory.mockResolvedValue(null);
    libraryAPI.deleteWatchHistoryItem.mockResolvedValue(null);
    libraryAPI.setWatchLater.mockResolvedValue({
      video_id: 1,
      active: false,
      updated_at: "2026-08-04T00:00:00Z"
    } satisfies WatchLaterStateResponse);
    act(() => root.render(<Harness onValue={(value) => { current = value; }} />));
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  it("deduplicates appended pages and restores only a failed optimistic item", () => {
    const first = item(1, "2026-08-04T02:00:00Z");
    const second = item(2, "2026-08-04T01:00:00Z");
    expect(appendLibraryItems([first], [first, second])).toEqual([first, second]);
    expect(restoreLibraryItem([], second)).toEqual([second]);
    expect(restoreLibraryItem([first], second)).toEqual([first, second]);
    expect(restoreLibraryItem([first, second], second)).toEqual([first, second]);
    expect(upsertLibraryItem([second], first)).toEqual([first, second]);
  });

  it("keeps independently resolving tab pages in their owning state", async () => {
    const likes = deferred<LibraryVideoPage>();
    const favorites = deferred<LibraryVideoPage>();
    libraryAPI.fetchLikedVideos.mockReturnValue(likes.promise);
    libraryAPI.fetchFavoriteVideos.mockReturnValue(favorites.promise);

    act(() => {
      void current.loadTab("likes", true);
      void current.loadTab("favorites", true);
    });
    await act(async () => {
      favorites.resolve(page([item(2)]));
      await favorites.promise;
    });
    expect(current.tabs.favorites.items.map((entry) => entry.video.id)).toEqual([2]);
    expect(current.tabs.likes.state).toBe("loading");

    await act(async () => {
      likes.resolve(page([item(1)]));
      await likes.promise;
    });
    expect(current.tabs.likes.items.map((entry) => entry.video.id)).toEqual([1]);
    expect(current.tabs.favorites.items.map((entry) => entry.video.id)).toEqual([2]);
  });

  it("restores only the failed Watch Later removal during overlapping mutations", async () => {
    libraryAPI.fetchWatchLater.mockResolvedValue(page([item(1), item(2)]));
    await act(async () => {
      await current.loadTab("watchLater", true);
    });

    const failed = deferred<WatchLaterStateResponse>();
    const succeeded = deferred<WatchLaterStateResponse>();
    libraryAPI.setWatchLater.mockImplementation((_token: string, videoID: number) =>
      videoID === 1 ? failed.promise : succeeded.promise
    );

    let firstResult: Promise<boolean>;
    let secondResult: Promise<boolean>;
    act(() => {
      firstResult = current.removeWatchLater(1);
      secondResult = current.removeWatchLater(2);
    });
    expect(current.tabs.watchLater.items).toEqual([]);

    await act(async () => {
      succeeded.resolve({ video_id: 2, active: false, updated_at: "2026-08-04T03:00:00Z" });
      expect(await secondResult!).toBe(true);
    });
    await act(async () => {
      failed.reject(new NetworkError());
      expect(await firstResult!).toBe(false);
    });

    expect(current.tabs.watchLater.items.map((entry) => entry.video.id)).toEqual([1]);
    expect(current.tabs.watchLater.error).toBe("网络连接失败，请检查网络后重试");
  });

  it("continues Watch Later pagination when removing the last loaded item", async () => {
    libraryAPI.fetchWatchLater
      .mockResolvedValueOnce({ items: [item(1)], next_cursor: "next", has_more: true })
      .mockResolvedValueOnce(page([item(2)]));
    await act(async () => {
      await current.loadTab("watchLater", true);
    });
    await act(async () => {
      expect(await current.removeWatchLater(1)).toBe(true);
    });
    expect(current.tabs.watchLater.items.map((entry) => entry.video.id)).toEqual([2]);
    expect(libraryAPI.fetchWatchLater).toHaveBeenCalledTimes(2);
  });

  it("updates an already loaded Watch Later cache after a successful add", async () => {
    libraryAPI.fetchWatchLater.mockResolvedValue(page([item(2)]));
    await act(async () => {
      await current.loadTab("watchLater", true);
    });
    act(() => {
      current.addWatchLater(item(1), "2026-08-04T03:00:00Z");
    });
    expect(current.tabs.watchLater.items.map((entry) => entry.video.id)).toEqual([1, 2]);
  });

  it("invalidates a partial Watch Later cache when an absent item is added", async () => {
    libraryAPI.fetchWatchLater.mockResolvedValue({
      items: [item(2)],
      next_cursor: "next",
      has_more: true
    });
    await act(async () => {
      await current.loadTab("watchLater", true);
    });
    act(() => {
      current.addWatchLater(item(1), "2026-08-04T03:00:00Z");
    });
    expect(current.tabs.watchLater).toMatchObject({
      items: [],
      nextCursor: "",
      hasMore: false,
      state: "idle"
    });
  });

  it("removes accepted membership changes locally while preserving pagination", async () => {
    libraryAPI.fetchLikedVideos
      .mockResolvedValueOnce({ items: [item(1)], next_cursor: "next", has_more: true })
      .mockResolvedValueOnce(page([item(2)]));
    await act(async () => {
      await current.loadTab("likes", true);
    });
    act(() => {
      current.applyVideoAction("likes", 1, "like", false, { like_count: 0 });
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(libraryAPI.fetchLikedVideos).toHaveBeenCalledTimes(2);
    expect(current.tabs.likes.items.map((entry) => entry.video.id)).toEqual([2]);

    libraryAPI.fetchFavoriteVideos.mockResolvedValueOnce(emptyPage());
    await act(async () => {
      await current.loadTab("favorites", true);
    });
    act(() => {
      current.applyVideoAction("history", 7, "favorite", true, { favorite_count: 1 });
    });
    expect(current.tabs.favorites.state).toBe("idle");
  });

  it("updates only the counter belonging to the accepted interaction", async () => {
    const historyItem = item(1);
    historyItem.video.favorite_count = 5;
    libraryAPI.fetchWatchHistory.mockResolvedValueOnce(page([historyItem]));
    await act(async () => {
      await current.loadTab("history", true);
    });
    act(() => {
      current.applyVideoAction("history", 1, "like", true, { like_count: 3 });
    });
    expect(current.tabs.history.items[0]?.video).toMatchObject({
      like_count: 3,
      favorite_count: 5
    });
  });

  it("releases the history loading lock when clear invalidates an active page", async () => {
    const pending = deferred<LibraryVideoPage>();
    libraryAPI.fetchWatchHistory
      .mockReturnValueOnce(pending.promise)
      .mockResolvedValueOnce(emptyPage());
    let firstLoad: Promise<void>;
    act(() => {
      firstLoad = current.loadTab("history", true);
    });
    await act(async () => {
      await current.clearHistory();
    });
    await act(async () => {
      pending.resolve(page([item(1)]));
      await firstLoad!;
    });
    await act(async () => {
      await current.loadTab("history", true);
    });
    expect(libraryAPI.fetchWatchHistory).toHaveBeenCalledTimes(2);
  });

  it("keeps same-source removal stable while pagination is active", async () => {
    const pagination = deferred<LibraryVideoPage>();
    libraryAPI.fetchLikedVideos
      .mockResolvedValueOnce({ items: [item(1)], next_cursor: "next", has_more: true })
      .mockReturnValueOnce(pagination.promise);
    await act(async () => {
      await current.loadTab("likes", true);
    });
    act(() => {
      void current.loadTab("likes");
      current.applyVideoAction("likes", 1, "like", false, { like_count: 0 });
    });
    await act(async () => {
      pagination.resolve(page([item(2)]));
      await pagination.promise;
    });
    expect(libraryAPI.fetchLikedVideos).toHaveBeenCalledTimes(2);
    expect(current.tabs.likes.items.map((entry) => entry.video.id)).toEqual([2]);
  });
});

function Harness({ onValue }: { onValue: (value: ReturnType<typeof useProfileLibrary>) => void }) {
  const value = useProfileLibrary("token");
  onValue(value);
  return null;
}

function emptyPage(): LibraryVideoPage {
  return { items: [], next_cursor: "", has_more: false };
}

function page(items: LibraryVideoItem[]): LibraryVideoPage {
  return { items, next_cursor: "", has_more: false };
}

function item(id: number, updatedAt = `2026-08-04T00:00:0${id}Z`): LibraryVideoItem {
  return {
    video: {
      id,
      author_id: 2,
      title: `视频 ${id}`,
      description: "",
      media_url: `/video-${id}.mp4`,
      cover_url: `/cover-${id}.jpg`,
      status: 2,
      visibility: "public",
      like_count: 0,
      comment_count: 0,
      favorite_count: 0,
      created_at: updatedAt,
      updated_at: updatedAt
    },
    updated_at: updatedAt
  };
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
