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
  | "/search"
  | "/upload"
  | "/messages"
  | `/messages/${number}`
  | "/not-found"
  | "/admin/login"
  | "/admin/reviews"
  | "/admin/videos"
  | "/admin/accounts"
  | `/admin/reviews/${number}`
  | `/users/${number}`
  | `/videos/${number}`;

export interface VideoDiscussionNavigation {
  route: `/videos/${number}`;
  comment?: number;
  highlight?: number;
}

export function messageFromRoute(route: Route): MessageRoute | null {
  const match = /^\/messages\/(\d+)$/.exec(route);
  if (!match) return null;
  const conversationID = positiveInteger(match[1]);
  return conversationID > 0 ? { conversationID } : null;
}

export function messagePath(target: MessageNavigation): string {
  const match = /^\/messages\/(\d+)$/.exec(target.route);
  if (!match || positiveInteger(match[1]) <= 0) return "/messages";
  return `/messages/${positiveInteger(match[1])}`;
}

export type SearchTab = "videos" | "users";

export interface SearchNavigation {
  route: "/search";
  query: string;
  tab?: SearchTab;
}

export function useMessageRoute(): MessageRoute | null {
  return messageFromRoute(useRouterValue().route);
}

export interface ProfileNavigation {
  route: "/profile";
  video?: number;
}

export interface MessageNavigation {
  route: `/messages/${number}`;
}

export interface SearchRoute {
  query: string;
  tab: SearchTab;
}

export type AdminProtectedRoute =
  | "/admin/reviews"
  | "/admin/videos"
  | "/admin/accounts"
  | `/admin/reviews/${number}`;

export interface AdminLoginNavigation {
  route: "/admin/login";
  returnTo?: AdminProtectedRoute;
}

export type NavigationTarget =
  | Route
  | VideoDiscussionNavigation
  | SearchNavigation
  | ProfileNavigation
  | MessageNavigation
  | AdminLoginNavigation;

export interface VideoDiscussionRoute {
  videoID: number;
  commentID: number;
  highlightID: number;
  invalidFocus: boolean;
}

export interface AdminReviewRoute {
  reviewID: number;
}

export interface AdminLoginRoute {
  returnTo: AdminProtectedRoute;
}

export interface MessageRoute {
  conversationID: number;
}

export function normalizeRoute(pathname: string): Route {
  if (pathname === "/login") return "/auth";
  if (pathname === "/me") return "/profile";
  if (/^\/users\/\d+$/.test(pathname)) return pathname as `/users/${number}`;
  if (/^\/videos\/\d+$/.test(pathname)) return pathname as `/videos/${number}`;
  const messageMatch = /^\/messages\/(\d+)$/.exec(pathname);
  if (messageMatch) {
    return positiveInteger(messageMatch[1]) > 0
      ? pathname as `/messages/${number}`
      : "/not-found";
  }
  if (pathname.startsWith("/messages/")) return "/not-found";
  const reviewMatch = /^\/admin\/reviews\/(\d+)$/.exec(pathname);
  if (reviewMatch) {
    return positiveInteger(reviewMatch[1]) > 0
      ? pathname as `/admin/reviews/${number}`
      : "/not-found";
  }
  if (pathname.startsWith("/admin/reviews/")) return "/not-found";
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
    case "/search":
      return "/search";
    case "/upload":
      return "/upload";
    case "/messages":
      return "/messages";
    case "/not-found":
      return "/not-found";
    case "/admin/login":
      return "/admin/login";
    case "/admin/reviews":
      return "/admin/reviews";
    case "/admin/videos":
      return "/admin/videos";
    case "/admin/accounts":
      return "/admin/accounts";
    default:
      return "/timeline";
  }
}

export function adminLoginFromLocation(route: Route, search: string): AdminLoginRoute | null {
  if (route !== "/admin/login") return null;
  const rawReturn = new URLSearchParams(search).get("return") || "";
  return { returnTo: validAdminReturnRoute(rawReturn) || "/admin/reviews" };
}

export function adminLoginPath(target: AdminLoginNavigation): string {
  if (!target.returnTo) return "/admin/login";
  return `/admin/login?return=${encodeURIComponent(target.returnTo)}`;
}

export function adminReviewFromRoute(route: Route): AdminReviewRoute | null {
  const match = /^\/admin\/reviews\/(\d+)$/.exec(route);
  if (!match) return null;
  const reviewID = positiveInteger(match[1]);
  return reviewID > 0 ? { reviewID } : null;
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

export function searchFromLocation(route: Route, search: string): SearchRoute | null {
  if (route !== "/search") return null;
  const params = new URLSearchParams(search);
  return {
    query: (params.get("q") || "").trim(),
    tab: params.get("tab") === "users" ? "users" : "videos"
  };
}

export function searchPath(target: SearchNavigation): string {
  const params = new URLSearchParams();
  const query = target.query.trim();
  if (query) params.set("q", query);
  if (target.tab === "users") params.set("tab", "users");
  const search = params.toString();
  return search ? `/search?${search}` : "/search";
}

export function profilePath(target: ProfileNavigation): string {
  const videoID = target.video && target.video > 0 ? Math.round(target.video) : 0;
  return videoID > 0 ? `/profile?video=${videoID}` : "/profile";
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
    const authoredPath = typeof target === "string"
      ? target
      : target.route === "/search"
        ? searchPath(target)
        : target.route === "/admin/login"
          ? adminLoginPath(target)
          : target.route === "/profile"
            ? profilePath(target)
            : /^\/messages\/\d+$/.test(target.route)
              ? messagePath(target as MessageNavigation)
              : videoDiscussionPath(target as VideoDiscussionNavigation);
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

export function useSearchRoute(): SearchRoute | null {
  const { route, search } = useRouterValue();
  return useMemo(() => searchFromLocation(route, search), [route, search]);
}

export function useAdminLoginRoute(): AdminLoginRoute | null {
  const { route, search } = useRouterValue();
  return useMemo(() => adminLoginFromLocation(route, search), [route, search]);
}

export function useProfileVideoTarget(): number {
  const { route, search } = useRouterValue();
  return useMemo(() => {
    if (route !== "/profile") return 0;
    return positiveInteger(new URLSearchParams(search).get("video") || "");
  }, [route, search]);
}

function validAdminReturnRoute(value: string): AdminProtectedRoute | null {
  if (value === "/admin/reviews" || value === "/admin/videos" || value === "/admin/accounts") return value;
  if (/^\/admin\/reviews\/[1-9]\d*$/.test(value)) {
    return value as `/admin/reviews/${number}`;
  }
  return null;
}

function positiveInteger(value: string): number {
  if (!/^\d+$/.test(value)) return 0;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : 0;
}
