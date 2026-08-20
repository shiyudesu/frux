// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useChatHistory } from "./useChatHistory";
import type {
  ChatConversation,
  ChatHistoryPage,
  ChatMessage
} from "../types";

const chatAPI = vi.hoisted(() => ({
  fetchChatHistory: vi.fn()
}));

vi.mock("../api/chat", () => chatAPI);

describe("useChatHistory", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    chatAPI.fetchChatHistory.mockReset();
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  it("refreshes an empty conversation and consumes server metadata", async () => {
    const refresh = deferred<ChatHistoryPage>();
    chatAPI.fetchChatHistory
      .mockResolvedValueOnce(page(7, "Alice", [], true))
      .mockReturnValueOnce(refresh.promise);
    await render(<HistoryHarness conversationID={7} token="account-a" />);

    expect(text("conversation")).toBe("Alice");
    expect(text("eligibility")).toBe("true");
    click(required<HTMLButtonElement>("[data-testid='refresh']"));
    expect(chatAPI.fetchChatHistory).toHaveBeenLastCalledWith("account-a", 7, "", 30, 0);

    await act(async () => {
      refresh.resolve(page(7, "Alice", [message(11, 7, "remote")], false));
      await refresh.promise;
    });
    expect(container.textContent).toContain("remote");
  });

  it("ignores stale conversation and account responses", async () => {
    const firstConversation = deferred<ChatHistoryPage>();
    const secondConversation = deferred<ChatHistoryPage>();
    const nextAccount = deferred<ChatHistoryPage>();
    chatAPI.fetchChatHistory
      .mockReturnValueOnce(firstConversation.promise)
      .mockReturnValueOnce(secondConversation.promise)
      .mockReturnValueOnce(nextAccount.promise);
    await render(<HistoryHarness conversationID={7} token="account-a" />);

    await render(<HistoryHarness conversationID={8} token="account-a" />);
    await act(async () => {
      firstConversation.resolve(page(7, "Alice", [message(1, 7, "stale conversation")], true));
      await firstConversation.promise;
    });
    expect(container.textContent).not.toContain("stale conversation");

    await render(<HistoryHarness conversationID={8} token="account-b" />);
    await act(async () => {
      secondConversation.resolve(page(8, "Other account", [message(2, 8, "stale account")], true));
      await secondConversation.promise;
    });
    expect(container.textContent).not.toContain("stale account");

    await act(async () => {
      nextAccount.resolve(page(8, "Current account", [message(3, 8, "current account")], true));
      await nextAccount.promise;
    });
    expect(container.textContent).toContain("current account");
  });

  it("rejects local messages from another conversation", async () => {
    chatAPI.fetchChatHistory.mockResolvedValue(page(7, "Alice", [], true));
    await render(<HistoryHarness conversationID={7} token="account-a" />);
    click(required<HTMLButtonElement>("[data-testid='add-mismatch']"));
    expect(container.textContent).not.toContain("wrong conversation");
  });

  async function render(node: React.ReactNode) {
    await act(async () => {
      root.render(node);
      await Promise.resolve();
      await Promise.resolve();
    });
  }

  function required<T extends Element>(selector: string): T {
    const element = container.querySelector<T>(selector);
    if (!element) throw new Error(`missing element: ${selector}`);
    return element;
  }

  function text(testID: string): string {
    return required<HTMLElement>(`[data-testid='${testID}']`).textContent || "";
  }
});

function HistoryHarness({
  conversationID,
  token
}: {
  conversationID: number;
  token: string;
}) {
  const history = useChatHistory(token, conversationID);
  const mismatch: ChatMessage = message(99, conversationID + 1, "wrong conversation");
  return (
    <div>
      <output data-testid="conversation">{history.conversation?.counterpart.nickname || ""}</output>
      <output data-testid="eligibility">{String(history.eligibility?.eligible ?? false)}</output>
      <div>{history.items.map((item) => item.text).join("|")}</div>
      <button type="button" data-testid="refresh" onClick={() => void history.refresh()}>refresh</button>
      <button type="button" data-testid="add-mismatch" onClick={() => history.addLocalMessage(mismatch)}>add</button>
    </div>
  );
}

function page(
  conversationID: number,
  nickname: string,
  items: ChatMessage[],
  eligible: boolean
): ChatHistoryPage {
  return {
    items,
    next_cursor: "",
    has_more: false,
    conversation: conversation(conversationID, nickname),
    eligibility: {
      eligible,
      reason: eligible ? "ELIGIBLE" : "NOT_MUTUAL_FOLLOW",
      conversation_id: conversationID
    }
  };
}

function conversation(id: number, nickname: string): ChatConversation {
  return {
    id,
    counterpart: {
      user_id: id + 100,
      nickname,
      avatar_url: "",
      bio: "",
      available: true
    },
    last_message_id: 0,
    unread_count: 0
  };
}

function message(id: number, conversationID: number, text: string): ChatMessage {
  return {
    id,
    conversation_id: conversationID,
    sender: {
      user_id: 101,
      nickname: "sender",
      avatar_url: "",
      bio: "",
      available: true
    },
    kind: "TEXT",
    text,
    created_at: "2026-08-19T00:00:00Z"
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function click(element: HTMLElement) {
  act(() => element.dispatchEvent(new MouseEvent("click", { bubbles: true })));
}
