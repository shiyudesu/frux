import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  apiErrorMessage,
  currentConsumerSessionEpoch,
  isUnauthorized
} from "../api/client";
import {
  markChatRead,
  sendChatMessage
} from "../api/chat";
import {
  createChatOperationKey,
  rotateChatOperationKey
} from "../chatOperations";
import { useChatConversations } from "../hooks/useChatConversations";
import { useChatHistory } from "../hooks/useChatHistory";
import { useChatPolling } from "../hooks/useChatPolling";
import { useNavigate } from "../router";
import { useSession, useUnreadCount } from "../session";
import type {
  ChatConversation,
  ChatParticipant,
  ChatMessage,
  ChatEligibilityResponse
} from "../types";
import { formatBadgeCount, formatRelativeTime, publicUserAvatar } from "../utils";
import { Icon } from "./Icon";
import { PageMessage } from "./StatusMessages";
import { ChatVideoCard } from "./ChatVideoCard";

const CHAT_TEXT_LIMIT = 2000;

interface ChatWorkspaceProps {
  initialConversationID?: number;
}

export function ChatWorkspace({ initialConversationID = 0 }: ChatWorkspaceProps) {
  const session = useSession();
  const navigate = useNavigate();
  const { refreshUnreadCount } = useUnreadCount();
  const authenticated = Boolean(session.token && session.user);
  const conversations = useChatConversations(session.token, authenticated);
  const [selectedID, setSelectedID] = useState(initialConversationID);
  const [query, setQuery] = useState("");
  const [composerText, setComposerText] = useState("");
  const [composerError, setComposerError] = useState("");
  const [composerBusy, setComposerBusy] = useState(false);
  const sendRequest = useRef(0);
  const lastMarkedMessage = useRef(0);
  const selectedIDRef = useRef(selectedID);
  selectedIDRef.current = selectedID;
  const sessionTokenRef = useRef(session.token);
  sessionTokenRef.current = session.token;
  const sessionUserIDRef = useRef(session.user?.id || 0);
  sessionUserIDRef.current = session.user?.id || 0;

  useEffect(() => {
    setSelectedID(initialConversationID);
  }, [initialConversationID]);

  useEffect(() => {
    sendRequest.current += 1;
    setComposerBusy(false);
    setComposerError("");
    setComposerText("");
  }, [selectedID, session.token, session.user?.id]);

  useEffect(() => {
    if (selectedID > 0 || conversations.items.length === 0) return;
    setSelectedID(conversations.items[0].id);
  }, [conversations.items, selectedID]);

  useEffect(() => {
    if (selectedID <= 0) return undefined;
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      setSelectedID(0);
      navigate("/messages");
    };
    window.addEventListener("keydown", handleEscape);
    return () => window.removeEventListener("keydown", handleEscape);
  }, [navigate, selectedID]);

  const selectedFromList = conversations.items.find((item) => item.id === selectedID);
  const history = useChatHistory(session.token, selectedID, authenticated && selectedID > 0);
  const refreshConversations = conversations.refresh;
  const patchUnread = conversations.patchUnread;
  const upsertConversation = conversations.upsert;
  const refreshHistory = history.refresh;
  const reloadHistory = history.reload;
  const addLocalMessage = history.addLocalMessage;
  const historyConversation = history.conversation?.id === selectedID
    ? history.conversation
    : null;
  const selectedConversation = historyConversation || selectedFromList;
  const counterpart = selectedConversation?.counterpart || null;
  const eligibility: ChatEligibilityResponse | null = historyConversation
    ? history.eligibility
    : null;

  useEffect(() => {
    lastMarkedMessage.current = 0;
  }, [selectedID, session.token, session.user?.id]);

  const refreshChat = useCallback(async () => {
    await refreshConversations();
    if (selectedID > 0) await refreshHistory();
  }, [refreshConversations, refreshHistory, selectedID]);
  const polling = useChatPolling(authenticated, refreshChat);

  useEffect(() => {
    if (!authenticated || selectedID <= 0 || history.items.length === 0) return;
    const latestReceived = [...history.items]
      .reverse()
      .find((item) => item.sender.user_id !== session.user?.id);
    if (!latestReceived || latestReceived.id <= lastMarkedMessage.current) return;
    lastMarkedMessage.current = latestReceived.id;
    void markChatRead(session.token, selectedID, latestReceived.id)
      .then((data) => {
        patchUnread(selectedID, data.unread_count);
        void refreshUnreadCount();
      })
      .catch(() => {
        lastMarkedMessage.current = Math.max(0, lastMarkedMessage.current - 1);
      });
  }, [
    authenticated,
    patchUnread,
    history.items,
    selectedID,
    session.token,
    session.user?.id,
    refreshUnreadCount
  ]);

  const selectConversation = useCallback((conversation: ChatConversation) => {
    setSelectedID(conversation.id);
    navigate({ route: `/messages/${conversation.id}` });
  }, [navigate]);

  const submitText = useCallback(async () => {
    if (!authenticated || selectedID <= 0 || composerBusy) return;
    const text = composerText.trim();
    if (!text) {
      setComposerError("请输入私信内容");
      return;
    }
    if (Array.from(text).length > CHAT_TEXT_LIMIT) {
      setComposerError(`私信不能超过 ${CHAT_TEXT_LIMIT} 个字符`);
      return;
    }
    if (eligibility && !eligibility.eligible) {
      setComposerError("需要互相关注后才能继续私信");
      return;
    }
    setComposerBusy(true);
    setComposerError("");
    const requestID = sendRequest.current + 1;
    sendRequest.current = requestID;
    const sessionEpoch = currentConsumerSessionEpoch();
    const requestConversationID = selectedID;
    const requestUserID = session.user?.id || 0;
    const requestToken = session.token;
    const identity = `${sessionEpoch}:${requestUserID}:${requestConversationID}:${text}`;
    const key = createChatOperationKey("text", identity);
    const isCurrent = () => (
      sendRequest.current === requestID
      && selectedIDRef.current === requestConversationID
      && sessionTokenRef.current === requestToken
      && sessionUserIDRef.current === requestUserID
      && currentConsumerSessionEpoch() === sessionEpoch
    );
    try {
      const result = await sendChatMessage(requestToken, requestConversationID, { kind: "TEXT", text }, key);
      const messageMatchesConversation = result.message.conversation_id === requestConversationID;
      if (messageMatchesConversation) rotateChatOperationKey("text", identity);
      if (!isCurrent() || !messageMatchesConversation) return;
      if (!addLocalMessage(result.message)) return;
      upsertConversation({
        id: requestConversationID,
        counterpart: counterpart || unavailableParticipant(),
        last_message_id: result.message.id,
        last_message: {
          id: result.message.id,
          kind: result.message.kind,
          preview: result.message.text || "私信",
          created_at: result.message.created_at
        },
        last_message_at: result.message.created_at,
        unread_count: selectedConversation?.unread_count || 0
      });
      setComposerText("");
    } catch (error) {
      if (!isCurrent()) return;
      if (isUnauthorized(error)) {
        session.clearAuth();
        navigate("/auth");
      } else {
        if (error instanceof Error && "code" in error && error.code === "CHAT_NOT_ELIGIBLE") {
          void reloadHistory();
        }
        setComposerError(apiErrorMessage(error, "私信发送失败"));
      }
    } finally {
      if (isCurrent()) setComposerBusy(false);
    }
  }, [
    authenticated,
    composerBusy,
    composerText,
    addLocalMessage,
    reloadHistory,
    upsertConversation,
    counterpart,
    eligibility,
    navigate,
    selectedConversation?.unread_count,
    selectedID,
    session
  ]);

  const filteredConversations = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase();
    if (!normalized) return conversations.items;
    return conversations.items.filter((item) => item.counterpart.nickname.toLocaleLowerCase().includes(normalized));
  }, [conversations.items, query]);

  if (!authenticated) {
    return <PageMessage icon="lock" title="登录后查看私信" action="登录" onAction={() => navigate("/auth")} />;
  }

  return (
    <section className={`chat-workspace ${selectedID > 0 ? "has-selection" : ""}`} data-ui="chat-workspace">
      <aside className="chat-conversation-column" aria-label="私信会话">
        <div className="chat-column-toolbar">
          <label className="chat-search">
            <Icon name="search" size={17} />
            <span className="sr-only">搜索会话</span>
            <input
              type="search"
              value={query}
              placeholder="搜索会话"
              onChange={(event) => setQuery(event.target.value)}
            />
          </label>
          <button
            className="icon-button"
            type="button"
            aria-label="刷新私信列表"
            onClick={() => void conversations.refresh()}
            disabled={conversations.state === "loading"}
          >
            <Icon name="refresh" size={17} />
          </button>
        </div>
        {conversations.state === "loading" && conversations.items.length === 0 && (
          <PageMessage icon="hourglass" title="正在加载私信" />
        )}
        {conversations.state === "error" && conversations.items.length === 0 && (
          <PageMessage icon="alert" title={conversations.error} action="重试" onAction={() => void conversations.refresh()} />
        )}
        {conversations.state === "ready" && conversations.items.length === 0 && (
          <PageMessage icon="message" title="暂无私信" />
        )}
        <div className="chat-conversation-list">
          {filteredConversations.map((conversation) => (
            <button
              className={`chat-conversation-item ${selectedID === conversation.id ? "active" : ""}`}
              type="button"
              key={conversation.id}
              onClick={() => selectConversation(conversation)}
            >
              <img src={publicUserAvatar(conversation.counterpart.avatar_url)} alt="" />
              <span className="chat-conversation-copy">
                <strong>{participantName(conversation.counterpart)}</strong>
                <small>{conversationSummary(conversation)}</small>
              </span>
              <span className="chat-conversation-meta">
                <time>{formatRelativeTime(conversation.last_message_at || conversation.last_message?.created_at || "")}</time>
                {conversation.unread_count > 0 && (
                  <span className="nav-badge">{formatBadgeCount(conversation.unread_count)}</span>
                )}
              </span>
            </button>
          ))}
        </div>
        {conversations.hasMore && (
          <button className="ghost-button compact chat-load-more" type="button" onClick={() => void conversations.loadMore()}>
            {conversations.state === "loadingMore" ? "加载中" : "加载更多"}
          </button>
        )}
      </aside>

      <section className="chat-detail" aria-label="私信对话">
        {!selectedID && <PageMessage icon="message" title="选择一个会话开始聊天" />}
        {selectedID > 0 && (
          <>
            <header className="chat-detail-header">
              <button
                className="icon-button chat-back"
                type="button"
                aria-label="返回私信列表"
                onClick={() => {
                  setSelectedID(0);
                  navigate("/messages");
                }}
              >
                <Icon name="chevron-down" size={18} className="chat-back-icon" />
              </button>
              <img src={publicUserAvatar(counterpart?.avatar_url)} alt="" />
              <div>
                <h2>{counterpart ? participantName(counterpart) : "用户暂不可用"}</h2>
                <small>{polling.degraded ? "同步暂时中断，稍后重试" : "私信"}</small>
              </div>
              <button className="icon-button" type="button" aria-label="刷新当前私信" onClick={() => void refreshChat()}>
                <Icon name="refresh" size={17} />
              </button>
            </header>
            <div className="chat-history" aria-live="polite">
              {history.hasMore && (
                <button className="ghost-button compact chat-older-button" type="button" onClick={() => void history.loadOlder()}>
                  {history.state === "loadingOlder" ? "加载中" : "查看更早消息"}
                </button>
              )}
              {history.state === "loading" && history.items.length === 0 && (
                <PageMessage icon="hourglass" title="正在加载聊天记录" />
              )}
              {history.state === "error" && history.items.length === 0 && (
                <PageMessage icon="alert" title={history.error} action="重试" onAction={() => void history.reload()} />
              )}
              {history.state === "ready" && history.items.length === 0 && (
                <PageMessage icon="message" title="还没有消息，发一条问候吧" />
              )}
              {history.items.map((message, index) => (
                <ChatMessageBubble
                  key={message.id}
                  message={message}
                  own={message.sender.user_id === session.user?.id}
                  previous={history.items[index - 1]}
                />
              ))}
            </div>
            <form
              className="chat-composer"
              onSubmit={(event) => {
                event.preventDefault();
                void submitText();
              }}
            >
              {eligibility && !eligibility.eligible && (
                <p className="chat-ineligible" role="status">需要互相关注后才能继续私信</p>
              )}
              {composerError && <p className="chat-inline-error" role="alert">{composerError}</p>}
              <div className="chat-composer-row">
                <textarea
                  value={composerText}
                  maxLength={CHAT_TEXT_LIMIT}
                  disabled={composerBusy || Boolean(eligibility && !eligibility.eligible)}
                  placeholder={eligibility && !eligibility.eligible ? "暂时无法发送私信" : "写下想说的话…"}
                  onChange={(event) => setComposerText(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" && !event.shiftKey) {
                      event.preventDefault();
                      void submitText();
                    }
                  }}
                  aria-label="私信内容"
                />
                <button className="primary-button compact" type="submit" disabled={composerBusy || !composerText.trim() || Boolean(eligibility && !eligibility.eligible)}>
                  <Icon name="send" size={16} />
                  {composerBusy ? "发送中" : "发送"}
                </button>
              </div>
              <small className="chat-composer-count">{Array.from(composerText).length}/{CHAT_TEXT_LIMIT}</small>
            </form>
          </>
        )}
      </section>
    </section>
  );
}

function ChatMessageBubble({
  message,
  own,
  previous
}: {
  message: ChatMessage;
  own: boolean;
  previous?: ChatMessage;
}) {
  const showDay = !previous || formatDay(previous.created_at) !== formatDay(message.created_at);
  return (
    <>
      {showDay && <time className="chat-day-divider">{formatDay(message.created_at)}</time>}
      <article className={`chat-message-row ${own ? "own" : "received"}`}>
        <div className="chat-message-bubble">
          {message.kind === "VIDEO" ? (
            message.video
              ? <ChatVideoCard card={message.video} />
              : <div className="chat-video-card chat-video-card-unavailable" role="status">视频已不可用</div>
          ) : (
            <p>{message.text || ""}</p>
          )}
          <time>{formatChatTime(message.created_at)}</time>
        </div>
      </article>
    </>
  );
}

function conversationSummary(conversation: ChatConversation): string {
  const message = conversation.last_message;
  if (!message) return "开始一段新对话";
  if (message.kind === "VIDEO") return "分享了一个视频";
  return message.preview?.trim() || "私信";
}

function formatDay(value: string): string {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return "";
  return new Intl.DateTimeFormat("zh-CN", { year: "numeric", month: "long", day: "numeric" }).format(date);
}

function formatChatTime(value: string): string {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return "";
  return new Intl.DateTimeFormat("zh-CN", { hour: "2-digit", minute: "2-digit" }).format(date);
}

function unavailableParticipant(): ChatParticipant {
  return { user_id: 0, nickname: "用户暂不可用", avatar_url: "", bio: "", available: false };
}

function participantName(participant: ChatParticipant): string {
  return participant.available && participant.nickname
    ? participant.nickname
    : "用户暂不可用";
}
