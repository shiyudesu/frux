// @vitest-environment jsdom
import { act, useEffect } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { emptyProfile } from "../constants";
import { RouterProvider } from "../router";
import { SessionProvider, useSession } from "../session";
import type { SessionUser } from "../types";
import { TopNav } from "./TopNav";

describe("top navigation search", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    localStorage.clear();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({
      code: "AUTH_INVALID_REFRESH_SESSION",
      error: "invalid refresh session"
    }), {
      status: 401,
      headers: { "Content-Type": "application/json" }
    })));
    window.history.replaceState({}, "", "/timeline");
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.restoreAllMocks();
  });

  it("submits typed search navigation and follows browser history", async () => {
    await render();
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

  it("keeps compact primary actions accessible for guests", async () => {
    await render();
    expect(required<HTMLButtonElement>("button.upload-button").textContent).toContain("投稿");
    expect(required<HTMLButtonElement>('button[aria-label="通知"]')).toBeTruthy();
    expect(required<HTMLButtonElement>('button[aria-label="登录"]')).toBeTruthy();
    expect(required<HTMLInputElement>('input[aria-label="搜索"]')).toBeTruthy();
  });

  it("dismisses the authenticated overflow menu and restores focus on Escape", async () => {
    await render(true);
    const trigger = required<HTMLButtonElement>('button[aria-label="更多账户操作"]');
    act(() => trigger.click());
    const logout = required<HTMLButtonElement>('[role="menuitem"]');
    expect(document.activeElement).toBe(logout);

    act(() => document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })));
    expect(container.querySelector('[role="menu"]')).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it("dismisses the authenticated overflow menu on outside pointer input", async () => {
    await render(true);
    const trigger = required<HTMLButtonElement>('button[aria-label="更多账户操作"]');
    act(() => trigger.click());
    expect(required<HTMLElement>('[role="menu"]')).toBeTruthy();

    act(() => document.body.dispatchEvent(new Event("pointerdown", { bubbles: true })));
    expect(container.querySelector('[role="menu"]')).toBeNull();
  });

  async function render(authenticated = false) {
    await act(async () => {
      root.render(
        <RouterProvider>
          <SessionProvider>
            {authenticated && <AuthenticatedSessionGate user={{ ...emptyProfile, id: 7, account: "owner", nickname: "Owner" }} />}
            <TopNav />
          </SessionProvider>
        </RouterProvider>
      );
      await flushPromises();
    });
  }

  function required<T extends Element>(selector: string): T {
    const element = container.querySelector<T>(selector);
    if (!element) throw new Error(`missing element: ${selector}`);
    return element;
  }
});

function AuthenticatedSessionGate({ user }: { user: SessionUser }) {
  const session = useSession();
  useEffect(() => {
    session.setAuth("token", user, 300);
  }, [session, user]);
  return null;
}

async function flushPromises() {
  await Promise.resolve();
  await Promise.resolve();
}

function changeInput(input: HTMLInputElement, value: string) {
  act(() => {
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
    setter?.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}
