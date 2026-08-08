// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TOKEN_KEY, USER_KEY, emptyProfile } from "../constants";
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

  it("keeps compact primary actions accessible for guests", () => {
    render();
    expect(required<HTMLButtonElement>("button.upload-button").textContent).toContain("投稿");
    expect(required<HTMLButtonElement>('button[aria-label="通知"]')).toBeTruthy();
    expect(required<HTMLButtonElement>('button[aria-label="登录"]')).toBeTruthy();
    expect(required<HTMLInputElement>('input[aria-label="搜索"]')).toBeTruthy();
  });

  it("dismisses the authenticated overflow menu and restores focus on Escape", () => {
    authenticate();
    render();
    const trigger = required<HTMLButtonElement>('button[aria-label="更多账户操作"]');
    act(() => trigger.click());
    const logout = required<HTMLButtonElement>('[role="menuitem"]');
    expect(document.activeElement).toBe(logout);

    act(() => document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })));
    expect(container.querySelector('[role="menu"]')).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it("dismisses the authenticated overflow menu on outside pointer input", () => {
    authenticate();
    render();
    const trigger = required<HTMLButtonElement>('button[aria-label="更多账户操作"]');
    act(() => trigger.click());
    expect(required<HTMLElement>('[role="menu"]')).toBeTruthy();

    act(() => document.body.dispatchEvent(new Event("pointerdown", { bubbles: true })));
    expect(container.querySelector('[role="menu"]')).toBeNull();
  });

  function authenticate() {
    localStorage.setItem(TOKEN_KEY, "token");
    localStorage.setItem(USER_KEY, JSON.stringify({ ...emptyProfile, id: 7, account: "owner", nickname: "Owner" }));
  }

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
