import type {
  MediaErrorLike,
  PlaybackError,
  PlaybackQuality,
  PlaybackSource,
  PlayerMediaElement,
  TimeRangesLike
} from "./types";

export function bufferedAheadFromRanges(ranges: TimeRangesLike, currentTime: number): number {
  const position = finiteNonNegative(currentTime);
  for (let index = 0; index < ranges.length; index += 1) {
    const start = ranges.start(index);
    const end = ranges.end(index);
    if (start <= position + 0.05 && end >= position) return Math.max(0, end - position);
  }
  return 0;
}

export function mediaSnapshot(element: PlayerMediaElement): {
  currentTime: number;
  duration: number;
  bufferedAhead: number;
  muted: boolean;
  volume: number;
  playbackRate: number;
} {
  return {
    currentTime: finiteNonNegative(element.currentTime),
    duration: finiteNonNegative(element.duration),
    bufferedAhead: bufferedAheadFromRanges(element.buffered, element.currentTime),
    muted: element.muted,
    volume: clamped(element.volume, 0, 1, 1),
    playbackRate: clamped(element.playbackRate, 0.25, 4, 1)
  };
}

export function qualityFromSource(
  source: PlaybackSource,
  selected = true,
  active = true
): PlaybackQuality {
  return {
    id: source.id,
    label: source.qualityLabel,
    width: source.width,
    height: source.height,
    bitrate: source.bitrate,
    selected,
    active
  };
}

export function mapNativeMediaError(error: MediaErrorLike | null, source?: PlaybackSource): PlaybackError {
  const code = error?.code ?? 0;
  const common = {
    sourceId: source?.id,
    code: `media_${code || "unknown"}`,
    message: error?.message?.trim() || nativeErrorMessage(code)
  };
  if (code === 2) return { ...common, category: "network", recoverable: true };
  if (code === 3) return { ...common, category: "decode", recoverable: false };
  if (code === 4) return { ...common, category: "unsupported_codec", recoverable: false };
  if (code === 1) return { ...common, category: "source_unavailable", recoverable: true };
  return { ...common, category: "unknown", recoverable: true };
}

export function mapPlayRejection(error: unknown, source?: PlaybackSource): PlaybackError {
  const named = errorName(error);
  const autoplay = named === "NotAllowedError";
  return {
    category: autoplay ? "autoplay" : "unknown",
    code: autoplay ? "autoplay_not_allowed" : "play_rejected",
    message: errorMessage(error, autoplay ? "Playback requires user interaction." : "Playback could not start."),
    recoverable: true,
    sourceId: source?.id
  };
}

export function isAutoplayRejection(error: unknown): boolean {
  return errorName(error) === "NotAllowedError";
}

export function errorMessage(value: unknown, fallback: string): string {
  if (value instanceof Error && value.message.trim()) return value.message.trim();
  if (typeof value === "string" && value.trim()) return value.trim();
  return fallback;
}

export function errorName(value: unknown): string {
  if (value instanceof Error) return value.name;
  if (isRecord(value) && typeof value.name === "string") return value.name;
  return "";
}

export function finiteNonNegative(value: number): number {
  return Number.isFinite(value) ? Math.max(0, value) : 0;
}

export function clamped(value: number, min: number, max: number, fallback: number): number {
  return Number.isFinite(value) ? Math.min(max, Math.max(min, value)) : fallback;
}

export function isRecord(value: unknown): value is Readonly<Record<string, unknown>> {
  return typeof value === "object" && value !== null;
}

function nativeErrorMessage(code: number): string {
  if (code === 1) return "Media loading was aborted.";
  if (code === 2) return "A network error interrupted media loading.";
  if (code === 3) return "The media could not be decoded.";
  if (code === 4) return "The media source or codec is unsupported.";
  return "An unknown media error occurred.";
}
