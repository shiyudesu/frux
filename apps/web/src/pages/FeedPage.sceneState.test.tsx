// @vitest-environment jsdom
import { StrictMode, act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  RouterProvider,
  feedSceneFromRoute,
  useNavigate,
  useRoute
} from "../router";
import type { FeedItem, FeedItemsResponse, FeedVideo } from "../types";
import { FeedPage } from "./FeedPage";

const feedAPI = vi.hoisted(() => ({
  fetchFeedPage: vi.fn(),
  fetchPlaybackConfig: vi.fn()
}));

const socialAPI = vi.hoisted(() => ({
  loadFollowingMap: vi.fn()
}));

const sessionMock = vi.hoisted(() => ({
  token: "token",
  user: { id: 7 },
  clearAuth: vi.fn(),
  updateUser: vi.fn()
}));

vi.mock("../api/feed", async (importOriginal) => {
  const original = await importOriginal<typeof import("../api/feed")>();
  return {
    ...original,
    fetchFeedPage: feedAPI.fetchFeedPage,
    fetchPlaybackConfig: feedAPI.fetchPlaybackConfig
  };
});

vi.mock("../api/social", async (importOriginal) => {
  const original = await importOriginal<typeof import("../api/social")>();
  return {
    ...original,
    loadFollowingMap: socialAPI.loadFollowingMap
  };
});

vi.mock("../session", () => ({
  useSession: () => sessionMock,
  updateSessionRelationCount: vi.fn()
}));

vi.mock("../hooks/useSwipe", () => ({
  getFeedTrackStyle: () => undefined,
  useSwipe: ({
    onIndexChange
  }: {
    onIndexChange: (index: number) => void;
  }) => ({
    swipe: null,
    cancelSwipe: vi.fn(),
    moveTo: (index: number) => onIndexChange(index),
    handlePointerDown: vi.fn(),
    handlePointerMove: vi.fn(),
    handlePointerEnd: vi.fn(),
    handleWheel: vi.fn()
  })
}));

vi.mock("../hooks/useComments", () => ({
  useComments: () => ({})
}));

vi.mock("../hooks/useFollowingDirectory", () => ({
  useFollowingDirectory: () => ({
    items: [],
    nextCursor: "",
    hasMore: false,
    state: "ready",
    error: "",
    query: "",
    normalizedQuery: "",
    setQuery: vi.fn(),
    loadMore: vi.fn(),
    retry: vi.fn(),
    refresh: vi.fn(),
    setUserActive: vi.fn()
  })
}));

vi.mock("../hooks/usePlayerPreferences", () => ({
  usePlayerPreferences: () => ({
    preferences: { quality: "auto", playbackRate: 1, continuousPlay: false },
    updatePreferences: vi.fn()
  })
}));

vi.mock("../hooks/useFeedPreloading", () => ({
  useFeedPreloading: () => ({
    controller: {},
    candidates: [],
    candidateByVideoID: new Map(),
    playerResourceByVideoID: new Map(),
    policy: { networkClass: "unknown", forwardCount: 0 },
    debug: { activeResources: 0, ready: 0, reused: 0, cancellations: 0, failures: 0 }
  })
}));

vi.mock("../components/VideoStage", async () => {
  const React = await import("react");
  return {
    VideoStage: React.forwardRef(({
      item,
      onComment
    }: {
      item: FeedVideo;
      onComment: () => void;
    }, _ref) => (
      <div data-ui="mock-video-stage" data-video-id={item.video_id}>
        <button type="button" onClick={onComment}>打开评论</button>
      </div>
    ))
  };
});

vi.mock("../components/FeedDetailsPanel", () => ({
  FeedDetailsPanel: ({ open }: { open: boolean }) => (
    <aside data-ui="mock-details-panel" data-open={open ? "true" : "false"} />
  )
}));

describe("FeedPage scene continuity", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    window.history.replaceState({}, "", "/timeline");
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    feedAPI.fetchFeedPage.mockReset();
    feedAPI.fetchPlaybackConfig.mockReset();
    feedAPI.fetchPlaybackConfig.mockResolvedValue({});
    socialAPI.loadFollowingMap.mockReset();
    socialAPI.loadFollowingMap.mockResolvedValue({});
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  it("restores the active video through direct navigation and browser Back with transient UI closed", async () => {
    feedAPI.fetchFeedPage.mockImplementation((scene: string) => Promise.resolve(
      scene === "hot"
        ? page("hot", [item(10)])
        : page("timeline", [item(1), item(2)])
    ));

    act(() => root.render(
      <StrictMode>
        <RouterProvider>
          <FeedRouteHarness />
        </RouterProvider>
      </StrictMode>
    ));
    await flush();
    const initialTimelineRequests = firstPageCalls("timeline").length;
    act(() => window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown" })));
    expect(activeVideoID()).toBe("2");
    act(() => requiredButton("打开评论").click());
    expect(detailsOpen()).toBe("true");

    act(() => requiredButton("前往热门").click());
    await flush();
    expect(window.location.pathname).toBe("/hotfeed");
    expect(activeVideoID()).toBe("10");
    expect(detailsOpen()).toBe("false");

    act(() => {
      window.history.replaceState({}, "", "/timeline");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await flush();
    expect(activeVideoID()).toBe("2");
    expect(detailsOpen()).toBe("false");
    expect(firstPageCalls("timeline")).toHaveLength(initialTimelineRequests);
  });

  function activeVideoID(): string | null {
    return container.querySelector<HTMLElement>('[data-ui="feed-layout"]')
      ?.getAttribute("data-active-video-id") || null;
  }

  function detailsOpen(): string | null {
    return container.querySelector<HTMLElement>('[data-ui="mock-details-panel"]')
      ?.getAttribute("data-open") || null;
  }

  function requiredButton(text: string): HTMLButtonElement {
    const button = [...container.querySelectorAll<HTMLButtonElement>("button")]
      .find((candidate) => candidate.textContent?.includes(text));
    if (!button) throw new Error(`missing button: ${text}`);
    return button;
  }

  function firstPageCalls(scene: string) {
    return feedAPI.fetchFeedPage.mock.calls.filter((call) => call[0] === scene && call[2] === "");
  }
});

function FeedRouteHarness() {
  const route = useRoute();
  const navigate = useNavigate();
  return (
    <>
      <button type="button" onClick={() => navigate("/hotfeed")}>前往热门</button>
      <FeedPage feedScene={feedSceneFromRoute(route)} />
    </>
  );
}

function page(scene: string, items: FeedItem[]): FeedItemsResponse {
  return {
    scene,
    items,
    next_cursor: "",
    has_more: false
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
