import {
  createInitialPlayerState,
  type NormalizedPlayerState,
  type PlaybackError,
  type PlaybackFallbackState,
  type PlaybackQuality,
  type PlaybackSource,
  type PlaybackStatus,
  type QualitySelection
} from "./types";

export type PlayerStateEvent =
  | { type: "load"; source: PlaybackSource; intendedPlay: boolean; selectedQuality: QualitySelection }
  | { type: "ready"; duration?: number; qualities?: readonly PlaybackQuality[] }
  | { type: "play-requested" }
  | { type: "playing" }
  | { type: "pause-requested" }
  | { type: "paused" }
  | { type: "buffering" }
  | { type: "ended" }
  | { type: "seeking"; time: number }
  | { type: "seeked"; time: number }
  | {
      type: "metrics";
      currentTime?: number;
      duration?: number;
      bufferedAhead?: number;
      muted?: boolean;
      volume?: number;
      playbackRate?: number;
    }
  | {
      type: "quality";
      qualities?: readonly PlaybackQuality[];
      selectedQuality?: QualitySelection;
      effectiveQualityId?: string | null;
    }
  | { type: "fallback"; fallback: PlaybackFallbackState }
  | { type: "fail"; error: PlaybackError }
  | { type: "retry" }
  | { type: "reset" };

const ALLOWED_TRANSITIONS: Readonly<Record<PlaybackStatus, ReadonlySet<PlaybackStatus>>> = {
  idle: new Set(["loading"]),
  loading: new Set(["ready", "playing", "paused", "buffering", "error", "idle"]),
  ready: new Set(["playing", "paused", "buffering", "ended", "error", "loading", "idle"]),
  playing: new Set(["paused", "buffering", "ended", "error", "loading", "idle"]),
  paused: new Set(["playing", "buffering", "ended", "error", "loading", "idle"]),
  buffering: new Set(["playing", "paused", "ready", "ended", "error", "loading", "idle"]),
  ended: new Set(["playing", "paused", "loading", "error", "idle"]),
  error: new Set(["loading", "idle"])
};

export function canTransition(from: PlaybackStatus, to: PlaybackStatus): boolean {
  return from === to || ALLOWED_TRANSITIONS[from].has(to);
}

export function transitionPlayerState(
  state: Readonly<NormalizedPlayerState>,
  event: PlayerStateEvent
): NormalizedPlayerState {
  switch (event.type) {
    case "load":
      return {
        ...createInitialPlayerState(),
        status: "loading",
        source: event.source,
        intendedPlay: event.intendedPlay,
        selectedQuality: event.selectedQuality,
        muted: state.muted,
        volume: state.volume,
        playbackRate: state.playbackRate
      };
    case "ready":
      return withStatus(state, "ready", {
        duration: finiteNonNegative(event.duration, state.duration),
        qualities: event.qualities ?? state.qualities,
        error: null
      });
    case "play-requested":
      return { ...state, intendedPlay: true, error: state.error?.category === "autoplay" ? null : state.error };
    case "playing":
      return withStatus(state, "playing", { intendedPlay: true, error: null });
    case "pause-requested":
      return { ...state, intendedPlay: false };
    case "paused":
      return state.status === "ended"
        ? { ...state, intendedPlay: false }
        : withStatus(state, "paused", { intendedPlay: false });
    case "buffering":
      return withStatus(state, "buffering");
    case "ended":
      return withStatus(state, "ended", {
        intendedPlay: false,
        currentTime: state.duration > 0 ? state.duration : state.currentTime
      });
    case "seeking":
      return {
        ...state,
        seeking: true,
        currentTime: finiteNonNegative(event.time, state.currentTime)
      };
    case "seeked":
      return {
        ...state,
        seeking: false,
        currentTime: finiteNonNegative(event.time, state.currentTime)
      };
    case "metrics":
      return {
        ...state,
        currentTime: finiteNonNegative(event.currentTime, state.currentTime),
        duration: finiteNonNegative(event.duration, state.duration),
        bufferedAhead: finiteNonNegative(event.bufferedAhead, state.bufferedAhead),
        muted: event.muted ?? state.muted,
        volume: clamped(event.volume, state.volume, 0, 1),
        playbackRate: clamped(event.playbackRate, state.playbackRate, 0.25, 4)
      };
    case "quality":
      return {
        ...state,
        qualities: event.qualities ?? state.qualities,
        selectedQuality: event.selectedQuality ?? state.selectedQuality,
        effectiveQualityId:
          event.effectiveQualityId === undefined ? state.effectiveQualityId : event.effectiveQualityId
      };
    case "fallback":
      return { ...state, fallback: event.fallback };
    case "fail":
      return withStatus(state, "error", { error: event.error });
    case "retry":
      return withStatus(state, "loading", { error: null });
    case "reset":
      return createInitialPlayerState();
  }
}

export class PlayerStateMachine {
  private state: NormalizedPlayerState = createInitialPlayerState();
  private readonly listeners = new Set<(state: Readonly<NormalizedPlayerState>) => void>();

  getState(): Readonly<NormalizedPlayerState> {
    return this.state;
  }

  dispatch(event: PlayerStateEvent): Readonly<NormalizedPlayerState> {
    const next = transitionPlayerState(this.state, event);
    if (next !== this.state) {
      this.state = next;
      for (const listener of this.listeners) listener(this.state);
    }
    return this.state;
  }

  subscribe(listener: (state: Readonly<NormalizedPlayerState>) => void): () => void {
    this.listeners.add(listener);
    listener(this.state);
    return () => this.listeners.delete(listener);
  }

  clearSubscribers(): void {
    this.listeners.clear();
  }
}

function withStatus(
  state: Readonly<NormalizedPlayerState>,
  status: PlaybackStatus,
  patch: Partial<NormalizedPlayerState> = {}
): NormalizedPlayerState {
  if (!canTransition(state.status, status)) return { ...state };
  return { ...state, ...patch, status };
}

function finiteNonNegative(value: number | undefined, fallback: number): number {
  return value !== undefined && Number.isFinite(value) ? Math.max(0, value) : fallback;
}

function clamped(value: number | undefined, fallback: number, min: number, max: number): number {
  return value !== undefined && Number.isFinite(value) ? Math.min(max, Math.max(min, value)) : fallback;
}
