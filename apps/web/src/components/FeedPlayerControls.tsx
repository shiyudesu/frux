import { Icon } from "./Icon";

interface FeedPlayerControlsProps {
  playing: boolean;
  muted: boolean;
  currentTime: number;
  duration: number;
  fullscreen: boolean;
  onTogglePlayback: () => void;
  onToggleMute: () => void;
  onSeek: (value: number) => void;
  onToggleFullscreen: () => void;
}

function formatPlaybackTime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return "00:00";
  const total = Math.floor(seconds);
  const minutes = Math.floor(total / 60);
  const remaining = total % 60;
  return `${String(minutes).padStart(2, "0")}:${String(remaining).padStart(2, "0")}`;
}

export function FeedPlayerControls({
  playing,
  muted,
  currentTime,
  duration,
  fullscreen,
  onTogglePlayback,
  onToggleMute,
  onSeek,
  onToggleFullscreen
}: FeedPlayerControlsProps) {
  const safeDuration = Number.isFinite(duration) && duration > 0 ? duration : 0;
  const safeCurrentTime = safeDuration > 0 ? Math.min(currentTime, safeDuration) : 0;

  return (
    <div className="player-controls" data-ui="player-controls">
      <label className="player-progress">
        <span className="sr-only">播放进度</span>
        <input
          aria-label="播放进度"
          max={safeDuration || 1}
          min="0"
          step="0.1"
          type="range"
          value={safeCurrentTime}
          disabled={!safeDuration}
          onChange={(event) => onSeek(Number(event.target.value))}
        />
        <span
          className="player-progress-fill"
          style={{ width: `${safeDuration ? (safeCurrentTime / safeDuration) * 100 : 0}%` }}
        />
      </label>
      <div className="player-controls-row">
        <button type="button" onClick={onTogglePlayback} aria-label={playing ? "暂停" : "播放"}>
          <Icon name={playing ? "pause" : "play"} size={20} />
        </button>
        <span className="player-time">
          {formatPlaybackTime(safeCurrentTime)} / {formatPlaybackTime(safeDuration)}
        </span>
        <span className="player-controls-spacer" />
        <button type="button" onClick={onToggleMute} aria-label={muted ? "打开声音" : "静音"}>
          <Icon name={muted ? "volume-off" : "volume"} size={20} />
        </button>
        <button type="button" onClick={onToggleFullscreen} aria-label={fullscreen ? "退出全屏" : "全屏"}>
          <Icon name="fullscreen" size={20} />
        </button>
      </div>
    </div>
  );
}
