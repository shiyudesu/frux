// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReturnTypeOfUseFollowingDirectory } from "../hooks/useFollowingDirectory";
import type { RelationUser } from "../types";
import { FollowingFeedDirectory } from "./FollowingFeedDirectory";

describe("FollowingFeedDirectory", () => {
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
  });

  it("renders truthful followed-user rows and opens profiles", () => {
    const onOpenUser = vi.fn();
    render(directory({ items: [user(7)] }), false, vi.fn(), onOpenUser);
    click(requiredButton("Creator"));
    expect(onOpenUser).toHaveBeenCalledWith(user(7));
    expect(container.textContent).not.toContain("正在直播");
    expect(container.textContent).not.toContain("未看");
  });

  it("collapses accessibly without discarding the mounted directory", () => {
    const onCollapse = vi.fn();
    render(directory({ items: [user(7)] }), false, onCollapse, vi.fn());
    click(requiredButton("收起"));
    expect(onCollapse).toHaveBeenCalledTimes(1);

    render(directory({ items: [user(7)] }), true, onCollapse, vi.fn());
    expect(required<HTMLElement>('[data-ui="following-directory"]').getAttribute("aria-hidden")).toBe("true");
    expect(container.textContent).toContain("Creator");
  });

  it("loads more near the independent scroll boundary", () => {
    const loadMore = vi.fn();
    render(directory({ items: [user(7)], hasMore: true, loadMore }), false, vi.fn(), vi.fn());
    const scroller = required<HTMLDivElement>(".following-directory-scroll");
    Object.defineProperties(scroller, {
      scrollHeight: { configurable: true, value: 500 },
      clientHeight: { configurable: true, value: 300 },
      scrollTop: { configurable: true, value: 150 }
    });
    act(() => scroller.dispatchEvent(new Event("scroll", { bubbles: true })));
    expect(loadMore).toHaveBeenCalledTimes(1);
  });

  function render(
    value: ReturnTypeOfUseFollowingDirectory,
    collapsed: boolean,
    onCollapse: () => void,
    onOpenUser: (user: RelationUser) => void
  ) {
    act(() => root.render(
      <FollowingFeedDirectory
        directory={value}
        collapsed={collapsed}
        onCollapse={onCollapse}
        onOpenUser={onOpenUser}
      />
    ));
  }

  function required<T extends Element>(selector: string): T {
    const element = container.querySelector<T>(selector);
    if (!element) throw new Error(`missing element: ${selector}`);
    return element;
  }

  function requiredButton(text: string): HTMLButtonElement {
    const button = [...container.querySelectorAll<HTMLButtonElement>("button")]
      .find((candidate) => candidate.textContent?.includes(text));
    if (!button) throw new Error(`missing button: ${text}`);
    return button;
  }
});

function user(id: number): RelationUser {
  return {
    user_id: id,
    account: "creator",
    nickname: "Creator",
    avatar_url: "",
    bio: "bio",
    followed_at: "2026-08-08T00:00:00Z"
  };
}

function directory(
  overrides: Partial<ReturnTypeOfUseFollowingDirectory> = {}
): ReturnTypeOfUseFollowingDirectory {
  return {
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
    setUserActive: vi.fn(),
    ...overrides
  };
}

function click(button: HTMLButtonElement) {
  act(() => button.click());
}
