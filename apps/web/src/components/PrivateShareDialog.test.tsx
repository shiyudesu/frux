// @vitest-environment jsdom
import { act, useEffect, useState } from "react";
import type { ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import { emptyProfile } from "../constants";
import { RouterProvider } from "../router";
import { SessionProvider, useSession } from "../session";
import type { ChatRecipient, FeedVideo } from "../types";
import { PrivateShareDialog } from "./PrivateShareDialog";

const chatAPI = vi.hoisted(() => ({
  createChatConversation: vi.fn(),
  fetchChatEligibility: vi.fn(),
  fetchChatRecipients: vi.fn(),
  sendChatMessage: vi.fn()
}));

vi.mock("../api/chat", () => chatAPI);

describe("PrivateShareDialog", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    localStorage.clear();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(
      JSON.stringify({ code: "AUTH_INVALID_REFRESH_SESSION", error: "expired" }),
      { status: 401, headers: { "Content-Type": "application/json" } }
    )));
    chatAPI.createChatConversation.mockReset();
    chatAPI.fetchChatEligibility.mockReset();
    chatAPI.fetchChatRecipients.mockReset();
    chatAPI.sendChatMessage.mockReset();
    chatAPI.fetchChatRecipients.mockResolvedValue({
      items: [recipient(2, "Alice"), recipient(3, "Bob")],
      next_cursor: "",
      has_more: false
    });
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      callback(0);
      return 1;
    });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    localStorage.clear();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("keeps the selected recipient fixed while a share is pending", async () => {
    const create = deferred<{ conversation_id: number }>();
    chatAPI.createChatConversation.mockReturnValue(create.promise);
    await render(<PrivateShareDialog video={video(1)} onClose={() => {}} />);
    click(recipientButton("Alice"));
    click(buttonByText("发送视频"));

    const bob = recipientButton("Bob");
    expect(bob.disabled).toBe(true);
    click(bob);
    expect(recipientButton("Alice").className).toContain("active");

    await act(async () => {
      create.resolve({ conversation_id: 9 });
      await create.promise;
    });
  });

  it("does not show success from a response for a previous video", async () => {
    const create = deferred<{ conversation_id: number }>();
    const send = deferred<{ message: never; created: boolean }>();
    chatAPI.createChatConversation.mockReturnValue(create.promise);
    chatAPI.sendChatMessage.mockReturnValue(send.promise);
    await render(<ShareHarness />);
    click(recipientButton("Alice"));
    click(buttonByText("发送视频"));
    click(required<HTMLButtonElement>("[data-testid='switch-video']"));

    await act(async () => {
      create.resolve({ conversation_id: 10 });
      await create.promise;
      send.resolve({ message: undefined as never, created: true });
      await send.promise;
    });
    expect(container.textContent).not.toContain("视频已发送");
  });

  it("clears an ineligible selection and refreshes recipients and eligibility", async () => {
    chatAPI.createChatConversation.mockResolvedValue({ conversation_id: 11 });
    chatAPI.sendChatMessage.mockRejectedValue(
      new ApiError("not eligible", 409, "CHAT_NOT_ELIGIBLE")
    );
    chatAPI.fetchChatEligibility.mockResolvedValue({
      eligible: false,
      reason: "NOT_MUTUAL_FOLLOW"
    });
    await render(<PrivateShareDialog video={video(1)} onClose={() => {}} />);
    click(recipientButton("Alice"));
    click(buttonByText("发送视频"));
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(container.querySelector(".chat-recipient-item.active")).toBeNull();
    expect(chatAPI.fetchChatRecipients).toHaveBeenCalledTimes(2);
    expect(chatAPI.fetchChatEligibility).toHaveBeenCalledWith("chat-token", 2);
  });

  it("reuses uncertain retry keys only for the unchanged recipient and video", async () => {
    chatAPI.createChatConversation
      .mockRejectedValueOnce(new Error("network uncertain"))
      .mockRejectedValueOnce(new Error("network uncertain"))
      .mockResolvedValueOnce({ conversation_id: 12 });
    chatAPI.sendChatMessage.mockResolvedValue({ message: {}, created: true });
    await render(<PrivateShareDialog video={video(1)} onClose={() => {}} />);
    click(recipientButton("Alice"));
    click(buttonByText("发送视频"));
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    click(buttonByText("发送视频"));
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    const firstKey = chatAPI.createChatConversation.mock.calls[0]?.[2];
    const retryKey = chatAPI.createChatConversation.mock.calls[1]?.[2];
    expect(retryKey).toBe(firstKey);

    click(recipientButton("Bob"));
    click(buttonByText("发送视频"));
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    const changedRecipientKey = chatAPI.createChatConversation.mock.calls[2]?.[2];
    expect(changedRecipientKey).not.toBe(retryKey);
  });

  async function render(node: ReactNode) {
    await act(async () => {
      root.render(
        <RouterProvider>
          <SessionProvider>
            <AuthenticatedSessionGate>{node}</AuthenticatedSessionGate>
          </SessionProvider>
        </RouterProvider>
      );
      await Promise.resolve();
      await Promise.resolve();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
  }

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

  function recipientButton(name: string): HTMLButtonElement {
    const button = [...container.querySelectorAll<HTMLButtonElement>(".chat-recipient-item")]
      .find((item) => item.textContent?.includes(name));
    if (!button) throw new Error(`missing recipient: ${name}`);
    return button;
  }
});

function AuthenticatedSessionGate({ children }: { children: ReactNode }) {
  const session = useSession();
  const [ready, setReady] = useState(false);
  useEffect(() => {
    const timer = window.setTimeout(() => {
      session.setAuth("chat-token", { ...emptyProfile, id: 1, nickname: "Owner" }, 3600);
      setReady(true);
    }, 0);
    return () => window.clearTimeout(timer);
  }, []);
  return ready && session.token ? <>{children}</> : null;
}

function ShareHarness() {
  const [videoID, setVideoID] = useState(1);
  return (
    <>
      <button type="button" data-testid="switch-video" onClick={() => setVideoID(2)}>switch</button>
      <PrivateShareDialog video={video(videoID)} onClose={() => {}} />
    </>
  );
}

function recipient(id: number, nickname: string): ChatRecipient {
  return {
    user_id: id,
    nickname,
    avatar_url: "",
    bio: "",
    followed_at: "2026-08-19T00:00:00Z"
  };
}

function video(id: number): FeedVideo {
  return {
    video_id: id,
    author_id: 4,
    title: `视频 ${id}`,
    media_url: `/video-${id}.mp4`,
    cover_url: `/cover-${id}.jpg`,
    like_count: 0,
    comment_count: 0,
    favorite_count: 0,
    liked: false,
    favorited: false,
    author: "作者",
    avatar_url: "",
    description: "",
    feed_scene: "timeline",
    request_id: `request-${id}`
  };
}

function click(element: HTMLElement) {
  act(() => element.dispatchEvent(new MouseEvent("click", { bubbles: true })));
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}
