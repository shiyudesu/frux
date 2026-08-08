// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FeedItem, FeedItemsResponse, RecommendationContext } from "../types";
import type { UseFeedCallbacks } from "./useFeed";
import { useFeed } from "./useFeed";

const feedAPI = vi.hoisted(() => ({
  fetchFeedPage: vi.fn(),
  fetchPlaybackConfig: vi.fn()
}));

const routerMocks = vi.hoisted(() => ({
  navigate: vi.fn()
}));

const sessionState = vi.hoisted(() => ({
  current: {
    token: "token-a",
    user: { id: 7 },
    clearAuth: vi.fn(),
    setAuth: vi.fn(),
    updateUser: vi.fn()
  }
}));

const preloadState = vi.hoisted(() => ({
  loadMore: null as null | (() => void)
}));

vi.mock("../api/feed", async (importOriginal) => {
  const original = await importOriginal<typeof import("../api/feed")>();
  return {
    ...original,
    fetchFeedPage: feedAPI.fetchFeedPage,
    fetchPlaybackConfig: feedAPI.fetchPlaybackConfig
  };
});

vi.mock("../router", () => ({
  useNavigate: () => routerMocks.navigate
}));

vi.mock("../session", () => ({
  useSession: () => sessionState.current
}));

vi.mock("./useFeedPreloading", () => ({
  useFeedPreloading: (input: { loadMore: () => void }) => {
    preloadState.loadMore = input.loadMore;
    return {
      controller: {},
      candidates: [],
      candidateByVideoID: new Map(),
      playerResourceByVideoID: new Map(),
      policy: { networkClass: "unknown", forwardCount: 0 },
      debug: { activeResources: 0, ready: 0, reused: 0, cancellations: 0, failures: 0 }
    };
  }
}));

describe("useFeed scene continuity", () => {
  let container: HTMLDivElement;
  let root: Root;
  let current: ReturnType<typeof useFeed>;
  let callbacks: UseFeedCallbacks;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    feedAPI.fetchFeedPage.mockReset();
    feedAPI.fetchPlaybackConfig.mockReset();
    feedAPI.fetchPlaybackConfig.mockResolvedValue({});
    routerMocks.navigate.mockReset();
    sessionState.current = {
      token: "token-a",
      user: { id: 7 },
      clearAuth: vi.fn(),
      setAuth: vi.fn(),
      updateUser: vi.fn()
    };
    preloadState.loadMore = null;
    callbacks = {
      resetSwipe: vi.fn(),
      closeComments: vi.fn()
    };
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  it("restores a committed scene without another first-page request", async () => {
    feedAPI.fetchFeedPage
      .mockResolvedValueOnce(page("timeline", [item(1), item(2)], "timeline-next", true))
      .mockResolvedValueOnce(page("hot", [item(10)], "", false));

    render("timeline");
    await flush();
    const timelineGeneration = current.feedGeneration;
    act(() => current.setIndex(1));
    render("hot");
    await flush();
    const hotGeneration = current.feedGeneration;
    render("timeline");
    await flush();

    expect(current.current?.video_id).toBe(2);
    expect(firstPageCalls("timeline")).toHaveLength(1);
    expect(hotGeneration).toBeGreaterThan(timelineGeneration);
    expect(current.feedGeneration).toBeGreaterThan(hotGeneration);
    expect(callbacks.resetSwipe).toHaveBeenCalledTimes(3);
    expect(callbacks.closeComments).toHaveBeenCalledTimes(3);
  });

  it("restores recommendation identity and refreshes from recommendation-only context", async () => {
    feedAPI.fetchFeedPage
      .mockResolvedValueOnce(page("recommend", [item(1), item(2)], "recommend-next", true, "server-request-1"))
      .mockResolvedValueOnce(page("timeline", [item(99)], "", false))
      .mockResolvedValueOnce(page("recommend", [item(3)], "", false, "server-request-2"));

    render("recommend");
    await flush();
    const firstContext = recommendationCalls()[0][3] as RecommendationContext;
    act(() => current.setIndex(1));
    render("timeline");
    await flush();
    render("recommend");
    await flush();

    expect(current.current?.video_id).toBe(2);
    expect(current.feedRequestID).toBe("server-request-1");
    expect(recommendationCalls()).toHaveLength(1);

    act(() => current.loadFeed());
    await flush();
    const refreshedContext = recommendationCalls()[1][3] as RecommendationContext;

    expect(refreshedContext.session_id).toBe(firstContext.session_id);
    expect(refreshedContext.refresh_index).toBe(1);
    expect(refreshedContext.recent_video_ids).toEqual([1, 2]);
    expect(refreshedContext.current_video_id).toBe(2);
    expect(refreshedContext.recent_video_ids).not.toContain(99);
  });

  it("ignores a first-page response that arrives after another scene activates", async () => {
    const timeline = deferred<FeedItemsResponse>();
    feedAPI.fetchFeedPage
      .mockReturnValueOnce(timeline.promise)
      .mockResolvedValueOnce(page("hot", [item(10)], "", false));

    render("timeline");
    render("hot");
    await flush();
    await act(async () => {
      timeline.resolve(page("timeline", [item(1)], "", false));
      await timeline.promise;
    });

    expect(current.current?.video_id).toBe(10);
    expect(current.items.map((video) => video.video_id)).toEqual([10]);
  });

  it("keeps only the latest scene authoritative during rapid switching", async () => {
    const timeline = deferred<FeedItemsResponse>();
    const hot = deferred<FeedItemsResponse>();
    feedAPI.fetchFeedPage
      .mockReturnValueOnce(timeline.promise)
      .mockReturnValueOnce(hot.promise)
      .mockResolvedValueOnce(page("following", [item(20)], "", false))
      .mockResolvedValueOnce(page("timeline", [item(2)], "", false));

    render("timeline");
    render("hot");
    render("following");
    await flush();
    await act(async () => {
      hot.resolve(page("hot", [item(10)], "", false));
      timeline.resolve(page("timeline", [item(1)], "", false));
      await Promise.all([hot.promise, timeline.promise]);
    });

    expect(current.current?.video_id).toBe(20);
    render("timeline");
    await flush();
    expect(current.current?.video_id).toBe(2);
  });

  it("restores committed pagination after an interrupted load-more and retries it safely", async () => {
    const interrupted = deferred<FeedItemsResponse>();
    feedAPI.fetchFeedPage
      .mockResolvedValueOnce(page("timeline", [item(1)], "next", true))
      .mockReturnValueOnce(interrupted.promise)
      .mockResolvedValueOnce(page("hot", [item(10)], "", false))
      .mockResolvedValueOnce(page("timeline", [item(2)], "", false));

    render("timeline");
    await flush();
    act(() => preloadState.loadMore?.());
    render("hot");
    await flush();
    await act(async () => {
      interrupted.resolve(page("timeline", [item(999)], "", false));
      await interrupted.promise;
    });
    render("timeline");
    await flush();

    expect(current.items.map((video) => video.video_id)).toEqual([1]);
    expect(current.hasMore).toBe(true);
    act(() => preloadState.loadMore?.());
    await flush();
    expect(current.items.map((video) => video.video_id)).toEqual([1, 2]);
  });

  it("invalidates every retained scene when authenticated identity changes", async () => {
    feedAPI.fetchFeedPage
      .mockResolvedValueOnce(page("timeline", [item(1)], "", false))
      .mockResolvedValueOnce(page("hot", [item(10)], "", false))
      .mockResolvedValueOnce(page("timeline", [item(2)], "", false));

    render("timeline");
    await flush();
    render("hot");
    await flush();
    sessionState.current = {
      token: "token-b",
      user: { id: 8 },
      clearAuth: vi.fn(),
      setAuth: vi.fn(),
      updateUser: vi.fn()
    };
    render("timeline");
    await flush();

    expect(current.current?.video_id).toBe(2);
    expect(firstPageCalls("timeline")).toHaveLength(2);
  });

  it("keeps recommendation suppression when temporarily visiting another scene", async () => {
    feedAPI.fetchFeedPage
      .mockResolvedValueOnce(page("recommend", [item(1), item(2)], "next", true, "server-request"))
      .mockResolvedValueOnce(page("timeline", [item(10)], "", false));

    render("recommend");
    await flush();
    const rejected = current.items[0];
    act(() => current.removeAcceptedFeedback(rejected, "not_interested"));
    render("timeline");
    await flush();
    render("recommend");
    await flush();

    expect(current.items.map((video) => video.video_id)).toEqual([2]);
    expect(recommendationCalls()).toHaveLength(1);
  });

  it("patches interaction state in every retained scene containing the video", async () => {
    feedAPI.fetchFeedPage
      .mockResolvedValueOnce(page("timeline", [item(2)], "", false))
      .mockResolvedValueOnce(page("hot", [item(2)], "", false));

    render("timeline");
    await flush();
    render("hot");
    await flush();
    render("timeline");
    await flush();
    act(() => current.updateViewerAction(2, "liked", true, { like_count: 9 }));
    render("hot");
    await flush();

    expect(current.current?.video_id).toBe(2);
    expect(current.liked[2]).toBe(true);
    expect(current.current?.like_count).toBe(9);
  });

  it("refreshes only the active scene while retaining other scene snapshots", async () => {
    feedAPI.fetchFeedPage
      .mockResolvedValueOnce(page("timeline", [item(1), item(2)], "", false))
      .mockResolvedValueOnce(page("hot", [item(10)], "", false))
      .mockResolvedValueOnce(page("timeline", [item(3)], "", false));

    render("timeline");
    await flush();
    act(() => current.setIndex(1));
    render("hot");
    await flush();
    render("timeline");
    await flush();
    expect(current.current?.video_id).toBe(2);

    render("timeline", 1);
    await flush();
    expect(current.current?.video_id).toBe(3);
    expect(firstPageCalls("timeline")).toHaveLength(2);

    render("hot");
    await flush();
    expect(current.current?.video_id).toBe(10);
    expect(firstPageCalls("hot")).toHaveLength(1);

    render("timeline", 1);
    await flush();
    expect(current.current?.video_id).toBe(3);
    expect(firstPageCalls("timeline")).toHaveLength(2);
  });

  function render(scene: string, refreshRequest = 0) {
    act(() => root.render(
      <Harness
        scene={scene}
        callbacks={callbacks}
        refreshRequest={refreshRequest}
        onValue={(value) => {
          current = value;
        }}
      />
    ));
  }

  function firstPageCalls(scene: string) {
    return feedAPI.fetchFeedPage.mock.calls.filter((call) => call[0] === scene && call[2] === "");
  }

  function recommendationCalls() {
    return feedAPI.fetchFeedPage.mock.calls.filter((call) => call[0] === "recommend");
  }
});

function Harness({
  scene,
  callbacks,
  refreshRequest,
  onValue
}: {
  scene: string;
  callbacks: UseFeedCallbacks;
  refreshRequest: number;
  onValue: (value: ReturnType<typeof useFeed>) => void;
}) {
  const value = useFeed(scene, callbacks, refreshRequest);
  onValue(value);
  return null;
}

function page(
  scene: string,
  items: FeedItem[],
  nextCursor: string,
  hasMore: boolean,
  requestID?: string
): FeedItemsResponse {
  return {
    scene,
    items,
    next_cursor: nextCursor,
    has_more: hasMore,
    request_id: requestID
  };
}

function item(id: number): FeedItem {
  return {
    video_id: id,
    author_id: id + 100,
    author_nickname: `author ${id}`,
    author_avatar_url: "",
    title: `video ${id}`,
    description: "",
    media_url: `/video-${id}.mp4`,
    cover_url: `/cover-${id}.jpg`,
    like_count: 0,
    comment_count: 0,
    favorite_count: 0,
    liked: false,
    favorited: false,
    published_at: "2026-08-08T00:00:00Z"
  };
}

async function flush() {
  await act(async () => {
    await new Promise((resolve) => window.setTimeout(resolve, 0));
  });
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
