import { useEffect, useState } from "react";
import { image } from "../constants";
import { useDialogFocus } from "../hooks/useDialogFocus";
import type { CommentsState } from "../hooks/useComments";
import type { Comment, FeedVideo, SessionUser } from "../types";
import type { PublicProfileInput } from "../utils";
import { formatMetric, formatRelativeTime, profileFromComment, profileFromFeedItem } from "../utils";
import { CommentMessage } from "./StatusMessages";
import { Icon } from "./Icon";

interface FeedDetailsPanelProps {
  item: FeedVideo | null;
  open: boolean;
  value: string;
  onChange: (value: string) => void;
  onSubmit: () => void;
  onClose: () => void;
  user: SessionUser;
  count: number;
  comments: Comment[];
  state: CommentsState;
  error: string;
  onRetry: () => void;
  authenticated: boolean;
  onOpenUser: (profile: PublicProfileInput) => void;
}

type DetailsTab = "details" | "comments";

export function FeedDetailsPanel({
  item,
  open,
  value,
  onChange,
  onSubmit,
  onClose,
  user,
  count,
  comments,
  state,
  error,
  onRetry,
  authenticated,
  onOpenUser
}: FeedDetailsPanelProps) {
  const [tab, setTab] = useState<DetailsTab>("comments");
  const [modalViewport, setModalViewport] = useState(() =>
    typeof window !== "undefined" ? window.matchMedia("(max-width: 1279px)").matches : false
  );
  const closeButtonRef = useDialogFocus<HTMLButtonElement>(open && modalViewport, onClose);

  useEffect(() => {
    if (open) {
      setTab("comments");
    }
  }, [item?.video_id, open]);

  useEffect(() => {
    const query = window.matchMedia("(max-width: 1279px)");
    const handleChange = (event: MediaQueryListEvent) => setModalViewport(event.matches);
    setModalViewport(query.matches);
    query.addEventListener("change", handleChange);
    return () => query.removeEventListener("change", handleChange);
  }, []);

  return (
    <>
      <button
        aria-label="关闭详情"
        className={`details-scrim ${open ? "open" : ""}`}
        data-dialog-allow
        type="button"
        onClick={onClose}
      />
      <aside
        aria-modal={modalViewport && open ? "true" : undefined}
        aria-hidden={!open}
        className={`details-panel ${open ? "open" : ""}`}
        data-ui="details-panel"
        role={modalViewport ? "dialog" : "complementary"}
      >
        <header className="details-header">
          <div className="details-tabs" role="tablist" aria-label="视频详情">
            <button
              aria-selected={tab === "details"}
              className={tab === "details" ? "active" : ""}
              role="tab"
              type="button"
              onClick={() => setTab("details")}
            >
              详情
            </button>
            <button
              aria-selected={tab === "comments"}
              className={tab === "comments" ? "active" : ""}
              role="tab"
              type="button"
              onClick={() => setTab("comments")}
            >
              评论 <span>{formatMetric(count)}</span>
            </button>
          </div>
          <button ref={closeButtonRef} className="icon-button small" type="button" aria-label="关闭评论" onClick={onClose}>
            <Icon name="close" size={20} />
          </button>
        </header>

        {tab === "details" ? (
          <div className="details-content">
            {item && (
              <>
                <button
                  className="details-author"
                  type="button"
                  onClick={() => onOpenUser(profileFromFeedItem(item))}
                >
                  <img src={item.avatar_url || image.creator} alt="" />
                  <span>
                    <strong>@{item.author}</strong>
                    <small>查看作者主页</small>
                  </span>
                </button>
                <h2>{item.title}</h2>
                <p>{item.description || "作者暂未填写视频简介。"}</p>
                <div className="details-metrics" aria-label="视频互动统计">
                  <span><strong>{formatMetric(item.like_count)}</strong>点赞</span>
                  <span><strong>{formatMetric(item.comment_count)}</strong>评论</span>
                  <span><strong>{formatMetric(item.favorite_count)}</strong>收藏</span>
                </div>
              </>
            )}
          </div>
        ) : (
          <>
            <div className="comment-list">
              {state === "loading" && <CommentMessage icon="hourglass" title="正在加载评论" />}
              {state === "error" && (
                <CommentMessage icon="alert" title={error || "评论加载失败"} action="重试" onAction={onRetry} />
              )}
              {state === "ready" && comments.length === 0 && <CommentMessage icon="comment" title="暂无评论" />}
              {comments.map((comment) => (
                <article className="comment-item" key={comment.id}>
                  <button className="comment-user-button" type="button" onClick={() => onOpenUser(profileFromComment(comment))}>
                    <img src={comment.user_avatar_url || image.currentUser} alt="" />
                  </button>
                  <div>
                    <div className="comment-meta">
                      <button type="button" onClick={() => onOpenUser(profileFromComment(comment))}>
                        {comment.user_nickname || `用户_${comment.user_id}`}
                      </button>
                      <span>{formatRelativeTime(comment.created_at)}</span>
                    </div>
                    <p>{comment.content}</p>
                  </div>
                </article>
              ))}
            </div>
            <form
              className="comment-form"
              onSubmit={(event) => {
                event.preventDefault();
                onSubmit();
              }}
            >
              <img src={user.avatar_url || image.currentUser} alt="" />
              <input
                value={value}
                onChange={(event) => onChange(event.target.value)}
                placeholder={authenticated ? "留下你的评论" : "登录后评论"}
                disabled={!authenticated}
              />
              <button aria-label="发送评论" disabled={!authenticated || !value.trim()}>
                <Icon name="send" size={18} />
              </button>
            </form>
          </>
        )}
      </aside>
    </>
  );
}
