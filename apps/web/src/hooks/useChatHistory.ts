import { useCallback, useEffect, useRef, useState } from "react";
import { apiErrorMessage } from "../api/client";
import { fetchChatHistory } from "../api/chat";
import type {
  ChatConversation,
  ChatEligibilityResponse,
  ChatHistoryPage,
  ChatMessage
} from "../types";

export type ChatHistoryState = "idle" | "loading" | "loadingOlder" | "refreshing" | "ready" | "error";

export interface ChatHistorySnapshot {
  items: ChatMessage[];
  nextCursor: string;
  hasMore: boolean;
  state: ChatHistoryState;
  error: string;
  conversation: ChatConversation | null;
  eligibility: ChatEligibilityResponse | null;
}

const emptyState: ChatHistorySnapshot = {
  items: [],
  nextCursor: "",
  hasMore: false,
  state: "idle",
  error: "",
  conversation: null,
  eligibility: null
};

export function useChatHistory(
  token: string,
  conversationID: number,
  enabled = true
) {
  const [snapshot, setSnapshot] = useState<ChatHistorySnapshot>(emptyState);
  const generation = useRef(0);
  const requestRef = useRef(0);
  const snapshotRef = useRef(snapshot);
  snapshotRef.current = snapshot;

  useEffect(() => {
    generation.current += 1;
    requestRef.current += 1;
    setSnapshot(emptyState);
  }, [conversationID, enabled, token]);

  const isCurrentRequest = useCallback((requestID: number, currentGeneration: number) => (
    requestRef.current === requestID && generation.current === currentGeneration
  ), []);

  const metadataFor = useCallback((data: ChatHistoryPage) => ({
    conversation: data.conversation?.id === conversationID ? data.conversation : null,
    eligibility: data.conversation?.id === conversationID ? data.eligibility || null : null
  }), [conversationID]);

  const loadInitial = useCallback(async () => {
    if (!enabled || !token || conversationID <= 0) return;
    const requestID = requestRef.current + 1;
    requestRef.current = requestID;
    const currentGeneration = generation.current;
    setSnapshot((state) => ({ ...state, state: "loading", error: "", items: [] }));
    try {
      const data = await fetchChatHistory(token, conversationID);
      if (!isCurrentRequest(requestID, currentGeneration)) return;
      const metadata = metadataFor(data);
      setSnapshot({
        items: sortChronologically(filterConversationMessages(data.items || [], conversationID)),
        nextCursor: data.next_cursor || "",
        hasMore: Boolean(data.has_more && data.next_cursor),
        state: "ready",
        error: "",
        ...metadata
      });
    } catch (error) {
      if (!isCurrentRequest(requestID, currentGeneration)) return;
      setSnapshot((state) => ({
        ...state,
        state: "error",
        error: apiErrorMessage(error, "私信记录加载失败")
      }));
    }
  }, [conversationID, enabled, isCurrentRequest, metadataFor, token]);

  useEffect(() => {
    if (enabled && token && conversationID > 0) void loadInitial();
  }, [conversationID, enabled, loadInitial, token]);

  const loadOlder = useCallback(async () => {
    const current = snapshotRef.current;
    if (
      !enabled ||
      !token ||
      conversationID <= 0 ||
      !current.hasMore ||
      current.state === "loadingOlder"
    ) return;
    const requestID = requestRef.current + 1;
    requestRef.current = requestID;
    const currentGeneration = generation.current;
    setSnapshot((state) => ({ ...state, state: "loadingOlder", error: "" }));
    try {
      const data = await fetchChatHistory(token, conversationID, current.nextCursor);
      if (!isCurrentRequest(requestID, currentGeneration)) return;
      const metadata = metadataFor(data);
      setSnapshot((state) => ({
        ...state,
        items: mergeMessages(filterConversationMessages(data.items || [], conversationID), state.items),
        nextCursor: data.next_cursor || "",
        hasMore: Boolean(data.has_more && data.next_cursor),
        state: "ready",
        error: "",
        ...metadata
      }));
    } catch (error) {
      if (!isCurrentRequest(requestID, currentGeneration)) return;
      setSnapshot((state) => ({
        ...state,
        state: "error",
        error: apiErrorMessage(error, "更早的私信加载失败")
      }));
    }
  }, [conversationID, enabled, isCurrentRequest, metadataFor, token]);

  const refresh = useCallback(async () => {
    const current = snapshotRef.current;
    const afterMessageID = current.items.reduce(
      (latest, item) => Math.max(latest, item.id), 0
    );
    if (!enabled || !token || conversationID <= 0) return;
    const requestID = requestRef.current + 1;
    requestRef.current = requestID;
    const currentGeneration = generation.current;
    setSnapshot((state) => ({ ...state, state: "refreshing", error: "" }));
    try {
      const data = await fetchChatHistory(token, conversationID, "", 30, afterMessageID);
      if (!isCurrentRequest(requestID, currentGeneration)) return;
      const metadata = metadataFor(data);
      setSnapshot((state) => ({
        ...state,
        items: mergeMessages(state.items, filterConversationMessages(data.items || [], conversationID)),
        nextCursor: afterMessageID > 0 ? state.nextCursor : data.next_cursor || "",
        hasMore: afterMessageID > 0
          ? state.hasMore
          : Boolean(data.has_more && data.next_cursor),
        state: "ready",
        error: "",
        ...metadata
      }));
    } catch (error) {
      if (!isCurrentRequest(requestID, currentGeneration)) return;
      setSnapshot((state) => ({
        ...state,
        state: "ready",
        error: apiErrorMessage(error, "私信同步失败")
      }));
      throw error;
    }
  }, [conversationID, enabled, isCurrentRequest, metadataFor, token]);

  const addLocalMessage = useCallback((message: ChatMessage) => {
    if (message.conversation_id !== conversationID) return false;
    setSnapshot((state) => ({
      ...state,
      items: mergeMessages(state.items, [message])
    }));
    return true;
  }, [conversationID]);

  return { ...snapshot, refresh, loadOlder, addLocalMessage, reload: loadInitial };
}

function sortChronologically(items: ChatMessage[]): ChatMessage[] {
  return mergeMessages([], items);
}

function mergeMessages(
  current: ChatMessage[],
  incoming: ChatMessage[]
): ChatMessage[] {
  const byID = new Map<number, ChatMessage>();
  for (const item of current) byID.set(item.id, item);
  for (const item of incoming) byID.set(item.id, item);
  return [...byID.values()].sort((left, right) => left.id - right.id);
}

function filterConversationMessages(
  items: ChatMessage[],
  conversationID: number
): ChatMessage[] {
  return items.filter((item) => item.conversation_id === conversationID);
}
