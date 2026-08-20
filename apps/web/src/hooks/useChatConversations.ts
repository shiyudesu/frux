import { useCallback, useEffect, useRef, useState } from "react";
import { apiErrorMessage } from "../api/client";
import { fetchChatConversations } from "../api/chat";
import type { ChatConversation } from "../types";

export type ChatListState = "idle" | "loading" | "loadingMore" | "ready" | "error";

export interface ChatConversationState {
  items: ChatConversation[];
  nextCursor: string;
  hasMore: boolean;
  state: ChatListState;
  error: string;
}

const emptyState: ChatConversationState = {
  items: [],
  nextCursor: "",
  hasMore: false,
  state: "idle",
  error: ""
};

export function useChatConversations(token: string, enabled = true) {
  const [snapshot, setSnapshot] = useState<ChatConversationState>(emptyState);
  const generation = useRef(0);
  const requestRef = useRef(0);
  const snapshotRef = useRef(snapshot);
  snapshotRef.current = snapshot;

  useEffect(() => {
    generation.current += 1;
    requestRef.current += 1;
    setSnapshot(emptyState);
  }, [token]);

  const load = useCallback(async (append = false): Promise<void> => {
    if (!enabled || !token) return;
    const current = snapshotRef.current;
    if (append && (!current.hasMore || current.state === "loadingMore")) return;
    const requestID = requestRef.current + 1;
    requestRef.current = requestID;
    const currentGeneration = generation.current;
    const cursor = append ? current.nextCursor : "";
    setSnapshot((state) => ({
      ...state,
      state: append ? "loadingMore" : "loading",
      error: "",
      items: append ? state.items : []
    }));
    try {
      const data = await fetchChatConversations(token, cursor);
      if (
        requestRef.current !== requestID ||
        generation.current !== currentGeneration
      ) return;
      const incoming = data.items || [];
      setSnapshot((state) => ({
        items: append ? mergeConversations(state.items, incoming) : mergeConversations([], incoming),
        nextCursor: data.next_cursor || "",
        hasMore: Boolean(data.has_more && data.next_cursor),
        state: "ready",
        error: ""
      }));
    } catch (error) {
      if (
        requestRef.current !== requestID ||
        generation.current !== currentGeneration
      ) return;
      setSnapshot((state) => ({
        ...state,
        state: "error",
        error: apiErrorMessage(error, "私信列表加载失败")
      }));
    }
  }, [enabled, token]);

  useEffect(() => {
    if (enabled && token) void load(false);
  }, [enabled, load, token]);

  const refresh = useCallback(() => load(false), [load]);
  const loadMore = useCallback(() => load(true), [load]);
  const patchUnread = useCallback((conversationID: number, unreadCount: number) => {
    setSnapshot((state) => ({
      ...state,
      items: state.items.map((item) => item.id === conversationID
        ? { ...item, unread_count: Math.max(0, unreadCount) }
        : item)
    }));
  }, []);
  const upsert = useCallback((conversation: ChatConversation) => {
    setSnapshot((state) => ({
      ...state,
      items: mergeConversations(state.items, [conversation])
    }));
  }, []);

  return { ...snapshot, refresh, loadMore, patchUnread, upsert };
}

function mergeConversations(
  current: ChatConversation[],
  incoming: ChatConversation[]
): ChatConversation[] {
  const byID = new Map<number, ChatConversation>();
  for (const item of current) byID.set(item.id, item);
  for (const item of incoming) byID.set(item.id, item);
  return [...byID.values()].sort((left, right) => {
    const leftMessage = left.last_message?.id || 0;
    const rightMessage = right.last_message?.id || 0;
    return rightMessage - leftMessage || right.id - left.id;
  });
}
