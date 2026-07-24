// 作品查看弹窗。
import { image } from "../constants";
import { useDialogFocus } from "../hooks/useDialogFocus";
import type { Video } from "../types";
import { formatMetric, isVideoSource } from "../utils";
import { Icon } from "./Icon";

interface WorkViewerProps {
  video: Video;
  onClose: () => void;
}

export function WorkViewer({ video, onClose }: WorkViewerProps) {
  const media = video.media_url || video.cover_url || image.stage;
  const cover = video.cover_url || image.stage;
  const closeButtonRef = useDialogFocus<HTMLButtonElement>(true, onClose);

  return (
    <div className="modal-backdrop work-viewer-backdrop" role="presentation" onClick={onClose}>
      <section
        aria-modal="true"
        className="work-viewer"
        data-ui="work-viewer"
        role="dialog"
        onClick={(event) => event.stopPropagation()}
      >
        <header>
          <div>
            <h2>{video.title || "作品"}</h2>
            <p>{formatMetric(video.like_count || 0)} 点赞 · {formatMetric(video.comment_count || 0)} 评论</p>
          </div>
          <button ref={closeButtonRef} className="icon-button small" type="button" onClick={onClose} aria-label="关闭">
            <Icon name="close" size={19} />
          </button>
        </header>
        <div className="work-viewer-stage">
          {isVideoSource(media) ? (
            <video src={media} poster={cover} controls autoPlay playsInline />
          ) : (
            <img src={cover} alt="" />
          )}
        </div>
      </section>
    </div>
  );
}
