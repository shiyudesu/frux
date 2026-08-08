// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createInitialPlayerState } from "../player";
import { FeedPlayerControls } from "./FeedPlayerControls";

describe("FeedPlayerControls menus", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
  });

  it("selects quality from the custom menu and returns focus to its trigger", async () => {
    const onSelectQuality = vi.fn();
    await act(async () => root.render(
      <FeedPlayerControls
        fullscreen={false}
        continuousPlay={false}
        state={{
          ...createInitialPlayerState(),
          selectedQuality: "auto",
          qualities: [{ id: "720p", label: "720p", selected: false, active: true }]
        }}
        onTogglePlayback={() => {}}
        onToggleMute={() => {}}
        onSeek={() => {}}
        onSelectQuality={onSelectQuality}
        onSelectRate={() => {}}
        onToggleContinuousPlay={() => {}}
        onRetry={() => {}}
        onToggleFullscreen={() => {}}
      />
    ));
    const trigger = requiredButton("清晰度，当前 自动");
    act(() => trigger.click());
    expect(container.querySelectorAll(".player-choice-menu button.active svg path")).toHaveLength(1);
    const option = requiredButton("720p");
    act(() => option.click());

    expect(onSelectQuality).toHaveBeenCalledWith("720p");
    expect(container.querySelector('[role="menu"][aria-label="清晰度"]')).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it("closes an open menu with Escape", async () => {
    await act(async () => root.render(
      <FeedPlayerControls
        fullscreen={false}
        continuousPlay={false}
        state={createInitialPlayerState()}
        onTogglePlayback={() => {}}
        onToggleMute={() => {}}
        onSeek={() => {}}
        onSelectQuality={() => {}}
        onSelectRate={() => {}}
        onToggleContinuousPlay={() => {}}
        onRetry={() => {}}
        onToggleFullscreen={() => {}}
      />
    ));
    const trigger = requiredButton("播放速度，当前 1x");
    act(() => trigger.click());
    expect(container.querySelector('[role="menu"][aria-label="播放速度"]')).not.toBeNull();

    act(() => document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })));

    expect(container.querySelector('[role="menu"][aria-label="播放速度"]')).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it("supports arrow and typeahead navigation inside a menu", async () => {
    await act(async () => root.render(
      <FeedPlayerControls
        fullscreen={false}
        continuousPlay={false}
        state={createInitialPlayerState()}
        onTogglePlayback={() => {}}
        onToggleMute={() => {}}
        onSeek={() => {}}
        onSelectQuality={() => {}}
        onSelectRate={() => {}}
        onToggleContinuousPlay={() => {}}
        onRetry={() => {}}
        onToggleFullscreen={() => {}}
      />
    ));
    const trigger = requiredButton("播放速度，当前 1x");
    act(() => {
      trigger.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
    });
    expect(document.activeElement?.textContent?.trim()).toBe("1x");

    act(() => {
      document.activeElement?.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
    });
    expect(document.activeElement?.textContent?.trim()).toBe("1.25x");

    act(() => {
      document.activeElement?.dispatchEvent(new KeyboardEvent("keydown", { key: "2", bubbles: true }));
    });
    expect(document.activeElement?.textContent?.trim()).toBe("2x");
  });

  function requiredButton(name: string): HTMLButtonElement {
    const button = [...container.querySelectorAll<HTMLButtonElement>("button")]
      .find((candidate) => candidate.getAttribute("aria-label") === name || candidate.textContent?.trim() === name);
    if (!button) throw new Error(`button not found: ${name}`);
    return button;
  }
});
