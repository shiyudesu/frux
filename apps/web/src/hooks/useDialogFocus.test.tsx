// @vitest-environment jsdom
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useDialogFocus } from "./useDialogFocus";

describe("dialog focus stack", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      callback(0);
      return 1;
    });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => {});
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  it("dismisses only the top share dialog before the collection playback", async () => {
    await act(async () => {
      root.render(<NestedDialogHarness />);
      await Promise.resolve();
    });
    click(required<HTMLButtonElement>("[data-testid='open-share']"));

    act(() => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    });
    expect(container.querySelector("[data-testid='share-dialog']")).toBeNull();
    expect(container.querySelector("[data-testid='collection-playback']")).not.toBeNull();

    act(() => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    });
    expect(container.querySelector("[data-testid='collection-playback']")).toBeNull();
  });

  function required<T extends Element>(selector: string): T {
    const element = container.querySelector<T>(selector);
    if (!element) throw new Error(`missing element: ${selector}`);
    return element;
  }
});

function NestedDialogHarness() {
  const [collectionOpen, setCollectionOpen] = useState(true);
  const [shareOpen, setShareOpen] = useState(false);
  const collectionCloseRef = useDialogFocus<HTMLButtonElement>(
    collectionOpen,
    () => setCollectionOpen(false)
  );
  const shareCloseRef = useDialogFocus<HTMLButtonElement>(
    shareOpen,
    () => setShareOpen(false)
  );
  if (!collectionOpen) return null;
  return (
    <section
      className="collection-queue-dialog"
      data-testid="collection-playback"
      role="dialog"
    >
      <button ref={collectionCloseRef} type="button">close collection</button>
      <button type="button" data-testid="open-share" onClick={() => setShareOpen(true)}>
        share
      </button>
      {shareOpen && (
        <section className="chat-share-dialog" data-testid="share-dialog" role="dialog">
          <button ref={shareCloseRef} type="button">close share</button>
        </section>
      )}
    </section>
  );
}

function click(element: HTMLElement) {
  act(() => element.dispatchEvent(new MouseEvent("click", { bubbles: true })));
}
