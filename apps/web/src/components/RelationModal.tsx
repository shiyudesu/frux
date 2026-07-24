// 关注/粉丝弹窗。
import type { RelationTab } from "../api/social";
import type { RelationUser } from "../types";
import { useDialogFocus } from "../hooks/useDialogFocus";
import { RelationList } from "./RelationList";
import { Icon } from "./Icon";

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
  const closeButtonRef = useDialogFocus<HTMLButtonElement>(true, onClose);

  return (
    <div className="modal-backdrop relation-modal-backdrop" role="presentation" onClick={onClose}>
      <section
        aria-modal="true"
        className="relation-modal"
        data-ui="relation-modal"
        role="dialog"
        onClick={(event) => event.stopPropagation()}
      >
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
            <button ref={closeButtonRef} className="icon-button small" type="button" onClick={onClose} aria-label="关闭关系弹窗">
              <Icon name="close" size={19} />
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
