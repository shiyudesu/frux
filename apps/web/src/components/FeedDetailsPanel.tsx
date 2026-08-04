import { useEffect, useState } from "react";
import type { CommentsController } from "../hooks/useComments";
import { useDialogFocus } from "../hooks/useDialogFocus";
import type { FeedVideo, SessionUser } from "../types";
import type { PublicProfileInput } from "../utils";
import { formatMetric } from "../utils";
import { Icon } from "./Icon";
import { ThreadedComments } from "./ThreadedComments";
import { VideoDetails } from "./VideoDetails";

interface FeedDetailsPanelProps {
  item: FeedVideo | null;
  open: boolean;
  onClose: () => void;
  user: SessionUser;
  count: number;
  comments: CommentsController;
  authenticated: boolean;
  onOpenUser: (profile: PublicProfileInput) => void;
}

type DetailsTab = "details" | "comments";

export function FeedDetailsPanel({
  item,
  open,
  onClose,
  user,
  count,
  comments,
  authenticated,
  onOpenUser
}: FeedDetailsPanelProps) {
  const [tab, setTab] = useState<DetailsTab>("comments");
  const [modalViewport, setModalViewport] = useState(() =>
    typeof window !== "undefined" ? window.matchMedia("(max-width: 1279px)").matches : false
  );
  const closeButtonRef = useDialogFocus<HTMLButtonElement>(open && modalViewport, onClose);

  useEffect(() => {
    if (open) setTab("comments");
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
        hidden={modalViewport && !open}
        className={`details-panel ${open ? "open" : ""}`}
        data-presentation={modalViewport ? "drawer" : "panel"}
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
          <VideoDetails item={item} onOpenUser={onOpenUser} />
        ) : (
          <ThreadedComments
            controller={comments}
            authenticated={authenticated}
            user={user}
            canModerateThreads={Boolean(
              item && user.id > 0 && (item.author_id === user.id || user.role === "admin")
            )}
            onOpenUser={onOpenUser}
          />
        )}
      </aside>
    </>
  );
}
