// 关注/粉丝弹窗。
import type { RelationTab } from "../api/social";
import type { RelationUser } from "../types";
import { RelationList } from "./RelationList";

interface RelationModalProps {
  tab: RelationTab;
  items: RelationUser[];
  /** "idle" | "loading" | "loadingMore" | "ready" | "error" */
  state: string;
  error: string;
  hasMore: boolean;
  following: Record<number, boolean>;
  busyID: number;
  currentUserID: number;
  onTabChange: (tab: RelationTab) => void;
  onClose: () => void;
  onRetry: () => void;
  onLoadMore: () => void;
  onToggleFollow: (targetUserID: number) => void;
}

export function RelationModal({
  tab,
  items,
  state,
  error,
  hasMore,
  following,
  busyID,
  currentUserID,
  onTabChange,
  onClose,
  onRetry,
  onLoadMore,
  onToggleFollow
}: RelationModalProps) {
  return (
    <div className="modal-backdrop relation-modal-backdrop" role="presentation" onClick={onClose}>
      <section className="relation-modal" onClick={(event) => event.stopPropagation()}>
        <header>
          <div>
            <p className="eyebrow">关系</p>
            <h2>{tab === "following" ? "关注的人" : "粉丝"}</h2>
          </div>
          <div className="relation-modal-actions">
            <div className="relation-tabs">
              <button className={tab === "following" ? "active" : ""} type="button" onClick={() => onTabChange("following")}>
                关注
              </button>
              <button className={tab === "followers" ? "active" : ""} type="button" onClick={() => onTabChange("followers")}>
                粉丝
              </button>
            </div>
            <button className="icon-button small" type="button" onClick={onClose} aria-label="关闭关系弹窗">
              <span className="material-symbols-outlined">close</span>
            </button>
          </div>
        </header>
        <RelationList
          tab={tab}
          items={items}
          state={state}
          error={error}
          hasMore={hasMore}
          following={following}
          busyID={busyID}
          currentUserID={currentUserID}
          onRetry={onRetry}
          onLoadMore={onLoadMore}
          onToggleFollow={onToggleFollow}
        />
      </section>
    </div>
  );
}
