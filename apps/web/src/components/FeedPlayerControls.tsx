import { useEffect, useRef, useState } from "react";
import type { KeyboardEvent as ReactKeyboardEvent } from "react";
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
  const qualityOptions = [
    { value: "auto", label: "自动" },
    ...state.qualities.map((quality) => ({ value: quality.id, label: quality.label }))
  ];

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
        <PlayerChoiceMenu
          label="清晰度"
          options={qualityOptions}
          value={state.selectedQuality}
          onSelect={onSelectQuality}
        />
        <PlayerChoiceMenu
          label="播放速度"
          options={SUPPORTED_PLAYBACK_RATES.map((rate) => ({ value: String(rate), label: `${rate}x` }))}
          value={String(state.playbackRate)}
          onSelect={(value) => onSelectRate(Number(value))}
        />
        <button
          type="button"
          aria-label={continuousPlay ? "关闭连续播放" : "开启连续播放"}
          aria-pressed={continuousPlay}
          className={`player-continuous-toggle ${continuousPlay ? "active" : ""}`}
          onClick={onToggleContinuousPlay}
        >
          <span>自动连播</span>
          <span className="player-toggle-track" aria-hidden="true"><i /></span>
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

interface PlayerChoiceMenuProps {
  label: string;
  value: string;
  options: ReadonlyArray<{ value: string; label: string }>;
  onSelect: (value: string) => void;
}

function PlayerChoiceMenu({ label, value, options, onSelect }: PlayerChoiceMenuProps) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const optionRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const typeahead = useRef("");
  const typeaheadTimer = useRef(0);
  const selected = options.find((option) => option.value === value) || options[0];

  useEffect(() => {
    if (!open) return undefined;
    typeahead.current = "";
    const selectedIndex = Math.max(0, options.findIndex((option) => option.value === value));
    optionRefs.current[selectedIndex]?.focus();
    const handlePointerDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      setOpen(false);
      triggerRef.current?.focus();
    };
    document.addEventListener("pointerdown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [open, value]);

  useEffect(() => () => window.clearTimeout(typeaheadTimer.current), []);

  function handleTriggerKeyDown(event: ReactKeyboardEvent<HTMLButtonElement>) {
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    event.preventDefault();
    setOpen(true);
  }

  function handleMenuKeyDown(event: ReactKeyboardEvent<HTMLDivElement>) {
    const currentIndex = optionRefs.current.findIndex((option) => option === document.activeElement);
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const direction = event.key === "ArrowDown" ? 1 : -1;
      const nextIndex = (Math.max(0, currentIndex) + direction + options.length) % options.length;
      optionRefs.current[nextIndex]?.focus();
      return;
    }
    if (event.key === "Home" || event.key === "End") {
      event.preventDefault();
      optionRefs.current[event.key === "Home" ? 0 : options.length - 1]?.focus();
      return;
    }
    if (event.key === "Tab") {
      setOpen(false);
      return;
    }
    if (
      event.key.length !== 1
      || event.altKey
      || event.ctrlKey
      || event.metaKey
    ) return;
    typeahead.current += event.key.toLocaleLowerCase();
    window.clearTimeout(typeaheadTimer.current);
    typeaheadTimer.current = window.setTimeout(() => {
      typeahead.current = "";
    }, 600);
    const matchIndex = options.findIndex((option) =>
      option.label.toLocaleLowerCase().startsWith(typeahead.current)
    );
    if (matchIndex >= 0) optionRefs.current[matchIndex]?.focus();
  }

  return (
    <div ref={rootRef} className={`player-choice ${open ? "open" : ""}`}>
      <button
        ref={triggerRef}
        type="button"
        className="player-choice-trigger"
        aria-label={`${label}，当前 ${selected.label}`}
        aria-expanded={open}
        aria-haspopup="menu"
        onClick={() => setOpen((current) => !current)}
        onKeyDown={handleTriggerKeyDown}
      >
        <span>{selected.label}</span>
        <Icon name={open ? "chevron-down" : "chevron-up"} size={12} />
      </button>
      {open && (
        <div className="player-choice-menu" role="menu" aria-label={label} onKeyDown={handleMenuKeyDown}>
          {options.map((option, index) => (
            <button
              ref={(element) => {
                optionRefs.current[index] = element;
              }}
              type="button"
              className={option.value === value ? "active" : ""}
              key={option.value}
              role="menuitemradio"
              aria-checked={option.value === value}
              onClick={() => {
                onSelect(option.value);
                setOpen(false);
                triggerRef.current?.focus();
              }}
            >
              <span>{option.label}</span>
              {option.value === value && <Icon name="check" size={14} />}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function playerStatusText(state: Readonly<NormalizedPlayerState>): string {
  if (state.status === "loading") return "正在加载视频";
  if (state.status === "buffering") return "缓冲中";
  if (state.status === "error") {
    switch (state.error?.category) {
      case "autoplay":
        return "浏览器已阻止自动播放，请手动播放";
      case "network":
        return "视频加载中断，请检查网络后重试";
      case "manifest":
        return "视频清单加载失败，请稍后重试";
      case "unsupported_codec":
        return "当前浏览器不支持该视频格式";
      case "decode":
        return "视频解码失败，请更换浏览器或稍后重试";
      case "source_unavailable":
        return "视频源暂时不可用，请稍后重试";
      default:
        return "视频播放失败，请稍后重试";
    }
  }
  if (state.fallback) return "自适应播放不可用，已切换兼容画质";
  return "";
}
