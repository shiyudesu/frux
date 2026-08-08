// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { STAGE_SINGLE_CLICK_DELAY_MS, StageGestureLayer } from "./StageGestureLayer";

describe("StageGestureLayer", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.useFakeTimers();
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.useRealTimers();
  });

  it("toggles playback after a confirmed single click", async () => {
    const onTogglePlayback = vi.fn();
    await renderGesture({ onTogglePlayback });
    const video = mountedVideo();

    act(() => {
      video.dispatchEvent(new MouseEvent("click", { bubbles: true, detail: 1 }));
      vi.advanceTimersByTime(STAGE_SINGLE_CLICK_DELAY_MS - 1);
    });
    expect(onTogglePlayback).not.toHaveBeenCalled();

    act(() => vi.advanceTimersByTime(1));
    expect(onTogglePlayback).toHaveBeenCalledOnce();
  });

  it("turns a double click into one-way like without toggling playback", async () => {
    const onLike = vi.fn();
    const onTogglePlayback = vi.fn();
    await renderGesture({ onLike, onTogglePlayback });
    const video = mountedVideo();

    act(() => {
      video.dispatchEvent(new MouseEvent("click", { bubbles: true, detail: 1 }));
      video.dispatchEvent(new MouseEvent("click", { bubbles: true, detail: 2 }));
      video.dispatchEvent(new MouseEvent("dblclick", {
        bubbles: true,
        cancelable: true,
        clientX: 80,
        clientY: 60,
        detail: 2
      }));
      vi.advanceTimersByTime(STAGE_SINGLE_CLICK_DELAY_MS);
    });

    expect(onLike).toHaveBeenCalledOnce();
    expect(onTogglePlayback).not.toHaveBeenCalled();
    expect(container.querySelector(".stage-like-burst")).not.toBeNull();
  });

  it("keeps an existing like active on double click", async () => {
    const onLike = vi.fn();
    await renderGesture({ liked: true, onLike });

    act(() => {
      requiredLayer().dispatchEvent(new MouseEvent("dblclick", {
        bubbles: true,
        cancelable: true,
        detail: 2
      }));
    });

    expect(onLike).not.toHaveBeenCalled();
    expect(container.querySelector(".stage-like-burst")).not.toBeNull();
  });

  it("does not show false like feedback when liking is unavailable", async () => {
    const onLike = vi.fn();
    await renderGesture({ canLike: false, onLike });

    act(() => {
      requiredLayer().dispatchEvent(new MouseEvent("dblclick", {
        bubbles: true,
        cancelable: true,
        detail: 2
      }));
    });

    expect(onLike).not.toHaveBeenCalled();
    expect(container.querySelector(".stage-like-burst")).toBeNull();
  });

  async function renderGesture(overrides: Partial<React.ComponentProps<typeof StageGestureLayer>>) {
    await act(async () => root.render(
      <StageGestureLayer
        active
        canLike
        liked={false}
        videoID={1}
        onLike={() => {}}
        onTogglePlayback={() => {}}
        {...overrides}
      />
    ));
  }

  function requiredLayer(): HTMLDivElement {
    const layer = container.querySelector<HTMLDivElement>('[data-ui="stage-gesture-layer"]');
    if (!layer) throw new Error("gesture layer not found");
    return layer;
  }

  function mountedVideo(): HTMLVideoElement {
    const video = document.createElement("video");
    requiredLayer().appendChild(video);
    return video;
  }
});
