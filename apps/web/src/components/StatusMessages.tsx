// 各类空态/状态提示组件：FeedMessage（Feed 页）、CommentMessage（评论面板）、PageMessage（整页）。
interface StatusMessageProps {
  icon: string;
  title: string;
  action?: string;
  onAction?: () => void;
}

export function FeedMessage({ icon, title, action, onAction }: StatusMessageProps) {
  return (
    <div className="feed-message">
      <span className="material-symbols-outlined">{icon}</span>
      <strong>{title}</strong>
      {action && <button onClick={onAction}>{action}</button>}
    </div>
  );
}

export function CommentMessage({ icon, title, action, onAction }: StatusMessageProps) {
  return (
    <div className="comment-empty">
      <span className="material-symbols-outlined">{icon}</span>
      <strong>{title}</strong>
      {action && <button onClick={onAction}>{action}</button>}
    </div>
  );
}

export function PageMessage({ icon, title, action, onAction }: StatusMessageProps) {
  return (
    <div className="page-message">
      <span className="material-symbols-outlined">{icon}</span>
      <strong>{title}</strong>
      {action && <button onClick={onAction}>{action}</button>}
    </div>
  );
}
