import { describe, expect, it, vi } from "vitest";
import { codecContentType, detectPlaybackCapabilities, isConstrainedNetwork } from "./capabilities";
import type { PlaybackSource } from "./types";

const mp4: PlaybackSource = {
  id: "mp4",
  type: "mp4",
  url: "https://media.example/video.mp4",
  mimeType: "video/mp4",
  codecs: ["avc1.42E01E", "mp4a.40.2"],
  qualityLabel: "720p",
  role: "baseline",
  revision: "v1",
  width: 1280,
  height: 720,
  bitrate: 2_000_000
};

const dash: PlaybackSource = {
  ...mp4,
  id: "dash",
  type: "dash",
  url: "https://media.example/manifest.mpd",
  mimeType: "application/dash+xml",
  qualityLabel: "Auto",
  role: "adaptive"
};

describe("detectPlaybackCapabilities", () => {
  it("detects codec, MediaSource, MediaCapabilities, network, save-data, and viewport signals", async () => {
    const decodingInfo = vi.fn(async () => ({
      supported: true,
      smooth: true,
      powerEfficient: false,
      keySystemAccess: null
    }));
    const capabilities = await detectPlaybackCapabilities([mp4, dash], {
      navigator: {
        onLine: true,
        connection: { effectiveType: "3g", downlink: 1.2, rtt: 550, saveData: true },
        mediaCapabilities: { decodingInfo }
      },
      mediaSource: { isTypeSupported: () => true },
      mediaElement: { canPlayType: () => "probably" },
      viewport: { width: 390, height: 844, devicePixelRatio: 3 }
    });

    expect(capabilities).toMatchObject({
      online: true,
      mediaSource: true,
      mediaCapabilities: true,
      saveData: true,
      effectiveType: "3g",
      downlinkMbps: 1.2,
      rttMs: 550,
      viewportWidth: 390,
      viewportHeight: 844,
      devicePixelRatio: 3
    });
    expect(capabilities.sources).toEqual([
      { sourceId: "mp4", playable: true, smooth: true, powerEfficient: false },
      { sourceId: "dash", playable: true, smooth: true, powerEfficient: false }
    ]);
    expect(decodingInfo).toHaveBeenCalledTimes(2);
    expect(isConstrainedNetwork(capabilities)).toBe(true);
  });

  it("rejects DASH without MediaSource and skips unsupported codecs", async () => {
    const capabilities = await detectPlaybackCapabilities([mp4, dash], {
      navigator: { onLine: true },
      mediaElement: { canPlayType: () => "" },
      viewport: { width: 1280, height: 720, devicePixelRatio: 1 }
    });

    expect(capabilities.sources).toEqual([
      expect.objectContaining({ sourceId: "mp4", playable: false, reason: "codec" }),
      expect.objectContaining({ sourceId: "dash", playable: false, reason: "media_source" })
    ]);
  });

  it("uses an MP4 codec content type for DASH capability probing", () => {
    expect(codecContentType(dash)).toBe('video/mp4; codecs="avc1.42E01E,mp4a.40.2"');
  });
});
