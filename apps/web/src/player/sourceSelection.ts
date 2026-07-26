import { isConstrainedNetwork } from "./capabilities";
import type {
  AdaptiveQualityBounds,
  LegacyPlaybackItem,
  LegacyPlaybackSource,
  PlaybackClientCapabilities,
  PlaybackQuality,
  PlaybackSelectionPolicy,
  PlaybackSource,
  PlaybackSourcePlan,
  QualitySelection
} from "./types";

export function normalizePlaybackSources(item: LegacyPlaybackItem): readonly PlaybackSource[] {
  const normalized: PlaybackSource[] = [];
  for (const [index, source] of (item.playback_sources ?? []).entries()) {
    const candidate = normalizeAdditiveSource(source, index, item.media_status);
    if (candidate && !hasSource(normalized, candidate)) normalized.push(candidate);
  }

  const legacyURL = item.media_url.trim();
  if (legacyURL && !normalized.some((source) => source.type === "mp4" && source.url === legacyURL)) {
    normalized.push({
      id: uniqueId(normalized, "legacy-mp4"),
      type: "mp4",
      url: legacyURL,
      mimeType: "video/mp4",
      codecs: ["avc1.42E01E", "mp4a.40.2"],
      qualityLabel: "Compatible",
      role: "baseline",
      revision: `${item.media_status?.trim() || "legacy"}:${legacyURL}`
    });
  }
  return normalized;
}

export function selectPlaybackSourcePlan(
  sources: readonly PlaybackSource[],
  capabilities: PlaybackClientCapabilities,
  policy: PlaybackSelectionPolicy = {},
  qualityPreference: QualitySelection = "auto"
): PlaybackSourcePlan | null {
  const support = new Map(capabilities.sources.map((capability) => [capability.sourceId, capability]));
  const playable = sources.filter((source) => support.get(source.id)?.playable === true);
  if (!playable.length) return null;

  const mp4 = playable.filter((source) => source.type === "mp4").sort(compareQuality);
  const dash = playable.find((source) => source.type === "dash");
  const policyCompatibleMP4 = filterByPolicy(mp4, policy);
  const compatibleMP4 = filterByViewport(policyCompatibleMP4, capabilities, policy);
  const manualMP4 =
    qualityPreference === "auto"
      ? undefined
      : policyCompatibleMP4.find(
          (source) =>
            source.id === qualityPreference ||
            source.qualityLabel.toLowerCase() === qualityPreference.trim().toLowerCase()
        );
  const dashAllowed = policy.allowDash !== false && dash !== undefined && capabilities.mediaSource;
  const primary = manualMP4 ?? (dashAllowed ? dash : selectAutomaticMP4(compatibleMP4, capabilities, policy));
  if (!primary) return null;

  const adaptiveBounds = deriveAdaptiveQualityBounds(policyCompatibleMP4, capabilities, policy);
  const fallbacks = playable
    .filter((source) => source.id !== primary.id && source.type === "mp4")
    .sort((left, right) => fallbackScore(right, adaptiveBounds.maxBitrate) - fallbackScore(left, adaptiveBounds.maxBitrate));
  const selectedQuality = manualMP4
    ? manualMP4.id
    : primary.type === "dash"
      ? qualityPreference
      : "auto";
  return {
    primary,
    fallbacks,
    qualities: policyCompatibleMP4.map((source) => quality(source, selectedQuality, primary.id)),
    selectedQuality,
    adaptiveBounds
  };
}

export function deriveAdaptiveQualityBounds(
  sources: readonly PlaybackSource[],
  capabilities: PlaybackClientCapabilities,
  policy: PlaybackSelectionPolicy = {}
): AdaptiveQualityBounds {
  const bitrates = sources
    .map((source) => source.bitrate)
    .filter((bitrate): bitrate is number => bitrate !== undefined && Number.isFinite(bitrate) && bitrate > 0)
    .sort((left, right) => left - right);
  const networkMax = networkBitrateLimit(capabilities);
  const viewportMax = viewportBitrateLimit(sources, capabilities, policy);
  const maxCandidates = [policy.maxBitrate, networkMax, viewportMax].filter(
    (value): value is number => value !== undefined && Number.isFinite(value) && value > 0
  );
  const maxBitrate = maxCandidates.length ? Math.min(...maxCandidates) : bitrates.at(-1);
  const minBitrate = policy.minBitrate ?? bitrates[0];
  const preferredInitial = policy.preferredInitialBitrate ?? maxBitrate;
  const initialBitrate = nearestAtOrBelow(bitrates, preferredInitial, minBitrate, maxBitrate);
  return compactBounds({ minBitrate, maxBitrate, initialBitrate });
}

function normalizeAdditiveSource(
  source: LegacyPlaybackSource,
  index: number,
  mediaStatus: string | undefined
): PlaybackSource | null {
  if (source.type === "image") return null;
  const url = source.url.trim();
  if (!url) return null;
  const qualityLabel = source.quality?.trim() || inferredQuality(source);
  const role = source.role?.trim() || (source.type === "dash" ? "adaptive" : "variant");
  return {
    id: `${source.type}-${slug(qualityLabel || role)}-${index}`,
    type: source.type,
    url,
    mimeType: source.type === "dash" ? "application/dash+xml" : "video/mp4",
    codecs: normalizeCodecs(source.codec, source.audio_codec),
    qualityLabel,
    role,
    revision: `${mediaStatus?.trim() || "ready"}:${source.type}:${url}`,
    width: positiveInteger(source.width),
    height: positiveInteger(source.height),
    bitrate: positiveInteger(source.bitrate)
  };
}

function normalizeCodecs(videoCodec: string | undefined, audioCodec: string | undefined): readonly string[] {
  const values = [videoCodec, audioCodec]
    .flatMap((codec) => codec?.split(",") ?? [])
    .map((codec) => codec.trim())
    .filter(Boolean)
    .map(normalizeCodecIdentifier);
  return [...new Set(values)];
}

function normalizeCodecIdentifier(codec: string): string {
  const normalized = codec.toLowerCase();
  if (normalized === "h264" || normalized === "avc") return "avc1.42E01E";
  if (normalized === "h265" || normalized === "hevc") return "hvc1.1.6.L93.B0";
  if (normalized === "vp9") return "vp09.00.10.08";
  if (normalized === "av1") return "av01.0.04M.08";
  if (normalized === "aac") return "mp4a.40.2";
  return codec;
}

function filterByPolicy(
  sources: readonly PlaybackSource[],
  policy: PlaybackSelectionPolicy
): readonly PlaybackSource[] {
  const filtered = sources.filter((source) => {
    if (policy.minBitrate !== undefined && source.bitrate !== undefined && source.bitrate < policy.minBitrate) {
      return false;
    }
    if (policy.maxBitrate !== undefined && source.bitrate !== undefined && source.bitrate > policy.maxBitrate) {
      return false;
    }
    return true;
  });
  return filtered.length ? filtered : sources;
}

function filterByViewport(
  sources: readonly PlaybackSource[],
  capabilities: PlaybackClientCapabilities,
  policy: PlaybackSelectionPolicy
): readonly PlaybackSource[] {
  const targetHeight = Math.max(
    capabilities.viewportHeight,
    capabilities.viewportWidth * 9 / 16
  ) * capabilities.devicePixelRatio;
  const heightLimit = Math.min(policy.preferredMaxHeight ?? Number.POSITIVE_INFINITY, targetHeight * 1.25);
  const withinViewport = sources.filter((source) => source.height === undefined || source.height <= heightLimit);
  return withinViewport.length ? withinViewport : sources.slice(0, 1);
}

function selectAutomaticMP4(
  sources: readonly PlaybackSource[],
  capabilities: PlaybackClientCapabilities,
  policy: PlaybackSelectionPolicy
): PlaybackSource | undefined {
  if (!sources.length) return undefined;
  const bounds = deriveAdaptiveQualityBounds(sources, capabilities, policy);
  const underLimit = sources.filter(
    (source) => source.bitrate === undefined || bounds.maxBitrate === undefined || source.bitrate <= bounds.maxBitrate
  );
  return underLimit.at(-1) ?? sources[0];
}

function fallbackScore(
  source: PlaybackSource,
  maxBitrate: number | undefined
): number {
  if (source.bitrate === undefined) return 1;
  return maxBitrate === undefined || source.bitrate <= maxBitrate
    ? source.bitrate + 1_000_000_000
    : -source.bitrate;
}

function networkBitrateLimit(capabilities: PlaybackClientCapabilities): number | undefined {
  if (!capabilities.online || capabilities.saveData) return 500_000;
  if (/^(slow-2g|2g)$/.test(capabilities.effectiveType)) return 350_000;
  if (capabilities.effectiveType === "3g") return 900_000;
  if (capabilities.downlinkMbps !== undefined) return Math.max(350_000, capabilities.downlinkMbps * 700_000);
  return isConstrainedNetwork(capabilities) ? 900_000 : undefined;
}

function viewportBitrateLimit(
  sources: readonly PlaybackSource[],
  capabilities: PlaybackClientCapabilities,
  policy: PlaybackSelectionPolicy
): number | undefined {
  const targetHeight =
    Math.max(capabilities.viewportHeight, capabilities.viewportWidth * 9 / 16) *
    capabilities.devicePixelRatio;
  const heightLimit = Math.min(policy.preferredMaxHeight ?? Number.POSITIVE_INFINITY, targetHeight * 1.25);
  return sources
    .filter((source) => source.height !== undefined && source.height <= heightLimit)
    .map((source) => source.bitrate)
    .filter((bitrate): bitrate is number => bitrate !== undefined)
    .sort((left, right) => left - right)
    .at(-1);
}

function nearestAtOrBelow(
  bitrates: readonly number[],
  preferred: number | undefined,
  min: number | undefined,
  max: number | undefined
): number | undefined {
  const candidates = bitrates.filter(
    (bitrate) =>
      (min === undefined || bitrate >= min) &&
      (max === undefined || bitrate <= max) &&
      (preferred === undefined || bitrate <= preferred)
  );
  return candidates.at(-1) ?? bitrates.find((bitrate) => max === undefined || bitrate <= max) ?? bitrates[0];
}

function compactBounds(bounds: AdaptiveQualityBounds): AdaptiveQualityBounds {
  const result: AdaptiveQualityBounds = {};
  if (bounds.minBitrate !== undefined) result.minBitrate = Math.round(bounds.minBitrate);
  if (bounds.maxBitrate !== undefined) result.maxBitrate = Math.max(result.minBitrate ?? 0, Math.round(bounds.maxBitrate));
  if (bounds.initialBitrate !== undefined) {
    result.initialBitrate = Math.min(
      result.maxBitrate ?? Number.POSITIVE_INFINITY,
      Math.max(result.minBitrate ?? 0, Math.round(bounds.initialBitrate))
    );
  }
  return result;
}

function quality(source: PlaybackSource, selected: QualitySelection, activeId: string): PlaybackQuality {
  return {
    id: source.id,
    label: source.qualityLabel,
    width: source.width,
    height: source.height,
    bitrate: source.bitrate,
    selected: selected === source.id,
    active: activeId === source.id
  };
}

function compareQuality(left: PlaybackSource, right: PlaybackSource): number {
  return (left.bitrate ?? left.height ?? 0) - (right.bitrate ?? right.height ?? 0);
}

function inferredQuality(source: LegacyPlaybackSource): string {
  if (source.height && source.height > 0) return `${Math.round(source.height)}p`;
  if (source.type === "dash") return "Auto";
  return "Compatible";
}

function hasSource(sources: readonly PlaybackSource[], candidate: PlaybackSource): boolean {
  return sources.some((source) => source.type === candidate.type && source.url === candidate.url);
}

function uniqueId(sources: readonly PlaybackSource[], base: string): string {
  if (!sources.some((source) => source.id === base)) return base;
  let suffix = 2;
  while (sources.some((source) => source.id === `${base}-${suffix}`)) suffix += 1;
  return `${base}-${suffix}`;
}

function slug(value: string): string {
  const normalized = value.trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
  return normalized || "source";
}

function positiveInteger(value: number | undefined): number | undefined {
  return value !== undefined && Number.isFinite(value) && value > 0 ? Math.round(value) : undefined;
}
