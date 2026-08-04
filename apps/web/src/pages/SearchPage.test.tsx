// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RouterProvider } from "../router";
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

function user(id: number) {
  return { id, account: `user${id}`, nickname: `用户 ${id}`, avatar_url: "", bio: "" };
}
