// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RouterProvider } from "../router";
import type { SearchUser } from "../types";
import { SearchPage } from "./SearchPage";

const searchAPI = vi.hoisted(() => ({
  searchUsers: vi.fn(),
  searchVideos: vi.fn()
}));

vi.mock("../api/search", () => searchAPI);

describe("search page navigation", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    searchAPI.searchVideos.mockReset();
    searchAPI.searchUsers.mockReset();
    searchAPI.searchVideos.mockResolvedValue({ items: [video(1)], next_cursor: "", has_more: false });
    searchAPI.searchUsers.mockResolvedValue({ items: [user(2)], next_cursor: "", has_more: false });
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  it("opens existing typed video and public-profile destinations", async () => {
    window.history.replaceState({}, "", "/search?q=test");
    await render(<SearchPage query="test" tab="videos" />);
    click(required<HTMLButtonElement>('button[aria-label="打开作品：视频 1"]'));
    expect(window.location.pathname).toBe("/videos/1");

    window.history.replaceState({}, "", "/search?q=test&tab=users");
    await render(<SearchPage query="test" tab="users" />);
    click(buttonByText("用户 2"));
    expect(window.location.pathname).toBe("/users/2");
  });

  it("keeps a tall user page and loads the next cursor page", async () => {
    searchAPI.searchUsers
      .mockResolvedValueOnce({
        items: Array.from({ length: 20 }, (_, index) => user(index + 1)),
        next_cursor: "users-next",
        has_more: true
      })
      .mockResolvedValueOnce({
        items: [user(21)],
        next_cursor: "",
        has_more: false
      });

    window.history.replaceState({}, "", "/search?q=test&tab=users");
    await render(<SearchPage query="test" tab="users" />);

    expect(container.querySelectorAll(".search-user-card")).toHaveLength(20);
    expect(required<HTMLElement>(".search-page").contains(buttonByText("加载更多用户"))).toBe(true);

    await clickAndFlush(buttonByText("加载更多用户"));

    expect(searchAPI.searchUsers).toHaveBeenNthCalledWith(1, "test", "");
    expect(searchAPI.searchUsers).toHaveBeenNthCalledWith(2, "test", "users-next");
    expect(container.querySelectorAll(".search-user-card")).toHaveLength(21);
    expect(buttonByText("用户 1")).toBeTruthy();
    expect(buttonByText("用户 21")).toBeTruthy();
    expect([...container.querySelectorAll("button")].some((item) => item.textContent === "加载更多用户")).toBe(false);
  });

  it("renders and describes nickname-only user identity", async () => {
    searchAPI.searchUsers.mockResolvedValue({
      items: [legacyUser(2)],
      next_cursor: "",
      has_more: false
    });
    window.history.replaceState({}, "", "/search?q=test&tab=users");

    await render(<SearchPage query="test" tab="users" />);

    expect(container.textContent).toContain("用户按昵称匹配");
    expect(container.textContent).toContain("用户 2");
    expect(container.textContent).not.toContain("private-login-2");
  });

  async function render(node: React.ReactNode) {
    await act(async () => {
      root.render(<RouterProvider>{node}</RouterProvider>);
      await Promise.resolve();
    });
  }

  function required<T extends Element>(selector: string): T {
    const element = container.querySelector<T>(selector);
    if (!element) throw new Error(`missing element: ${selector}`);
    return element;
  }

  function buttonByText(text: string): HTMLButtonElement {
    const button = [...container.querySelectorAll("button")].find((item) => item.textContent?.includes(text));
    if (!button) throw new Error(`missing button: ${text}`);
    return button;
  }
});

function click(element: HTMLElement) {
  act(() => element.dispatchEvent(new MouseEvent("click", { bubbles: true })));
}

async function clickAndFlush(element: HTMLElement) {
  await act(async () => {
    element.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await Promise.resolve();
  });
}

function video(id: number) {
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

function user(id: number): SearchUser {
  return { id, nickname: `用户 ${id}`, avatar_url: "", bio: "" };
}

function legacyUser(id: number): SearchUser & { account: string } {
  return { ...user(id), account: `private-login-${id}` };
}
