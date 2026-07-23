// 作品网格：资料页"我的作品"/"他的作品"列表。
import { image } from "../constants";
import type { Video } from "../types";
import { formatMetric } from "../utils";

interface VideoGridProps {
  videos: Video[];
  /** "loading" | "ready" | 错误文案 */
  state: string;
  onSelect: (video: Video) => void;
}

export function VideoGrid({ videos, state, onSelect }: VideoGridProps) {
  return (
    <div className="work-list">
      {state === "loading" && <p className="card-empty">正在加载作品</p>}
      {state !== "loading" && typeof state === "string" && state !== "ready" && <p className="card-empty">{state}</p>}
      {state === "ready" && videos.length === 0 && <p className="card-empty">暂无作品</p>}
      {videos.map((video) => (
        // 后端作品必有 id；迁移前的 `video.id || video.video_id` 中 video_id 恒为 undefined
        <button className="work-item" key={video.id} onClick={() => onSelect(video)}>
          <div className="work-thumb">
            <img src={video.cover_url || image.stage} alt="" />
            <span className="material-symbols-outlined">play_arrow</span>
          </div>
          <div className="work-meta">
            <h3>{video.title}</h3>
            <p>{formatMetric(video.like_count || 0)} 点赞 · {formatMetric(video.comment_count || 0)} 评论</p>
            <span className="status-badge">{video.status === 0 ? "审核中" : "已发布"}</span>
          </div>
        </button>
      ))}
    </div>
  );
}
