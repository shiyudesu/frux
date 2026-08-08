// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RouterProvider } from "../router";
import type { FeedVideo } from "../types";
import { FeedPage } from "./FeedPage";

const feedMocks = vi.hoisted(() => ({
  loadFollowingMap: vi.fn(() => new Promise<Record<number, boolean>>(() => {})),
  useFollowingDirectory: vi.fn()
}));

vi.mock("../api/social", async (importOriginal) => {
  const original = await importOriginal<typeof import("../api/social")>();
  return {
    ...original,
    loadFollowingMap: feedMocks.loadFollowingMap
  };
});

vi.mock("../session", () => ({
  useSession: () => ({
    token: "token",
    user: { id: 42 },
    clearAuth: vi.fn()
  }),
  updateSessionRelationCount: vi.fn()
}));

vi.mock("../hooks/useFollowingDirectory", () => ({
  useFollowingDirectory: feedMocks.useFollowingDirectory
}));

vi.mock("../hooks/useFeed", () => ({
  shouldApplyAcceptedRecommendationFeedback: () => false,
  useFeed: () => ({
    items: [video()],
    index: 0,
    setIndex: vi.fn(),
    liked: {},
    favorited: {},
    feedState: "ready",
    feedError: "",
    hasMore: false,
    loadingMore: false,
    current: video(),
    loadFeed: vi.fn(),
    updateCurrentItem: vi.fn(),
    updateViewerAction: vi.fn(),
    removeAcceptedFeedback: vi.fn(),
    isRecommendationSceneActive: () => false,
    preloadController: {},
    preloadCandidateByVideoID: new Map(),
    playerResourceByVideoID: new Map(),
    preloadPolicy: { networkClass: "unknown" },
    preloadDebug: { activeResources: 0, ready: 0, reused: 0, cancellations: 0, failures: 0 }
  })
}));

vi.mock("../hooks/useSwipe", () => ({
  getFeedTrackStyle: () => undefined,
  useSwipe: () => ({
    swipe: null,
    cancelSwipe: vi.fn(),
    moveTo: vi.fn(),
    handlePointerDown: vi.fn(),
    handlePointerMove: vi.fn(),
    handlePointerEnd: vi.fn(),
    handleWheel: vi.fn()
  })
}));

vi.mock("../hooks/useComments", () => ({
  useComments: () => ({})
}));

vi.mock("../hooks/usePlayerPreferences", () => ({
  usePlayerPreferences: () => ({
    preferences: { quality: "auto", playbackRate: 1, continuousPlay: false },
    updatePreferences: vi.fn()
  })
}));

vi.mock("../components/VideoStage", async () => {
  const React = await import("react");
  return {
    VideoStage: React.forwardRef(() => <div data-ui="mock-video-stage" />)
  };
});

vi.mock("../components/FeedDetailsPanel", () => ({
  FeedDetailsPanel: () => <aside data-ui="mock-details-panel" />
}));

describe("FeedPage following directory integration", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    feedMocks.useFollowingDirectory.mockReturnValue(directory());
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  it("renders the directory as a Feed sibling only for Following and preserves it while collapsed", () => {
    render("following");
    const layout = required<HTMLElement>('[data-ui="feed-layout"]');
    const directoryElement = required<HTMLElement>('[data-ui="following-directory"]');
    const feedMain = required<HTMLElement>('[data-ui="feed-main"]');
    expect(directoryElement.parentElement).toBe(layout);
    expect(feedMain.parentElement).toBe(layout);
    expect(feedMain.contains(directoryElement)).toBe(false);

    click(requiredButton("收起"));
    expect(required<HTMLElement>('[data-ui="following-directory"]').getAttribute("aria-hidden")).toBe("true");
    expect(requiredButton("关注列表")).toBeTruthy();

    render("timeline");
    expect(container.querySelector('[data-ui="following-directory"]')).toBeNull();

    render("following");
    expect(required<HTMLElement>('[data-ui="following-directory"]').getAttribute("aria-hidden")).toBe("true");
    expect(requiredButton("关注列表")).toBeTruthy();
  });

  function render(scene: string) {
    act(() => root.render(
      <RouterProvider>
        <FeedPage feedScene={scene} />
      </RouterProvider>
    ));
  }

  function required<T extends Element>(selector: string): T {
    const element = container.querySelector<T>(selector);
    if (!element) throw new Error(`missing element: ${selector}`);
    return element;
  }

  function requiredButton(text: string): HTMLButtonElement {
    const button = [...container.querySelectorAll<HTMLButtonElement>("button")]
      .find((candidate) => candidate.textContent?.includes(text) || candidate.getAttribute("aria-label") === text);
    if (!button) throw new Error(`missing button: ${text}`);
    return button;
  }
});

function video(): FeedVideo {
  return {
    video_id: 1,
    author_id: 7,
    title: "video",
    media_url: "/cover.jpg",
    cover_url: "/cover.jpg",
    like_count: 0,
    comment_count: 0,
    favorite_count: 0,
    liked: false,
    favorited: false,
    author: "Creator",
    avatar_url: "",
    description: "",
    feed_scene: "following",
    request_id: ""
  };
}

function directory() {
  return {
    items: [{
      user_id: 7,
      account: "creator",
      nickname: "Creator",
      avatar_url: "",
      bio: "",
      followed_at: "2026-08-08T00:00:00Z"
    }],
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
  };
}

function click(button: HTMLButtonElement) {
  act(() => button.click());
}
