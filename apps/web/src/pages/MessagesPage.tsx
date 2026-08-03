// 消息中心页。迁移后通过 useSession/useNavigate/useUnreadCount 自取状态。
import { useCallback, useEffect, useState } from "react";
import { apiErrorMessage, isUnauthorized } from "../api/client";
import { fetchMessages, markMessagesRead } from "../api/messages";
import { PageMessage } from "../components/StatusMessages";
import { useNavigate } from "../router";
import type { NavigationTarget } from "../router";
import { useSession, useUnreadCount } from "../session";
import type { Message } from "../types";
import {
  appendMessages,
  formatRelativeTime,
  messageActor,
  messageBody,
  messageDiscussionTarget,
  messageIcon,
  messageTypeLabel
} from "../utils";
import { Icon } from "../components/Icon";

type MessagesState = "loading" | "loadingMore" | "ready" | "error";

export function MessagesPage() {
  const session = useSession();
  const navigate = useNavigate();
  const { refreshUnreadCount } = useUnreadCount();
  const [items, setItems] = useState<Message[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [hasMore, setHasMore] = useState(false);
  const [state, setState] = useState<MessagesState>("loading");
  const [error, setError] = useState("");
  const [busyID, setBusyID] = useState(0);
  const [markingAll, setMarkingAll] = useState(false);

  const loadMessages = useCallback(
    (cursor = "", append = false): Promise<void> => {
      if (!session.token) {
        navigate("/auth");
        return Promise.resolve();
      }
      setState(append ? "loadingMore" : "loading");
      setError("");
      return fetchMessages(session.token, cursor)
        .then((data) => {
          const nextItems = data.items || [];
          setItems((current) => (append ? appendMessages(current, nextItems) : nextItems));
          setNextCursor(data.next_cursor || "");
          setHasMore(Boolean(data.has_more && data.next_cursor));
          setState("ready");
        })
        .catch((loadError: unknown) => {
          if (isUnauthorized(loadError)) {
            session.clearAuth();
            navigate("/auth");
            return;
          }
          setError(apiErrorMessage(loadError, "消息加载失败"));
          setState("error");
        });
    },
    [navigate, session]
  );

  useEffect(() => {
    loadMessages("", false);
  }, [loadMessages]);

  async function markMessageRead(message: Message): Promise<boolean> {
    if (!message || message.is_read) return true;
    if (busyID || markingAll) return false;
    setBusyID(message.id);
    try {
      await markMessagesRead(session.token, [message.id]);
      setItems((current) =>
        current.map((item) => (item.id === message.id ? { ...item, is_read: true, read_at: new Date().toISOString() } : item))
      );
      await refreshUnreadCount();
      return true;
    } catch (markError) {
      if (isUnauthorized(markError)) {
        session.clearAuth();
        navigate("/auth");
        return false;
      }
      setError(apiErrorMessage(markError, "已读操作失败"));
      return false;
    } finally {
      setBusyID(0);
    }
  }

  async function activateMessage(message: Message) {
    await activateMessageNavigation(message, markMessageRead, navigate);
  }

  async function markAllRead() {
    if (markingAll || items.every((item) => item.is_read)) return;
    setMarkingAll(true);
    setError("");
    try {
      await markMessagesRead(session.token, []);
      setItems((current) => current.map((item) => ({ ...item, is_read: true, read_at: item.read_at || new Date().toISOString() })));
      await refreshUnreadCount();
    } catch (markError) {
      if (isUnauthorized(markError)) {
        session.clearAuth();
        navigate("/auth");
        return;
      }
      setError(apiErrorMessage(markError, "全部已读失败"));
    } finally {
      setMarkingAll(false);
    }
  }

  const unreadCount = items.filter((item) => !item.is_read).length;
  const loadingInitial = state === "loading" && items.length === 0;
  const loadingMore = state === "loadingMore";

  return (
    <main className="messages-page" data-ui="messages-page">
      <section className="messages-header">
        <div>
          <p className="eyebrow">Messages</p>
          <h1>消息中心</h1>
        </div>
        <div className="messages-actions">
          <span className="messages-count">{unreadCount > 0 ? `${unreadCount} 未读` : "已读完"}</span>
          <button className="ghost-button compact" onClick={() => loadMessages("", false)} disabled={loadingInitial || loadingMore}>
            <Icon name="refresh" size={17} />
            刷新
          </button>
          <button className="primary-button compact" onClick={markAllRead} disabled={markingAll || unreadCount === 0}>
            <Icon name="check-all" size={17} />
            {markingAll ? "处理中" : "全部已读"}
          </button>
        </div>
      </section>

      <section className="messages-list-wrap">
        {loadingInitial && <PageMessage icon="hourglass" title="正在加载消息" />}
        {state === "error" && items.length === 0 && (
          <PageMessage icon="alert" title={error || "消息加载失败"} action="重试" onAction={() => loadMessages("", false)} />
        )}
        {state === "ready" && items.length === 0 && <PageMessage icon="bell" title="暂无消息" />}
        {error && items.length > 0 && <p className="form-message">{error}</p>}
        <div className="messages-list">
          {items.map((message) => {
            const actor = messageActor(message);
            const body = messageBody(message);
            const target = messageDiscussionTarget(message);
            return (
              <button
                className={`message-item ${message.is_read ? "read" : "unread"} ${target ? "actionable" : "read-only"}`}
                key={message.id}
                type="button"
                onClick={() => activateMessage(message)}
                disabled={busyID === message.id}
              >
                <span className={`message-icon ${message.is_read ? "" : "active"}`}>
                  <Icon name={messageIcon(message.type)} filled={!message.is_read} />
                </span>
                <span className="message-copy">
                  <span className="message-title-row">
                    <strong><span className="message-type-label">{messageTypeLabel(message.type)}</span>{message.title}</strong>
                    <small>{formatRelativeTime(message.created_at)}</small>
                  </span>
                  {actor && (
                    <span className="message-actor-row">
                      <img src={actor.avatar_url} alt="" />
                      <strong>{actor.nickname}</strong>
                    </span>
                  )}
                  <span className="message-content-text">{body}</span>
                </span>
                <span className="message-state">
                  {target ? "查看讨论" : message.is_read ? "已读" : busyID === message.id ? "处理中" : "未读"}
                </span>
              </button>
            );
          })}
        </div>
        {hasMore && (
          <button className="ghost-button messages-more" onClick={() => loadMessages(nextCursor, true)} disabled={loadingMore}>
            <Icon name="chevron-down" size={17} />
            {loadingMore ? "加载中" : "加载更多"}
          </button>
        )}
      </section>
    </main>
  );
}

export async function activateMessageNavigation(
  message: Message,
  markRead: (message: Message) => Promise<boolean>,
  navigate: (target: NavigationTarget) => void
): Promise<boolean> {
  const marked = await markRead(message);
  if (!marked) return false;
  const target = messageDiscussionTarget(message);
  if (!target) return true;
  navigate(target);
  return true;
}
