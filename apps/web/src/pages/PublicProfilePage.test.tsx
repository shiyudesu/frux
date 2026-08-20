// @vitest-environment jsdom
import { act, useEffect, useState } from "react";
import type { ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RouterProvider } from "../router";
import { SessionProvider, useSession } from "../session";
import { emptyProfile } from "../constants";
import type { Video } from "../types";
import { PublicProfilePage } from "./PublicProfilePage";

const accountAPI = vi.hoisted(() => ({
  fetchPublicProfile: vi.fn(),
  fetchUserVideos: vi.fn()
}));
const chatAPI = vi.hoisted(() => ({
  createChatConversation: vi.fn(),
  fetchChatEligibility: vi.fn()
}));

vi.mock("../api/account", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/account")>()),
  ...accountAPI
}));
vi.mock("../api/chat", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/chat")>()),
  ...chatAPI
}));

describe("public profile playback", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    localStorage.clear();
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    vi.useFakeTimers();
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      callback(0);
      return 1;
    });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: vi.fn((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: () => {},
        removeListener: () => {},
        addEventListener: () => {},
        removeEventListener: () => {},
        dispatchEvent: () => true
      }))
    });
    accountAPI.fetchPublicProfile.mockReset();
    accountAPI.fetchUserVideos.mockReset();
    chatAPI.createChatConversation.mockReset();
    chatAPI.fetchChatEligibility.mockReset();
    accountAPI.fetchPublicProfile.mockResolvedValue({
      id: 2,
      nickname: "作者",
      avatar_url: "",
      bio: "",
      following_count: 0,
      follower_count: 0,
      work_count: 2,
      gender: 2,
      public_work_count: 2,
      received_like_count: 0,
      liked_videos_public: false
    });
    accountAPI.fetchUserVideos.mockResolvedValue({
      items: [video(1), video(2)],
      limit: 24,
      offset: 0
    });
    chatAPI.fetchChatEligibility.mockResolvedValue({
      eligible: true,
      reason: "ELIGIBLE",
      conversation_id: 8
    });
    chatAPI.createChatConversation.mockResolvedValue({ conversation_id: 8 });
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    localStorage.clear();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("opens author works in the feed-style continuous queue", async () => {
    await act(async () => {
      root.render(
        <RouterProvider>
          <SessionProvider>
            <PublicProfilePage userID={2} />
          </SessionProvider>
        </RouterProvider>
      );
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(accountAPI.fetchUserVideos).toHaveBeenCalledWith(2, 24, 0);
    expect(container.textContent).toContain("作者");
    expect(container.textContent).toContain("女");
    expect(container.textContent).not.toContain("账号：");
    click(required<HTMLButtonElement>('button[aria-label="打开作品：作品 1"]'));

    const queue = required<HTMLElement>(".collection-queue-dialog");
    expect(queue.dataset.source).toBe("publicWorks");
    expect(queue.dataset.activeVideoId).toBe("1");
    expect(container.querySelector(".work-viewer")).toBeNull();

    click(required<HTMLButtonElement>('button[aria-label="下一个视频"]'));
    act(() => vi.advanceTimersByTime(400));
    expect(queue.dataset.activeVideoId).toBe("2");
  });

  it("loads chat eligibility and opens an authorized conversation", async () => {
    act(() => {
      root.render(
        <RouterProvider>
          <SessionProvider>
            <AuthenticatedSessionGate>
              <PublicProfilePage userID={2} />
            </AuthenticatedSessionGate>
          </SessionProvider>
        </RouterProvider>
      );
    });
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    await act(async () => {
      vi.runOnlyPendingTimers();
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(chatAPI.fetchChatEligibility).toHaveBeenCalledWith("profile-token", 2);
    click(buttonByText("私信"));
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(chatAPI.createChatConversation).toHaveBeenCalledWith(
      "profile-token",
      2,
      expect.any(String)
    );
    expect(window.location.pathname).toBe("/messages/8");
  });

  function required<T extends Element>(selector: string): T {
    const element = container.querySelector<T>(selector);
    if (!element) throw new Error(`missing element: ${selector}`);
    return element;
  }

  function buttonByText(text: string): HTMLButtonElement {
    const button = [...container.querySelectorAll("button")].find((item) => item.textContent?.trim() === text);
    if (!button) throw new Error(`missing button: ${text}`);
    return button;
  }
});

function AuthenticatedSessionGate({ children }: { children: ReactNode }) {
  const session = useSession();
  const [ready, setReady] = useState(false);
  useEffect(() => {
    const timer = window.setTimeout(() => {
      session.setAuth("profile-token", { ...emptyProfile, id: 1, nickname: "Owner" }, 3600);
      setReady(true);
    }, 0);
    return () => window.clearTimeout(timer);
  }, []);
  return ready && session.token ? <>{children}</> : null;
}

function click(element: HTMLElement) {
  act(() => element.dispatchEvent(new MouseEvent("click", { bubbles: true })));
}

function video(id: number): Video {
  const timestamp = `2026-08-${String(id).padStart(2, "0")}T00:00:00Z`;
  return {
    id,
    author_id: 2,
    title: `作品 ${id}`,
    description: "",
    media_url: `/image-${id}.jpg`,
    cover_url: `/image-${id}.jpg`,
    status: 2,
    visibility: "public",
    like_count: 0,
    comment_count: 0,
    favorite_count: 0,
    published_at: timestamp,
    created_at: timestamp,
    updated_at: timestamp
  };
}
