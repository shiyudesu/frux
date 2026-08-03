// 手写路由：Route union 类型 + normalizeRoute + RouterProvider + useRoute/useNavigate。
// 搬运自 LegacyApp.jsx 的路由逻辑（normalizeRoute/navigate/popstate），行为不变。
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { FEED_SCENES } from "./constants";

/** 合法路由的判别联合；"/login"、"/me" 在 normalizeRoute 中被归一化，不会出现在此 union 中 */
export type Route =
  | "/"
  | "/auth"
  | "/recommend"
  | "/timeline"
  | "/following"
  | "/hotfeed"
  | "/profile"
  | "/upload"
  | "/messages"
  | `/users/${number}`
  | `/videos/${number}`;

export interface VideoDiscussionNavigation {
  route: `/videos/${number}`;
  comment?: number;
  highlight?: number;
}

export type NavigationTarget = Route | VideoDiscussionNavigation;

export interface VideoDiscussionRoute {
  videoID: number;
  commentID: number;
  highlightID: number;
  invalidFocus: boolean;
}

export function normalizeRoute(pathname: string): Route {
  if (pathname === "/login") return "/auth";
  if (pathname === "/me") return "/profile";
  if (/^\/users\/\d+$/.test(pathname)) return pathname as `/users/${number}`;
  if (/^\/videos\/\d+$/.test(pathname)) return pathname as `/videos/${number}`;
  switch (pathname) {
    case "/":
      return "/";
    case "/auth":
      return "/auth";
    case "/recommend":
      return "/recommend";
    case "/timeline":
      return "/timeline";
    case "/following":
      return "/following";
    case "/hotfeed":
      return "/hotfeed";
    case "/profile":
      return "/profile";
    case "/upload":
      return "/upload";
    case "/messages":
      return "/messages";
    default:
      return "/timeline";
  }
}

export function feedSceneFromRoute(route: Route): string {
  const scene = FEED_SCENES.find((item) => item.route === route);
  return scene?.key || FEED_SCENES[0].key;
}

export function publicUserIDFromRoute(route: Route): number {
  const match = /^\/users\/(\d+)$/.exec(route);
  if (!match) return 0;
  return Number(match[1]);
}

export function videoDiscussionFromLocation(route: Route, search: string): VideoDiscussionRoute | null {
  const match = /^\/videos\/(\d+)$/.exec(route);
  if (!match) return null;
  const videoID = positiveInteger(match[1]);
  const params = new URLSearchParams(search);
  const rawComment = params.get("comment");
  const rawHighlight = params.get("highlight");
  const commentID = rawComment === null ? 0 : positiveInteger(rawComment);
  const highlightID = rawHighlight === null ? 0 : positiveInteger(rawHighlight);
  const invalidFocus =
    (rawComment !== null && commentID === 0) ||
    (rawHighlight !== null && highlightID === 0) ||
    (highlightID > 0 && commentID === 0);
  return { videoID, commentID, highlightID, invalidFocus };
}

export function videoDiscussionPath(target: VideoDiscussionNavigation): string {
  const params = new URLSearchParams();
  if (target.comment && target.comment > 0) params.set("comment", String(Math.round(target.comment)));
  if (target.highlight && target.highlight > 0) params.set("highlight", String(Math.round(target.highlight)));
  const search = params.toString();
  return search ? `${target.route}?${search}` : target.route;
}

interface RouterValue {
  route: Route;
  search: string;
  navigate: (path: NavigationTarget) => void;
}

const RouterContext = createContext<RouterValue | null>(null);

export function RouterProvider({ children }: { children: ReactNode }) {
  const [route, setRoute] = useState<Route>(() => normalizeRoute(window.location.pathname));
  const [search, setSearch] = useState(() => window.location.search);

  useEffect(() => {
    const handlePopState = () => {
      setRoute(normalizeRoute(window.location.pathname));
      setSearch(window.location.search);
    };
    window.addEventListener("popstate", handlePopState);
    return () => window.removeEventListener("popstate", handlePopState);
  }, []);

  const navigate = useCallback((target: NavigationTarget) => {
    const authoredPath = typeof target === "string" ? target : videoDiscussionPath(target);
    const url = new URL(authoredPath, window.location.origin);
    const nextPath = normalizeRoute(url.pathname);
    window.history.pushState({}, "", `${nextPath}${url.search}`);
    setRoute(nextPath);
    setSearch(url.search);
  }, []);

  const value = useMemo(() => ({ route, search, navigate }), [navigate, route, search]);
  return <RouterContext.Provider value={value}>{children}</RouterContext.Provider>;
}

function useRouterValue(): RouterValue {
  const value = useContext(RouterContext);
  if (!value) {
    throw new Error("useRoute/useNavigate must be used within RouterProvider");
  }
  return value;
}

export function useRoute(): Route {
  return useRouterValue().route;
}

export function useNavigate(): (path: NavigationTarget) => void {
  return useRouterValue().navigate;
}

export function useVideoDiscussionRoute(): VideoDiscussionRoute | null {
  const { route, search } = useRouterValue();
  return useMemo(() => videoDiscussionFromLocation(route, search), [route, search]);
}

function positiveInteger(value: string): number {
  if (!/^\d+$/.test(value)) return 0;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : 0;
}
