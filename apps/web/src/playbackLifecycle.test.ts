import { describe, expect, it } from "vitest";
import { PlaybackLifecycle } from "./playbackLifecycle";
import type { FeedVideo } from "./types";

const item: FeedVideo = {
  video_id: 42,
  author_id: 7,
  title: "test",
  media_url: "/uploads/video/test.mp4",
  cover_url: "/uploads/cover/test.jpg",
  like_count: 0,
  comment_count: 0,
  favorite_count: 0,
  liked: false,
  favorited: false,
  author: "creator",
  avatar_url: "",
  description: "",
  feed_scene: "recommend",
  request_id: "req-1"
};

function createLifecycle() {
  let id = 0;
  let occurredAt = 0;
  return new PlaybackLifecycle(item, {
    createID: (prefix) => `${prefix}-${++id}`,
    occurredAt: () => `2026-07-25T10:00:${String(occurredAt++).padStart(2, "0")}Z`
  });
}

describe("PlaybackLifecycle", () => {
  it("emits exposure and play once per activation", () => {
    const lifecycle = createLifecycle();

    expect(lifecycle.activate(1_000).map((event) => event.event_type)).toEqual(["exposed"]);
    expect(lifecycle.activate(1_001)).toEqual([]);
    expect(lifecycle.playing(1_100, 0, 100).map((event) => event.event_type)).toEqual(["play"]);
    expect(lifecycle.playing(1_200, 0.1, 100)).toEqual([]);
  });

  it("reports progress only after the bounded interval", () => {
    const lifecycle = createLifecycle();
    lifecycle.activate(1_000);
    lifecycle.playing(1_000, 0, 100);

    expect(lifecycle.timeUpdate(6_000, 5, 100)).toEqual([]);
    const [progress] = lifecycle.timeUpdate(11_050, 10.2, 100);

    expect(progress.event_type).toBe("progress");
    expect(progress.watch_ms).toBe(10_050);
    expect(progress.position_ms).toBe(10_200);
    expect(progress.duration_ms).toBe(100_000);
  });

  it("flushes partial progress when playback pauses", () => {
    const lifecycle = createLifecycle();
    lifecycle.activate(1_000);
    lifecycle.playing(1_000, 0, 100);

    const [progress] = lifecycle.pause(3_500, 2, 100);

    expect(progress.event_type).toBe("progress");
    expect(progress.watch_ms).toBe(2_500);
  });

  it("does not count time while the page is hidden", () => {
    const lifecycle = createLifecycle();
    lifecycle.activate(1_000);
    lifecycle.playing(1_000, 0, 100);

    const [hiddenProgress] = lifecycle.setVisibility(6_000, false, 5, 100);
    expect(hiddenProgress.watch_ms).toBe(5_000);

    lifecycle.setVisibility(16_000, true, 5, 100);
    expect(lifecycle.timeUpdate(21_000, 10, 100)).toEqual([]);
    const [progress] = lifecycle.timeUpdate(26_000, 15, 100);
    expect(progress.watch_ms).toBe(15_000);
  });

  it("emits completion once and never follows it with skip", () => {
    const lifecycle = createLifecycle();
    lifecycle.activate(1_000);
    lifecycle.playing(1_000, 0, 100);

    expect(lifecycle.timeUpdate(9_000, 96, 100)[0].event_type).toBe("progress");
    const [complete] = lifecycle.timeUpdate(10_000, 98, 100);

    expect(complete.event_type).toBe("complete");
    expect(complete.completed).toBe(true);
    expect(lifecycle.timeUpdate(10_500, 99, 100)).toEqual([]);
    expect(lifecycle.finish(11_000, 100, 100)).toEqual([]);
  });

  it("requires both completion boundaries for short videos", () => {
    const lifecycle = createLifecycle();
    lifecycle.activate(1_000);
    lifecycle.playing(1_000, 0, 20);

    expect(lifecycle.timeUpdate(2_000, 18, 20)[0].event_type).toBe("progress");
    expect(lifecycle.timeUpdate(3_000, 19, 20)[0].event_type).toBe("complete");
  });

  it("resumes effective watch accounting after a seek", () => {
    const lifecycle = createLifecycle();
    lifecycle.activate(1_000);
    lifecycle.playing(1_000, 0, 100);

    lifecycle.seeking(5_000, 4, 100);
    lifecycle.waiting(5_500, 4, 100);
    expect(lifecycle.seeked(6_000, 50, 100, true)[0].watch_ms).toBe(4_000);
    const [progress] = lifecycle.timeUpdate(16_000, 60, 100);

    expect(progress.event_type).toBe("progress");
    expect(progress.watch_ms).toBe(14_000);
  });

  it("flushes progress without terminalizing a BFCache session", () => {
    const lifecycle = createLifecycle();
    lifecycle.activate(1_000);
    lifecycle.playing(1_000, 0, 100);

    const [progress] = lifecycle.flush(6_000, 5, 100);
    expect(progress.event_type).toBe("progress");

    lifecycle.setVisibility(6_000, false, 5, 100);
    lifecycle.setVisibility(16_000, true, 5, 100);
    expect(lifecycle.playing(16_000, 5, 100)).toEqual([]);
    expect(lifecycle.finish(18_000, 7, 100)[0].event_type).toBe("skip");
  });

  it("emits one terminal skip with stable sequencing", () => {
    const lifecycle = createLifecycle();
    const [exposed] = lifecycle.activate(1_000);
    const [play] = lifecycle.playing(1_100, 0, 100);
    const [skip] = lifecycle.finish(4_100, 3, 100);

    expect([exposed.sequence, play.sequence, skip.sequence]).toEqual([1, 2, 3]);
    expect(skip.event_type).toBe("skip");
    expect(skip.watch_ms).toBe(3_000);
    expect(lifecycle.finish(5_000, 4, 100)).toEqual([]);
  });

  it("does not create a phantom terminal event before exposure", () => {
    const lifecycle = createLifecycle();

    expect(lifecycle.finish(1_000, 0, 100)).toEqual([]);
  });
});
