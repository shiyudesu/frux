import {
  SUPPORTED_PLAYBACK_RATES,
  type NormalizedPlayerState,
  type QualitySelection
} from "../player";
import { Icon } from "./Icon";

interface FeedPlayerControlsProps {
  state: Readonly<NormalizedPlayerState>;
  fullscreen: boolean;
  continuousPlay: boolean;
  onTogglePlayback: () => void;
  onToggleMute: () => void;
  onSeek: (value: number) => void;
  onSelectQuality: (selection: QualitySelection) => void;
  onSelectRate: (rate: number) => void;
  onToggleContinuousPlay: () => void;
  onRetry: () => void;
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
  state,
  fullscreen,
  continuousPlay,
  onTogglePlayback,
  onToggleMute,
  onSeek,
  onSelectQuality,
  onSelectRate,
  onToggleContinuousPlay,
  onRetry,
  onToggleFullscreen
}: FeedPlayerControlsProps) {
  const safeDuration = Number.isFinite(state.duration) && state.duration > 0 ? state.duration : 0;
  const safeCurrentTime = safeDuration > 0 ? Math.min(state.currentTime, safeDuration) : 0;
  const playing = state.status === "playing" || (state.status === "buffering" && state.intendedPlay);
  const busy = state.status === "loading";
  const statusText = playerStatusText(state);

  return (
    <div className="player-controls" data-ui="player-controls">
      {statusText && (
        <div className={`player-status ${state.status}`} role="status" aria-live="polite">
          {statusText}
          {state.error?.recoverable && (
            <button type="button" className="player-retry" onClick={onRetry}>
              重试
            </button>
          )}
        </div>
      )}
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
        <button type="button" onClick={onTogglePlayback} aria-label={playing ? "暂停" : "播放"} disabled={busy}>
          <Icon name={playing ? "pause" : "play"} size={20} />
        </button>
        <span className="player-time">
          {formatPlaybackTime(safeCurrentTime)} / {formatPlaybackTime(safeDuration)}
        </span>
        <span className="player-controls-spacer" />
        <label className="player-select">
          <span className="sr-only">清晰度</span>
          <select
            aria-label="清晰度"
            value={state.selectedQuality}
            onChange={(event) => onSelectQuality(event.target.value)}
          >
            <option value="auto">自动</option>
            {state.qualities.map((quality) => (
              <option key={quality.id} value={quality.id}>
                {quality.label}{quality.active ? " · 当前" : ""}
              </option>
            ))}
          </select>
        </label>
        <label className="player-select">
          <span className="sr-only">播放速度</span>
          <select
            aria-label="播放速度"
            value={state.playbackRate}
            onChange={(event) => onSelectRate(Number(event.target.value))}
          >
            {SUPPORTED_PLAYBACK_RATES.map((rate) => (
              <option key={rate} value={rate}>{rate}x</option>
            ))}
          </select>
        </label>
        <button
          type="button"
          aria-label={continuousPlay ? "关闭连续播放" : "开启连续播放"}
          aria-pressed={continuousPlay}
          className={continuousPlay ? "active" : ""}
          onClick={onToggleContinuousPlay}
        >
          连播
        </button>
        <button type="button" onClick={onToggleMute} aria-label={state.muted ? "打开声音" : "静音"}>
          <Icon name={state.muted ? "volume-off" : "volume"} size={20} />
        </button>
        <button type="button" onClick={onToggleFullscreen} aria-label={fullscreen ? "退出全屏" : "全屏"}>
          <Icon name="fullscreen" size={20} />
        </button>
      </div>
    </div>
  );
}

function playerStatusText(state: Readonly<NormalizedPlayerState>): string {
  if (state.status === "loading") return "正在加载视频";
  if (state.status === "buffering") return "缓冲中";
  if (state.status === "error") return state.error?.message || "播放失败";
  if (state.fallback) return "自适应播放不可用，已切换兼容画质";
  return "";
}
