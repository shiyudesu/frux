import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createChatConversation,
  fetchChatHistory,
  sendChatMessage
} from "./chat";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("typed chat API", () => {
  it("binds conversation routes and message payloads", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      conversation_id: 8
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" }
    }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(createChatConversation("token", 2, "conversation-key")).resolves.toEqual({
      conversation_id: 8
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/chat/conversations",
      expect.objectContaining({
        method: "POST",
        headers: expect.objectContaining({
          Authorization: "Bearer token",
          "Idempotency-Key": "conversation-key"
        }),
        body: JSON.stringify({ target_user_id: 2 })
      })
    );
  });

  it("encodes history cursors and after-message refreshes", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      items: [],
      next_cursor: "",
      has_more: false
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" }
    })));

    await fetchChatHistory("token", 8, "cursor", 30, 42);
    expect(vi.mocked(fetch).mock.calls[0]?.[0]).toBe(
      "/api/chat/conversations/8/messages?limit=30&cursor=cursor&after_message_id=42"
    );
  });

  it("requires an idempotency header for message sends", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      message: {
        id: 1,
        conversation_id: 8,
        sender_id: 3,
        kind: "TEXT",
        text: "hello",
        created_at: "2026-08-19T00:00:00Z"
      },
      replayed: false
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" }
    }));
    vi.stubGlobal("fetch", fetchMock);

    await sendChatMessage("token", 8, { kind: "TEXT", text: "hello" }, "message-key");
    expect(fetchMock.mock.calls[0]?.[1]).toEqual(expect.objectContaining({
      headers: expect.objectContaining({ "Idempotency-Key": "message-key" }),
      body: JSON.stringify({ kind: "TEXT", text: "hello" })
    }));
  });
});
