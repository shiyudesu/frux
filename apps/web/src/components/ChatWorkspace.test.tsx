// @vitest-environment jsdom
import { act, useEffect, useState } from "react";
import type { ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { emptyProfile } from "../constants";
import { RouterProvider } from "../router";
import { SessionProvider, useSession } from "../session";
import type {
  ChatConversation,
  ChatHistoryPage,
  ChatMessage
} from "../types";
import { ChatWorkspace } from "./ChatWorkspace";

const chatAPI = vi.hoisted(() => ({
  fetchChatConversations: vi.fn(),
  fetchChatHistory: vi.fn(),
  markChatRead: vi.fn(),
  sendChatMessage: vi.fn()
}));

vi.mock("../api/chat", () => chatAPI);
vi.mock("../hooks/useChatPolling", () => ({
  useChatPolling: () => ({ degraded: false, retry: vi.fn() })
}));

describe("ChatWorkspace", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    localStorage.clear();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(
      JSON.stringify({ code: "AUTH_INVALID_REFRESH_SESSION", error: "expired" }),
      { status: 401, headers: { "Content-Type": "application/json" } }
    )));
    chatAPI.fetchChatConversations.mockReset();
    chatAPI.fetchChatHistory.mockReset();
    chatAPI.markChatRead.mockReset();
    chatAPI.sendChatMessage.mockReset();
    chatAPI.fetchChatConversations.mockResolvedValue({
      items: [conversation(1, "Alice"), conversation(2, "Bob")],
      next_cursor: "",
      has_more: false
    });
    chatAPI.fetchChatHistory.mockImplementation((_token: string, id: number) =>
      Promise.resolve(historyPage(id, id === 1 ? "Alice" : "Bob", []))
    );
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    localStorage.clear();
    vi.unstubAllGlobals();
  });

  it("does not apply a pending send after switching conversations", async () => {
    const pending = deferred<{ message: ChatMessage; created: boolean }>();
    chatAPI.sendChatMessage.mockReturnValue(pending.promise);
    await render(<WorkspaceHarness />);

    typeText(required<HTMLTextAreaElement>("textarea[aria-label='私信内容']"), "old response");
    click(buttonByText("发送"));
    click(required<HTMLButtonElement>("[data-testid='switch-conversation']"));
    await act(async () => {
      await Promise.resolve();
    });
    expect(container.textContent).toContain("Bob");

    await act(async () => {
      pending.resolve({ message: message(20, 1, "old response"), created: true });
      await pending.promise;
    });
    expect(container.textContent).not.toContain("old response");
    expect(container.textContent).toContain("Bob");
  });

  it("rejects a send response whose message belongs to another conversation", async () => {
    chatAPI.sendChatMessage.mockResolvedValue({
      message: message(30, 2, "mismatched local message"),
      created: true
    });
    await render(<ChatWorkspace initialConversationID={1} />);
    typeText(required<HTMLTextAreaElement>("textarea[aria-label='私信内容']"), "hello");
    click(buttonByText("发送"));
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(container.textContent).not.toContain("mismatched local message");
  });

  async function render(node: ReactNode) {
    await act(async () => {
      root.render(
        <RouterProvider>
          <SessionProvider>
            <AuthenticatedSessionGate>
              {node}
            </AuthenticatedSessionGate>
          </SessionProvider>
        </RouterProvider>
      );
      await Promise.resolve();
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

function WorkspaceHarness() {
  const [conversationID, setConversationID] = useState(1);
  return (
    <>
      <button
        data-testid="switch-conversation"
        type="button"
        onClick={() => setConversationID(2)}
      >
        switch
      </button>
      <ChatWorkspace initialConversationID={conversationID} />
    </>
  );
}

function conversation(id: number, nickname: string): ChatConversation {
  return {
    id,
    counterpart: {
      user_id: id + 10,
      nickname,
      avatar_url: "",
      bio: "",
      available: true
    },
    last_message_id: 0,
    unread_count: 0
  };
}

function historyPage(id: number, nickname: string, items: ChatMessage[]): ChatHistoryPage {
  return {
    items,
    next_cursor: "",
    has_more: false,
    conversation: conversation(id, nickname),
    eligibility: { eligible: true, reason: "ELIGIBLE", conversation_id: id }
  };
}

function message(id: number, conversationID: number, text: string): ChatMessage {
  return {
    id,
    conversation_id: conversationID,
    sender: {
      user_id: 1,
      nickname: "Owner",
      avatar_url: "",
      bio: "",
      available: true
    },
    kind: "TEXT",
    text,
    created_at: "2026-08-19T00:00:00Z"
  };
}

function typeText(input: HTMLTextAreaElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")?.set;
  setter?.call(input, value);
  act(() => input.dispatchEvent(new Event("input", { bubbles: true })));
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
