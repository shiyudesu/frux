// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RouterProvider } from "../router";
import { SessionProvider } from "../session";
import { TopNav } from "./TopNav";

describe("top navigation search", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    localStorage.clear();
    window.history.replaceState({}, "", "/timeline");
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  it("submits typed search navigation and follows browser history", () => {
    render();
    const input = required<HTMLInputElement>('input[aria-label="搜索"]');
    changeInput(input, "猫 视频");
    act(() => {
      required<HTMLFormElement>('form[role="search"]').dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });
    expect(window.location.pathname).toBe("/search");
    expect(new URLSearchParams(window.location.search).get("q")).toBe("猫 视频");

    act(() => {
      window.history.pushState({}, "", "/search?q=%E7%94%A8%E6%88%B7&tab=users");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(input.value).toBe("用户");
  });

  function render() {
    act(() => root.render(
      <RouterProvider>
        <SessionProvider>
          <TopNav />
        </SessionProvider>
      </RouterProvider>
    ));
  }

  function required<T extends Element>(selector: string): T {
    const element = container.querySelector<T>(selector);
    if (!element) throw new Error(`missing element: ${selector}`);
    return element;
  }
});

function changeInput(input: HTMLInputElement, value: string) {
  act(() => {
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
    setter?.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}
