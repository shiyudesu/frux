// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  FeedRefreshProvider,
  useFeedRefreshRequest
} from "../feedRefresh";
import { RouterProvider } from "../router";
import { SideNav } from "./AppNavigation";

const sessionMock = vi.hoisted(() => ({
  token: "token",
  user: { id: 7 }
}));

vi.mock("../session", () => ({
  useSession: () => sessionMock,
  useUnreadCount: () => ({ unreadCount: 0, refreshUnreadCount: vi.fn() })
}));

describe("Feed navigation refresh controls", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
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

  it("shows one refresh control beside the active Feed destination", () => {
    render();
    expect(refreshButtons()).toHaveLength(1);
    expect(refreshButtons()[0].getAttribute("aria-label")).toBe("刷新最新流");

    act(() => refreshButtons()[0].click());
    expect(output("timeline")).toBe("1");
    expect(window.location.pathname).toBe("/timeline");

    act(() => navButton("热门").click());
    expect(window.location.pathname).toBe("/hotfeed");
    expect(refreshButtons()).toHaveLength(1);
    expect(refreshButtons()[0].getAttribute("aria-label")).toBe("刷新热门流");
    expect(container.querySelector('button[aria-label="刷新最新流"]')).toBeNull();

    act(() => refreshButtons()[0].click());
    expect(output("hot")).toBe("1");
    expect(output("timeline")).toBe("1");
  });

  function render() {
    act(() => root.render(
      <RouterProvider>
        <FeedRefreshProvider>
          <SideNav />
          <RefreshProbe />
        </FeedRefreshProvider>
      </RouterProvider>
    ));
  }

  function refreshButtons(): HTMLButtonElement[] {
    return [...container.querySelectorAll<HTMLButtonElement>('[data-ui="feed-refresh"]')];
  }

  function navButton(label: string): HTMLButtonElement {
    const button = [...container.querySelectorAll<HTMLButtonElement>(".side-nav-link")]
      .find((candidate) => candidate.textContent?.includes(label));
    if (!button) throw new Error(`missing navigation button: ${label}`);
    return button;
  }

  function output(scene: string): string | null {
    return container.querySelector(`[data-scene-request="${scene}"]`)?.textContent || null;
  }
});

function RefreshProbe() {
  return (
    <>
      <output data-scene-request="timeline">{useFeedRefreshRequest("timeline")}</output>
      <output data-scene-request="hot">{useFeedRefreshRequest("hot")}</output>
    </>
  );
}
