// 消息域 API：消息列表、已读、未读数。
import type { MarkReadResponse, MessageListResponse, UnreadStatResponse } from "../types";
import { apiRequest } from "./client";

export function fetchMessages(token: string, cursor = ""): Promise<MessageListResponse> {
  const params = new URLSearchParams({ limit: "20" });
  if (cursor) {
    params.set("cursor", cursor);
  }
  return apiRequest<MessageListResponse>(`/api/messages?${params.toString()}`, { token, auth: "consumer" });
}

/** messageIDs 为空数组表示全部已读 */
export function markMessagesRead(token: string, messageIDs: number[] = []): Promise<MarkReadResponse> {
  return apiRequest<MarkReadResponse>("/api/messages", {
    method: "PATCH",
    token,
    auth: "consumer",
    body: {
      message_ids: messageIDs
    }
  });
}

export function fetchUnreadStat(token: string): Promise<UnreadStatResponse> {
  return apiRequest<UnreadStatResponse>("/api/message-stats/unread", { token, auth: "consumer" });
}
