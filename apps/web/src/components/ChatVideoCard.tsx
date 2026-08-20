import type { ChatVideoCard as ChatVideoCardData } from "../types";
import { useNavigate } from "../router";
import { image } from "../constants";
import { Icon } from "./Icon";

export function ChatVideoCard({ card }: { card: ChatVideoCardData }) {
  const navigate = useNavigate();
  if (!card.available) {
    return (
      <div className="chat-video-card chat-video-card-unavailable" role="status">
        <Icon name="alert" size={20} />
        <span>视频已不可用</span>
      </div>
    );
  }
  return (
    <button
      className="chat-video-card"
      type="button"
      onClick={() => navigate({ route: `/videos/${card.video_id}` })}
      aria-label={`打开视频：${card.title}`}
    >
      <img src={card.cover_url || image.stage} alt="" />
      <span className="chat-video-card-copy">
        <strong>{card.title}</strong>
        <small>@{card.author_nickname}</small>
      </span>
      <Icon name="chevron-down" size={16} className="chat-video-card-arrow" />
    </button>
  );
}
