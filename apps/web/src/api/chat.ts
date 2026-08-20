import type {
  ChatConversationPage,
  ChatCreateConversationResponse,
  ChatEligibilityResponse,
  ChatHistoryPage,
  ChatReadResponse,
  ChatRecipientPage,
  ChatSendRequest,
  ChatSendResponse
} from "../types";
import { apiRequest } from "./client";

export function fetchChatEligibility(
  token: string,
  targetUserID: number
): Promise<ChatEligibilityResponse> {
  return apiRequest<ChatEligibilityResponse>(
    `/api/chat/users/${targetUserID}/eligibility`,
    { token, auth: "consumer" }
  );
}

export function fetchChatRecipients(
  token: string,
  query = "",
  cursor = "",
  limit = 20
): Promise<ChatRecipientPage> {
  const params = new URLSearchParams({ limit: String(limit) });
  if (query.trim()) params.set("q", query.trim());
  if (cursor) params.set("cursor", cursor);
  return apiRequest<ChatRecipientPage>(
    `/api/chat/recipients?${params.toString()}`,
    { token, auth: "consumer" }
  );
}

export function createChatConversation(
  token: string,
  targetUserID: number,
  idempotencyKey?: string
): Promise<ChatCreateConversationResponse> {
  return apiRequest<ChatCreateConversationResponse>("/api/chat/conversations", {
    method: "POST",
    token,
    auth: "consumer",
    ...(idempotencyKey ? { headers: { "Idempotency-Key": idempotencyKey } } : {}),
    body: { target_user_id: targetUserID }
  });
}

export function fetchChatConversations(
  token: string,
  cursor = "",
  limit = 20
): Promise<ChatConversationPage> {
  const params = new URLSearchParams({ limit: String(limit) });
  if (cursor) params.set("cursor", cursor);
  return apiRequest<ChatConversationPage>(
    `/api/chat/conversations?${params.toString()}`,
    { token, auth: "consumer" }
  );
}

export function fetchChatHistory(
  token: string,
  conversationID: number,
  cursor = "",
  limit = 30,
  afterMessageID = 0
): Promise<ChatHistoryPage> {
  const params = new URLSearchParams({ limit: String(limit) });
  if (cursor) params.set("cursor", cursor);
  if (afterMessageID > 0) params.set("after_message_id", String(afterMessageID));
  return apiRequest<ChatHistoryPage>(
    `/api/chat/conversations/${conversationID}/messages?${params.toString()}`,
    { token, auth: "consumer" }
  );
}

export function sendChatMessage(
  token: string,
  conversationID: number,
  body: ChatSendRequest,
  idempotencyKey: string
): Promise<ChatSendResponse> {
  return apiRequest<ChatSendResponse>(
    `/api/chat/conversations/${conversationID}/messages`,
    {
      method: "POST",
      token,
      auth: "consumer",
      headers: { "Idempotency-Key": idempotencyKey },
      body
    }
  );
}

export function markChatRead(
  token: string,
  conversationID: number,
  throughMessageID: number
): Promise<ChatReadResponse> {
  return apiRequest<ChatReadResponse>(
    `/api/chat/conversations/${conversationID}/read`,
    {
      method: "PATCH",
      token,
      auth: "consumer",
      body: { through_message_id: throughMessageID }
    }
  );
}
