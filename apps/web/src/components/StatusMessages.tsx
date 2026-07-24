import { Icon } from "./Icon";
import type { IconName } from "./Icon";

interface StatusMessageProps {
  icon: IconName;
  title: string;
  action?: string;
  onAction?: () => void;
}

export function FeedMessage({ icon, title, action, onAction }: StatusMessageProps) {
  return (
    <div className="feed-message">
      <span className="status-icon"><Icon name={icon} /></span>
      <strong>{title}</strong>
      {action && <button onClick={onAction}>{action}</button>}
    </div>
  );
}

export function CommentMessage({ icon, title, action, onAction }: StatusMessageProps) {
  return (
    <div className="comment-empty">
      <span className="status-icon"><Icon name={icon} /></span>
      <strong>{title}</strong>
      {action && <button onClick={onAction}>{action}</button>}
    </div>
  );
}

export function PageMessage({ icon, title, action, onAction }: StatusMessageProps) {
  return (
    <div className="page-message">
      <span className="status-icon"><Icon name={icon} /></span>
      <strong>{title}</strong>
      {action && <button onClick={onAction}>{action}</button>}
    </div>
  );
}
