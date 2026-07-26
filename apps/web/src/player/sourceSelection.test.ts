import { describe, expect, it } from "vitest";
import {
  deriveAdaptiveQualityBounds,
  normalizePlaybackSources,
  selectPlaybackSourcePlan
} from "./sourceSelection";
import type {
  PlaybackClientCapabilities,
  PlaybackSource,
  PlaybackSourceCapability
} from "./types";

function client(
  sources: readonly PlaybackSource[],
  overrides: Partial<PlaybackClientCapabilities> = {},
  supportOverrides: Readonly<Record<string, Partial<PlaybackSourceCapability>>> = {}
): PlaybackClientCapabilities {
  return {
    online: true,
    mediaSource: true,
    mediaCapabilities: true,
    saveData: false,
    effectiveType: "4g",
    downlinkMbps: 10,
    rttMs: 40,
    viewportWidth: 1920,
    viewportHeight: 1080,
    devicePixelRatio: 1,
    sources: sources.map((source) => ({
      sourceId: source.id,
      playable: true,
      smooth: true,
      powerEfficient: true,
      ...supportOverrides[source.id]
    })),
    ...overrides
  };
}

const normalized = normalizePlaybackSources({
  media_url: "https://media.example/baseline.mp4",
  media_status: "ready",
  playback_sources: [
    {
      type: "mp4",
      url: "https://media.example/360.mp4",
      codec: "h264",
      audio_codec: "aac",
      width: 640,
      height: 360,
      bitrate: 400_000,
      quality: "360p",
      role: "variant"
    },
    {
      type: "mp4",
      url: "https://media.example/720.mp4",
      codec: "avc1.4D401F",
      width: 1280,
      height: 720,
      bitrate: 1_500_000,
      quality: "720p"
    },
    {
      type: "dash",
      url: "https://media.example/manifest.mpd",
      codec: "avc1.4D401F",
      quality: "Auto",
      role: "adaptive"
    }
  ]
});

describe("normalizePlaybackSources", () => {
  it("synthesizes exactly one compatible MP4 source for a legacy item", () => {
    expect(normalizePlaybackSources({ media_url: " /uploads/video.mp4 " })).toEqual([
      expect.objectContaining({
        id: "legacy-mp4",
        type: "mp4",
        url: "/uploads/video.mp4",
        role: "baseline",
        codecs: ["avc1.42E01E", "mp4a.40.2"]
      })
    ]);
  });

  it("normalizes additive sources and retains media_url as the compatibility fallback", () => {
    expect(normalized).toHaveLength(4);
    expect(normalized[0]).toMatchObject({
      type: "mp4",
      qualityLabel: "360p",
      codecs: ["avc1.42E01E", "mp4a.40.2"],
      bitrate: 400_000
    });
    expect(normalized.at(-1)).toMatchObject({
      id: "legacy-mp4",
      url: "https://media.example/baseline.mp4",
      role: "baseline"
    });
  });
});

describe("selectPlaybackSourcePlan", () => {
  it("uses supported DASH with conservative adaptive bounds on constrained networks", () => {
    const capabilities = client(normalized, {
      saveData: true,
      effectiveType: "3g",
      downlinkMbps: 0.8,
      viewportWidth: 390,
      viewportHeight: 640
    });
    const plan = selectPlaybackSourcePlan(normalized, capabilities);

    expect(plan?.primary.type).toBe("dash");
    expect(plan?.fallbacks[0]).toMatchObject({ type: "mp4", qualityLabel: "360p" });
    expect(plan?.adaptiveBounds).toEqual({
      minBitrate: 400_000,
      maxBitrate: 500_000,
      initialBitrate: 400_000
    });
  });

  it("honors a compatible manual MP4 quality and server bounds", () => {
    const high = normalized.find((source) => source.qualityLabel === "720p");
    if (!high) throw new Error("Expected high quality source.");
    const plan = selectPlaybackSourcePlan(
      normalized,
      client(normalized),
      { minBitrate: 300_000, maxBitrate: 2_000_000, preferredInitialBitrate: 600_000 },
      high.id
    );

    expect(plan?.primary.id).toBe(high.id);
    expect(plan?.selectedQuality).toBe(high.id);
    expect(plan?.qualities.find((quality) => quality.id === high.id)).toMatchObject({
      selected: true,
      active: true
    });
  });

  it("honors an explicit playable manual quality above the automatic viewport target", () => {
    const high = normalized.find((source) => source.qualityLabel === "720p");
    if (!high) throw new Error("Expected high quality source.");

    const plan = selectPlaybackSourcePlan(
      normalized,
      client(normalized, { viewportWidth: 320, viewportHeight: 568, devicePixelRatio: 1 }),
      { allowDash: false },
      high.id
    );

    expect(plan?.primary.id).toBe(high.id);
    expect(plan?.selectedQuality).toBe(high.id);
    expect(plan?.adaptiveBounds.initialBitrate).toBe(400_000);
  });

  it("skips unsupported codecs instead of attempting them", () => {
    const dash = normalized.find((source) => source.type === "dash");
    const high = normalized.find((source) => source.qualityLabel === "720p");
    if (!dash || !high) throw new Error("Expected DASH and high quality sources.");
    const plan = selectPlaybackSourcePlan(
      normalized,
      client(normalized, {}, {
        [dash.id]: { playable: false, reason: "media_source" },
        [high.id]: { playable: false, reason: "codec" }
      })
    );

    expect(plan?.primary).toMatchObject({ type: "mp4", qualityLabel: "360p" });
    expect(plan?.fallbacks.some((source) => source.id === high.id)).toBe(false);
  });

  it("normalizes an unavailable manual preference to auto on MP4 fallback", () => {
    const plan = selectPlaybackSourcePlan(
      normalized,
      client(normalized, {}, {
        [normalized.find((source) => source.type === "dash")?.id || "dash"]: { playable: false }
      }),
      { allowDash: false },
      "stale-representation"
    );

    expect(plan?.primary.type).toBe("mp4");
    expect(plan?.selectedQuality).toBe("auto");
  });

  it("derives viewport, network, and policy bounded adaptive values", () => {
    const bounds = deriveAdaptiveQualityBounds(
      normalized.filter((source) => source.type === "mp4" && source.bitrate !== undefined),
      client(normalized, { downlinkMbps: 2 }),
      { minBitrate: 300_000, maxBitrate: 1_200_000, preferredInitialBitrate: 900_000 }
    );
    expect(bounds).toEqual({
      minBitrate: 300_000,
      maxBitrate: 1_200_000,
      initialBitrate: 400_000
    });
  });
});
