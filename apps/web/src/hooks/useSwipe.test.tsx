// @vitest-environment jsdom
import { act, useRef } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useSwipe } from "./useSwipe";

describe("useSwipe pointer capture", () => {
  let container: HTMLDivElement;
  let root: Root;
  let captured: boolean;
  let setPointerCapture: ReturnType<typeof vi.fn>;
  let releasePointerCapture: ReturnType<typeof vi.fn>;

  beforeEach(async () => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    captured = false;
    setPointerCapture = vi.fn(() => {
      captured = true;
    });
    releasePointerCapture = vi.fn(() => {
      captured = false;
    });
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    await act(async () => root.render(<SwipeHarness />));
    const stage = requiredStage();
    stage.setPointerCapture = setPointerCapture;
    stage.releasePointerCapture = releasePointerCapture;
    stage.hasPointerCapture = () => captured;
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
  });

  it("does not capture a normal desktop click", () => {
    const layer = requiredLayer();
    act(() => {
      layer.dispatchEvent(pointerEvent("pointerdown", 100));
      layer.dispatchEvent(pointerEvent("pointerup", 100));
    });

    expect(setPointerCapture).not.toHaveBeenCalled();
    expect(releasePointerCapture).not.toHaveBeenCalled();
  });

  it("captures only after the pointer becomes a vertical swipe", () => {
    const layer = requiredLayer();
    act(() => {
      layer.dispatchEvent(pointerEvent("pointerdown", 100));
      layer.dispatchEvent(pointerEvent("pointermove", 60));
    });

    expect(setPointerCapture).toHaveBeenCalledWith(1);
    expect(captured).toBe(true);

    act(() => layer.dispatchEvent(pointerEvent("pointerup", 60)));
    expect(releasePointerCapture).toHaveBeenCalledWith(1);
    expect(captured).toBe(false);
  });

  function SwipeHarness() {
    const stageRef = useRef<HTMLElement | null>(null);
    const swipe = useSwipe({
      index: 0,
      itemsCount: 2,
      onIndexChange: () => {},
      stageRef
    });
    return (
      <section
        ref={stageRef}
        data-testid="stage"
        onPointerDown={swipe.handlePointerDown}
        onPointerMove={swipe.handlePointerMove}
        onPointerUp={swipe.handlePointerEnd}
      >
        <div data-testid="gesture-layer" />
      </section>
    );
  }

  function requiredStage(): HTMLElement {
    const stage = container.querySelector<HTMLElement>('[data-testid="stage"]');
    if (!stage) throw new Error("stage not found");
    return stage;
  }

  function requiredLayer(): HTMLElement {
    const layer = container.querySelector<HTMLElement>('[data-testid="gesture-layer"]');
    if (!layer) throw new Error("gesture layer not found");
    return layer;
  }
});

function pointerEvent(type: string, clientY: number): Event {
  const event = new MouseEvent(type, { bubbles: true, button: 0, clientY });
  Object.defineProperties(event, {
    pointerId: { value: 1 },
    buttons: { value: type === "pointerup" ? 0 : 1 }
  });
  return event;
}
