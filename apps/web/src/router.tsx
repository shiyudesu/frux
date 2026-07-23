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
  | `/users/${number}`;

export function normalizeRoute(pathname: string): Route {
  if (pathname === "/login") return "/auth";
  if (pathname === "/me") return "/profile";
  if (/^\/users\/\d+$/.test(pathname)) return pathname as `/users/${number}`;
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

interface RouterValue {
  route: Route;
  navigate: (path: Route) => void;
}

const RouterContext = createContext<RouterValue | null>(null);

export function RouterProvider({ children }: { children: ReactNode }) {
  const [route, setRoute] = useState<Route>(() => normalizeRoute(window.location.pathname));

  useEffect(() => {
    const handlePopState = () => setRoute(normalizeRoute(window.location.pathname));
    window.addEventListener("popstate", handlePopState);
    return () => window.removeEventListener("popstate", handlePopState);
  }, []);

  const navigate = useCallback((path: Route) => {
    const nextPath = normalizeRoute(path);
    window.history.pushState({}, "", nextPath);
    setRoute(nextPath);
  }, []);

  const value = useMemo(() => ({ route, navigate }), [route, navigate]);
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

export function useNavigate(): (path: Route) => void {
  return useRouterValue().navigate;
}
