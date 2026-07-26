import { describe, expect, it, vi } from "vitest";
import { NativeMP4Adapter } from "./nativeMP4Adapter";
import { FakePlayerMedia, FakeTimeRanges } from "./testMedia";
import type { PlaybackSource } from "./types";

const source: PlaybackSource = {
  id: "mp4-720",
  type: "mp4",
  url: "https://media.example/video-720.mp4",
  mimeType: "video/mp4",
  codecs: ["avc1.42E01E", "mp4a.40.2"],
  qualityLabel: "720p",
  role: "baseline",
  revision: "v1",
  width: 1280,
  height: 720,
  bitrate: 2_000_000
};

describe("NativeMP4Adapter", () => {
  it("loads MP4 and maps media lifecycle into normalized state", async () => {
    const media = new FakePlayerMedia();
    const adapter = new NativeMP4Adapter(media);
    await adapter.load(source, { muted: false, volume: 0.7, playbackRate: 1.25, startTime: 3 });
    media.duration = 20;
    media.buffered = new FakeTimeRanges([[0, 9]]);
    media.emit("loadedmetadata");
    media.emit("playing");

    expect(media).toMatchObject({
      src: source.url,
      muted: false,
      volume: 0.7,
      playbackRate: 1.25,
      currentTime: 3,
      playsInline: true,
      loop: true
    });
    expect(adapter.getState()).toMatchObject({
      status: "playing",
      duration: 20,
      bufferedAhead: 6,
      effectiveQualityId: source.id
    });
  });

  it("maps waiting, seeking, ending, and media errors", async () => {
    const media = new FakePlayerMedia();
    const adapter = new NativeMP4Adapter(media);
    await adapter.load(source);
    media.emit("canplay");
    media.emit("waiting");
    expect(adapter.getState().status).toBe("buffering");

    media.currentTime = 4;
    media.emit("seeking");
    media.currentTime = 5;
    media.emit("seeked");
    expect(adapter.getState()).toMatchObject({ currentTime: 5, seeking: false });

    media.error = { code: 2, message: "offline" };
    media.emit("error");
    expect(adapter.getState().error).toMatchObject({ category: "network", recoverable: true });
  });

  it("reports autoplay rejection as a recoverable structured error", async () => {
    const media = new FakePlayerMedia();
    const rejection = new DOMException("blocked", "NotAllowedError");
    media.playResult = Promise.reject(rejection);
    const adapter = new NativeMP4Adapter(media);

    await expect(adapter.load(source, { autoPlay: true })).rejects.toBe(rejection);
    expect(adapter.getState().error).toMatchObject({
      category: "autoplay",
      code: "autoplay_not_allowed",
      recoverable: true
    });
  });

  it("retries with preserved playback properties and fully cleans up", async () => {
    const media = new FakePlayerMedia();
    media.playResult = Promise.resolve();
    const adapter = new NativeMP4Adapter(media);
    const listener = vi.fn();
    adapter.subscribe(listener);
    await adapter.load(source, { autoPlay: true, muted: false, playbackRate: 1.5 });
    media.currentTime = 8;
    media.emit("timeupdate");

    await adapter.retry();
    expect(media.currentTime).toBe(8);
    expect(media.muted).toBe(false);
    expect(media.playbackRate).toBe(1.5);
    expect(media.loadCount).toBe(2);

    adapter.destroy();
    expect(media.listenerCount()).toBe(0);
    expect(media.src).toBe("");
    expect(media.pauseCount).toBeGreaterThan(0);
    expect(adapter.getState().status).toBe("idle");
    adapter.destroy();
  });
});
