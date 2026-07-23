// CommentPanel：Feed 右侧评论面板。
import { image } from "../constants";
import type { CommentsState } from "../hooks/useComments";
import type { Comment, SessionUser } from "../types";
import type { PublicProfileInput } from "../utils";
import { formatMetric, formatRelativeTime, profileFromComment } from "../utils";
import { CommentMessage } from "./StatusMessages";

export interface CommentPanelProps {
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

export function CommentPanel({
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
}: CommentPanelProps) {
  return (
    <aside className={`comment-panel ${open ? "open" : ""}`}>
      <header className="comment-header">
        <h2>
          评论 <span>{formatMetric(count)}</span>
        </h2>
        <div>
          <button className="icon-button small" aria-label="筛选评论">
            <span className="material-symbols-outlined">tune</span>
          </button>
          <button className="icon-button small" type="button" aria-label="关闭评论" onClick={onClose}>
            <span className="material-symbols-outlined">close</span>
          </button>
        </div>
      </header>
      <div className="comment-list">
        {state === "loading" && <CommentMessage icon="hourglass_top" title="正在加载评论" />}
        {state === "error" && <CommentMessage icon="sync_problem" title={error || "评论加载失败"} action="重试" onAction={onRetry} />}
        {state === "ready" && comments.length === 0 && <CommentMessage icon="chat_bubble" title="暂无评论" />}
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
          placeholder={authenticated ? "添加评论..." : "登录后评论"}
          disabled={!authenticated}
        />
        <button aria-label="发送评论" disabled={!authenticated || !value.trim()}>
          <span className="material-symbols-outlined">send</span>
        </button>
      </form>
    </aside>
  );
}
