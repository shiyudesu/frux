import { describe, expect, it, vi } from "vitest";
import { canTransition, PlayerStateMachine, transitionPlayerState } from "./stateMachine";
import { createInitialPlayerState, type PlaybackSource } from "./types";

const source: PlaybackSource = {
  id: "baseline",
  type: "mp4",
  url: "https://media.example/video.mp4",
  mimeType: "video/mp4",
  codecs: ["avc1.42E01E", "mp4a.40.2"],
  qualityLabel: "720p",
  role: "baseline",
  revision: "ready:video.mp4",
  width: 1280,
  height: 720,
  bitrate: 2_000_000
};

describe("player state machine", () => {
  it("guards invalid lifecycle transitions", () => {
    expect(canTransition("idle", "playing")).toBe(false);
    expect(canTransition("idle", "loading")).toBe(true);
    expect(canTransition("error", "playing")).toBe(false);
    expect(canTransition("error", "loading")).toBe(true);

    const initial = createInitialPlayerState();
    const unchanged = transitionPlayerState(initial, { type: "playing" });
    expect(unchanged.status).toBe("idle");
  });

  it("tracks load, buffering, playback, seek, and end transitions", () => {
    const machine = new PlayerStateMachine();
    machine.dispatch({ type: "load", source, intendedPlay: true, selectedQuality: "auto" });
    machine.dispatch({ type: "ready", duration: 12 });
    machine.dispatch({ type: "playing" });
    machine.dispatch({ type: "buffering" });
    machine.dispatch({ type: "seeking", time: 7 });
    machine.dispatch({ type: "seeked", time: 8 });
    machine.dispatch({ type: "playing" });
    machine.dispatch({ type: "ended" });

    expect(machine.getState()).toMatchObject({
      status: "ended",
      currentTime: 12,
      duration: 12,
      intendedPlay: false,
      seeking: false
    });
  });

  it("normalizes invalid metrics and bounds volume and rate", () => {
    const state = transitionPlayerState(createInitialPlayerState(), {
      type: "metrics",
      currentTime: -2,
      duration: Number.NaN,
      bufferedAhead: Number.POSITIVE_INFINITY,
      volume: 5,
      playbackRate: 10
    });

    expect(state.currentTime).toBe(0);
    expect(state.duration).toBe(0);
    expect(state.bufferedAhead).toBe(0);
    expect(state.volume).toBe(1);
    expect(state.playbackRate).toBe(4);
  });

  it("publishes an initial snapshot and supports unsubscribe", () => {
    const machine = new PlayerStateMachine();
    const listener = vi.fn();
    const unsubscribe = machine.subscribe(listener);
    machine.dispatch({ type: "load", source, intendedPlay: false, selectedQuality: "auto" });
    unsubscribe();
    machine.dispatch({ type: "ready" });

    expect(listener).toHaveBeenCalledTimes(2);
    expect(listener.mock.calls[0]?.[0].status).toBe("idle");
    expect(listener.mock.calls[1]?.[0].status).toBe("loading");
  });
});
