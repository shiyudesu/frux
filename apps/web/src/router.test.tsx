// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  normalizeRoute,
  RouterProvider,
  searchFromLocation,
  searchPath,
  useNavigate,
  useRoute,
  useSearchRoute,
  useVideoDiscussionRoute,
  videoDiscussionFromLocation,
  videoDiscussionPath
} from "./router";

describe("typed video discussion routing", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    window.history.replaceState({}, "", "/timeline");
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  it("authors a typed target and renders it through RouterProvider", () => {
    renderRouter();
    click(required<HTMLButtonElement>('[data-testid="typed-video"]'));
    expect(window.location.pathname).toBe("/videos/42");
    expect(window.location.search).toBe("?comment=7&highlight=9");
    expect(required<HTMLElement>('[data-testid="route"]').textContent).toBe("/videos/42");
    expect(required<HTMLElement>('[data-testid="discussion"]').textContent).toBe("42:7:9:false");
  });

  it("tracks popstate and preserves a direct reload target", () => {
    renderRouter();
    act(() => {
      window.history.pushState({}, "", "/videos/8?comment=4&highlight=6");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(required<HTMLElement>('[data-testid="discussion"]').textContent).toBe("8:4:6:false");

    act(() => root.unmount());
    root = createRoot(container);
    window.history.replaceState({}, "", "/videos/8?comment=4&highlight=6");
    renderRouter();
    expect(required<HTMLElement>('[data-testid="route"]').textContent).toBe("/videos/8");
    expect(required<HTMLElement>('[data-testid="discussion"]').textContent).toBe("8:4:6:false");
  });

  it("renders malformed and inconsistent focus searches safely", () => {
    window.history.replaceState({}, "", "/videos/42?comment=nope&highlight=9");
    renderRouter();
    expect(required<HTMLElement>('[data-testid="discussion"]').textContent).toBe("42:0:9:true");

    act(() => {
      window.history.pushState({}, "", "/videos/42?highlight=9");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(required<HTMLElement>('[data-testid="discussion"]').textContent).toBe("42:0:9:true");
  });

  it("keeps pure route parsing compatible with valid and malformed URLs", () => {
    const path = videoDiscussionPath({ route: "/videos/42", comment: 7, highlight: 9 });
    expect(path).toBe("/videos/42?comment=7&highlight=9");
    const url = new URL(path, "https://gcfeed.test");
    const route = normalizeRoute(url.pathname);
    expect(videoDiscussionFromLocation(route, url.search)).toEqual({
      videoID: 42,
      commentID: 7,
      highlightID: 9,
      invalidFocus: false
    });

    expect(videoDiscussionFromLocation("/videos/42", "?comment=-2")).toMatchObject({
      invalidFocus: true
    });
  });

  it("authors and normalizes typed search destinations", () => {
    renderRouter();
    click(required<HTMLButtonElement>('[data-testid="typed-search"]'));
    expect(window.location.pathname).toBe("/search");
    expect(window.location.search).toBe("?q=%E7%8C%AB&tab=users");
    expect(required<HTMLElement>('[data-testid="search"]').textContent).toBe("猫:users");

    expect(searchPath({ route: "/search", query: "  视频  " })).toBe("/search?q=%E8%A7%86%E9%A2%91");
    expect(searchFromLocation("/search", "?q=test&tab=invalid")).toEqual({
      query: "test",
      tab: "videos"
    });
    expect(normalizeRoute("/search")).toBe("/search");
  });

  function renderRouter() {
    act(() => root.render(
      <RouterProvider>
        <RouterProbe />
      </RouterProvider>
    ));
  }

  function required<T extends Element>(selector: string): T {
    const element = container.querySelector<T>(selector);
    if (!element) throw new Error(`missing element: ${selector}`);
    return element;
  }
});

function RouterProbe() {
  const route = useRoute();
  const discussion = useVideoDiscussionRoute();
  const search = useSearchRoute();
  const navigate = useNavigate();
  return (
    <>
      <output data-testid="route">{route}</output>
      <output data-testid="discussion">
        {discussion
          ? `${discussion.videoID}:${discussion.commentID}:${discussion.highlightID}:${discussion.invalidFocus}`
          : "none"}
      </output>
      <output data-testid="search">{search ? `${search.query}:${search.tab}` : "none"}</output>
      <button
        data-testid="typed-video"
        type="button"
        onClick={() => navigate({ route: "/videos/42", comment: 7, highlight: 9 })}
      >
        Open
      </button>
      <button
        data-testid="typed-search"
        type="button"
        onClick={() => navigate({ route: "/search", query: "猫", tab: "users" })}
      >
        Search
      </button>
    </>
  );
}

function click(element: HTMLElement) {
  act(() => {
    element.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
}
