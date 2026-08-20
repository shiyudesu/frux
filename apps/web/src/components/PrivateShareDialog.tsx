import { useCallback, useEffect, useRef, useState } from "react";
import {
  apiErrorMessage,
  currentConsumerSessionEpoch,
  isUnauthorized
} from "../api/client";
import {
  createChatConversation,
  fetchChatEligibility,
  fetchChatRecipients,
  sendChatMessage
} from "../api/chat";
import {
  createChatOperationKey,
  rotateChatOperationKey
} from "../chatOperations";
import { useDialogFocus } from "../hooks/useDialogFocus";
import { useNavigate } from "../router";
import { useSession } from "../session";
import type { ChatRecipient, FeedVideo } from "../types";
import { publicUserAvatar } from "../utils";
import { Icon } from "./Icon";
import { PageMessage } from "./StatusMessages";

interface PrivateShareDialogProps {
  video: FeedVideo;
  onClose: () => void;
}

type DialogState = "loading" | "ready" | "sending" | "success" | "error";

export function PrivateShareDialog({ video, onClose }: PrivateShareDialogProps) {
  const session = useSession();
  const navigate = useNavigate();
  const closeRef = useDialogFocus<HTMLButtonElement>(true, onClose);
  const [recipients, setRecipients] = useState<ChatRecipient[]>([]);
  const [cursor, setCursor] = useState("");
  const [hasMore, setHasMore] = useState(false);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<ChatRecipient | null>(null);
  const [state, setState] = useState<DialogState>("loading");
  const [error, setError] = useState("");
  const [sentConversationID, setSentConversationID] = useState(0);
  const requestRef = useRef(0);
  const operationGeneration = useRef(0);
  const selectedRef = useRef(selected);
  selectedRef.current = selected;
  const videoIDRef = useRef(video.video_id);
  videoIDRef.current = video.video_id;
  const previousVideoIDRef = useRef(video.video_id);
  const sessionTokenRef = useRef(session.token);
  sessionTokenRef.current = session.token;
  const sessionUserIDRef = useRef(session.user?.id || 0);
  sessionUserIDRef.current = session.user?.id || 0;

  const loadRecipients = useCallback(async (append = false) => {
    if (!session.token) return;
    const requestID = requestRef.current + 1;
    requestRef.current = requestID;
    setState(append ? "ready" : "loading");
    setError("");
    try {
      const data = await fetchChatRecipients(session.token, query, append ? cursor : "");
      if (requestRef.current !== requestID) return;
      setRecipients((items) => append ? mergeRecipients(items, data.items || []) : data.items || []);
      setCursor(data.next_cursor || "");
      setHasMore(Boolean(data.has_more && data.next_cursor));
      setState("ready");
    } catch (loadError) {
      if (requestRef.current !== requestID) return;
      if (isUnauthorized(loadError)) {
        session.clearAuth();
        navigate("/auth");
        return;
      }
      setError(apiErrorMessage(loadError, "可私信用户加载失败"));
      setState("error");
    }
  }, [cursor, navigate, query, session]);
  const loadRecipientsRef = useRef(loadRecipients);
  loadRecipientsRef.current = loadRecipients;

  useEffect(() => {
    const previousVideoID = previousVideoIDRef.current;
    const previousRecipient = selectedRef.current;
    if (previousRecipient && previousVideoID !== video.video_id) {
      rotateOperationIdentity(
        currentConsumerSessionEpoch(),
        session.user?.id || 0,
        previousVideoID,
        previousRecipient.user_id
      );
    }
    previousVideoIDRef.current = video.video_id;
    operationGeneration.current += 1;
    requestRef.current += 1;
    selectedRef.current = null;
    setSelected(null);
    setRecipients([]);
    setCursor("");
    setHasMore(false);
    setError("");
    setSentConversationID(0);
    void loadRecipientsRef.current(false);
    return () => {
      operationGeneration.current += 1;
      requestRef.current += 1;
    };
  }, [session.token, session.user?.id, video.video_id]);

  const selectRecipient = useCallback((recipient: ChatRecipient) => {
    if (state === "sending") return;
    const previous = selectedRef.current;
    if (previous && previous.user_id !== recipient.user_id) {
      rotateOperationIdentity(
        currentConsumerSessionEpoch(),
        session.user?.id || 0,
        video.video_id,
        previous.user_id
      );
    }
    operationGeneration.current += 1;
    selectedRef.current = recipient;
    setSelected(recipient);
    setError("");
  }, [session.user?.id, state, video.video_id]);

  const clearRecipient = useCallback((
    epoch: number,
    userID: number,
    recipientID: number
  ) => {
    rotateOperationIdentity(epoch, userID, video.video_id, recipientID);
    operationGeneration.current += 1;
    selectedRef.current = null;
    setSelected(null);
  }, [video.video_id]);

  const sendVideo = useCallback(async () => {
    if (!session.token || !selected || state === "sending" || state === "success") return;
    setState("sending");
    setError("");
    const requestGeneration = operationGeneration.current;
    const requestVideoID = video.video_id;
    const requestRecipientID = selected.user_id;
    const requestUserID = session.user?.id || 0;
    const requestToken = session.token;
    const requestEpoch = currentConsumerSessionEpoch();
    const identity = chatOperationIdentity(
      requestEpoch,
      requestUserID,
      requestVideoID,
      requestRecipientID
    );
    const conversationIdentity = chatConversationIdentity(
      requestEpoch,
      requestUserID,
      requestRecipientID
    );
    const conversationKey = createChatOperationKey("conversation", conversationIdentity);
    const messageKey = createChatOperationKey("video", identity);
    const isCurrent = () => (
      operationGeneration.current === requestGeneration
      && videoIDRef.current === requestVideoID
      && selectedRef.current?.user_id === requestRecipientID
      && sessionTokenRef.current === requestToken
      && sessionUserIDRef.current === requestUserID
      && currentConsumerSessionEpoch() === requestEpoch
    );
    try {
      const conversation = await createChatConversation(requestToken, requestRecipientID, conversationKey);
      await sendChatMessage(
        requestToken,
        conversation.conversation_id,
        { kind: "VIDEO", video_id: requestVideoID },
        messageKey
      );
      rotateChatOperationKey("conversation", conversationIdentity);
      rotateChatOperationKey("video", identity);
      if (!isCurrent()) return;
      setSentConversationID(conversation.conversation_id);
      setState("success");
    } catch (sendError) {
      if (!isCurrent()) return;
      if (isUnauthorized(sendError)) {
        session.clearAuth();
        navigate("/auth");
        return;
      }
      const message = apiErrorMessage(sendError, "视频分享失败");
      const notEligible = sendError instanceof Error
        && "code" in sendError
        && sendError.code === "CHAT_NOT_ELIGIBLE";
      if (notEligible) {
        clearRecipient(requestEpoch, requestUserID, requestRecipientID);
        const reloadGeneration = operationGeneration.current;
        void Promise.allSettled([
          loadRecipients(false),
          fetchChatEligibility(requestToken, requestRecipientID)
        ]).then(() => {
          if (
            operationGeneration.current !== reloadGeneration
            || videoIDRef.current !== requestVideoID
            || sessionTokenRef.current !== requestToken
            || sessionUserIDRef.current !== requestUserID
            || currentConsumerSessionEpoch() !== requestEpoch
          ) return;
          setError(message);
          setState("error");
        });
      } else {
        setError(message);
        setState("error");
      }
    }
  }, [
    clearRecipient,
    loadRecipients,
    navigate,
    selected,
    session,
    state,
    video.video_id
  ]);

  return (
    <div className="modal-backdrop chat-share-backdrop" role="presentation">
      <section
        className="chat-share-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="chat-share-title"
      >
        <header>
          <div>
            <p className="eyebrow">Private share</p>
            <h2 id="chat-share-title">分享给私信联系人</h2>
          </div>
          <button ref={closeRef} className="icon-button" type="button" aria-label="关闭分享窗口" onClick={onClose}>
            <Icon name="close" size={20} />
          </button>
        </header>
        <div className="chat-share-video">
          <img src={video.cover_url} alt="" />
          <div>
            <strong>{video.title}</strong>
            <small>@{video.author}</small>
          </div>
        </div>
        {state === "success" ? (
          <div className="chat-share-success" role="status">
            <Icon name="check" size={24} />
            <strong>视频已发送</strong>
            <p>对方可以在私信中查看这个视频。</p>
            <div>
              <button className="ghost-button compact" type="button" onClick={onClose}>继续浏览</button>
              <button
                className="primary-button compact"
                type="button"
                onClick={() => {
                  onClose();
                  if (sentConversationID > 0) navigate({ route: `/messages/${sentConversationID}` });
                }}
              >
                打开会话
              </button>
            </div>
          </div>
        ) : (
          <>
            <label className="chat-share-search">
              <Icon name="search" size={17} />
              <span className="sr-only">筛选私信联系人</span>
              <input
                type="search"
                value={query}
                placeholder="按昵称筛选"
                disabled={state === "sending"}
                onChange={(event) => setQuery(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" && state !== "sending") {
                    event.preventDefault();
                    setCursor("");
                    void loadRecipients(false);
                  }
                }}
              />
              <button type="button" disabled={state === "sending"} onClick={() => {
                setCursor("");
                void loadRecipients(false);
              }}>筛选</button>
            </label>
            {state === "loading" && recipients.length === 0 && <PageMessage icon="hourglass" title="正在加载可私信用户" />}
            {state === "error" && recipients.length === 0 && <PageMessage icon="alert" title={error} action="重试" onAction={() => void loadRecipients(false)} />}
            {state === "ready" && recipients.length === 0 && <PageMessage icon="users" title="暂无符合条件的私信联系人" />}
            {error && recipients.length > 0 && <p className="chat-inline-error" role="alert">{error}</p>}
            <div className="chat-recipient-list">
              {recipients.map((recipient) => (
                <button
                  className={`chat-recipient-item ${selected?.user_id === recipient.user_id ? "active" : ""}`}
                  type="button"
                  disabled={state === "sending"}
                  key={recipient.user_id}
                  onClick={() => selectRecipient(recipient)}
                >
                  <img src={publicUserAvatar(recipient.avatar_url)} alt="" />
                  <span>
                    <strong>{recipient.nickname}</strong>
                    <small>互相关注</small>
                  </span>
                  {selected?.user_id === recipient.user_id && <Icon name="check" size={18} />}
                </button>
              ))}
            </div>
            {hasMore && (
              <button className="ghost-button compact chat-share-more" type="button" disabled={state === "sending"} onClick={() => void loadRecipients(true)}>
                加载更多
              </button>
            )}
            <div className="chat-share-actions">
              {state === "sending" && <span role="status">正在发送…</span>}
              <button className="ghost-button compact" type="button" onClick={onClose}>取消</button>
              <button className="primary-button compact" type="button" disabled={!selected || state === "sending"} onClick={() => void sendVideo()}>
                {state === "sending" ? "发送中" : "发送视频"}
              </button>
            </div>
          </>
        )}
      </section>
    </div>
  );
}

function mergeRecipients(current: ChatRecipient[], incoming: ChatRecipient[]): ChatRecipient[] {
  const seen = new Set(current.map((item) => item.user_id));
  return [...current, ...incoming.filter((item) => {
    if (seen.has(item.user_id)) return false;
    seen.add(item.user_id);
    return true;
  })];
}

function chatOperationIdentity(
  sessionEpoch: number,
  userID: number,
  videoID: number,
  recipientID: number
): string {
  return `${sessionEpoch}:${userID}:${videoID}:${recipientID}`;
}

function chatConversationIdentity(
  sessionEpoch: number,
  userID: number,
  recipientID: number
): string {
  return `${sessionEpoch}:${userID}:${recipientID}`;
}

function rotateOperationIdentity(
  sessionEpoch: number,
  userID: number,
  videoID: number,
  recipientID: number
): void {
  const identity = chatOperationIdentity(sessionEpoch, userID, videoID, recipientID);
  rotateChatOperationKey("conversation", chatConversationIdentity(sessionEpoch, userID, recipientID));
  rotateChatOperationKey("video", identity);
}
