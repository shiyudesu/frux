// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FeedVideo } from "../types";
import { FeedActionRail } from "./FeedActionRail";

describe("FeedActionRail menus", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  it("opens recommendation actions and restores focus with Escape", () => {
    render({ onRecommendationFeedback: vi.fn(async () => {}) });
    const trigger = requiredButton("更多操作");
    act(() => trigger.click());

    const menu = required<HTMLElement>('[role="menu"][aria-label="更多操作"]');
    expect(menu).toBeTruthy();
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    expect(document.activeElement?.textContent).toBe("不感兴趣");

    act(() => document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })));
    expect(container.querySelector('[role="menu"][aria-label="更多操作"]')).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it("dismisses recommendation actions on outside pointer input", () => {
    render({ onRecommendationFeedback: vi.fn(async () => {}) });
    act(() => requiredButton("更多操作").click());
    expect(required<HTMLElement>('[role="menu"][aria-label="更多操作"]')).toBeTruthy();

    act(() => document.body.dispatchEvent(new Event("pointerdown", { bubbles: true })));
    expect(container.querySelector('[role="menu"][aria-label="更多操作"]')).toBeNull();
  });

  it("does not render a nonfunctional more control without available actions", () => {
    render({});
    expect([...container.querySelectorAll("button")].some((button) => button.getAttribute("aria-label") === "更多操作")).toBe(false);
  });

  function render(overrides: { onRecommendationFeedback?: (type: "not_interested" | "reduce_author" | "already_seen") => Promise<void> }) {
    act(() => root.render(
      <FeedActionRail
        item={video()}
        liked={false}
        favorited={false}
        following={false}
        followBusy={false}
        ownVideo={false}
        onLike={() => {}}
        onComment={() => {}}
        onFavorite={() => {}}
        onFollow={() => {}}
        onOpenAuthor={() => {}}
        onRecommendationFeedback={overrides.onRecommendationFeedback}
      />
    ));
  }

  function required<T extends Element>(selector: string): T {
    const element = container.querySelector<T>(selector);
    if (!element) throw new Error(`missing element: ${selector}`);
    return element;
  }

  function requiredButton(label: string): HTMLButtonElement {
    const button = [...container.querySelectorAll<HTMLButtonElement>("button")]
      .find((candidate) => candidate.getAttribute("aria-label") === label);
    if (!button) throw new Error(`missing button: ${label}`);
    return button;
  }
});

function video(): FeedVideo {
  return {
    video_id: 1,
    author_id: 2,
    title: "title",
    media_url: "/video.mp4",
    cover_url: "/cover.jpg",
    like_count: 3,
    comment_count: 4,
    favorite_count: 5,
    liked: false,
    favorited: false,
    author: "author",
    avatar_url: "",
    description: "description",
    feed_scene: "recommend",
    request_id: "request-1"
  };
}
