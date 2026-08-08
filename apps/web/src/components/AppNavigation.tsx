import { FEED_SCENES } from "../constants";
import type { FeedSceneKey } from "../constants";
import { useRequestFeedRefresh } from "../feedRefresh";
import type { Route } from "../router";
import { useNavigate, useRoute } from "../router";
import { useSession, useUnreadCount } from "../session";
import { formatBadgeCount } from "../utils";
import { BrandMark } from "./BrandMark";
import { Icon } from "./Icon";
import type { IconName } from "./Icon";

interface NavItem {
  label: string;
  route: Route;
  icon: IconName;
  feedScene?: FeedSceneKey;
  requiresAuth?: boolean;
  badge?: number;
}

function useNavigationItems(): NavItem[] {
  const { token, user } = useSession();
  const { unreadCount } = useUnreadCount();
  const authenticated = Boolean(token && user);

  return [
    ...FEED_SCENES.map((scene) => ({
      label: scene.label,
      route: scene.route,
      icon: scene.icon,
      feedScene: scene.key
    })),
    {
      label: "消息",
      route: authenticated ? "/messages" : "/auth",
      icon: "message" as const,
      requiresAuth: true,
      badge: authenticated ? unreadCount : 0
    },
    {
      label: "投稿",
      route: authenticated ? "/upload" : "/auth",
      icon: "upload" as const,
      requiresAuth: true
    },
    {
      label: "我的",
      route: authenticated ? "/profile" : "/auth",
      icon: "user" as const,
      requiresAuth: true
    }
  ];
}

function isActiveRoute(current: Route, item: NavItem): boolean {
  if (item.requiresAuth && item.route === "/auth") return false;
  return current === item.route;
}

export function SideNav() {
  const route = useRoute();
  const navigate = useNavigate();
  const { token, user } = useSession();
  const requestFeedRefresh = useRequestFeedRefresh();
  const items = useNavigationItems();
  const homeRoute = token && user ? "/recommend" : "/timeline";

  return (
    <aside className="side-nav" data-ui="side-nav">
      <button className="brand-button" type="button" onClick={() => navigate(homeRoute)} aria-label="返回视频首页">
        <BrandMark />
        <BrandMark compact />
      </button>
      <nav className="side-nav-list" aria-label="主导航">
        {items.map((item, index) => {
          const active = isActiveRoute(route, item);
          const refreshable = active && item.feedScene;
          return (
            <div
              className={`side-nav-entry ${refreshable ? "has-refresh" : ""} ${index === 4 ? "section-start" : ""}`}
              key={`${item.label}-${item.route}`}
            >
              <button
                className={`side-nav-link ${active ? "active" : ""}`}
                data-active={active ? "true" : "false"}
                type="button"
                onClick={() => navigate(item.route)}
              >
                <Icon name={item.icon} filled={active} />
                <span>{item.label}</span>
                {Boolean(item.badge) && <span className="nav-badge">{formatBadgeCount(item.badge || 0)}</span>}
              </button>
              {refreshable && (
                <button
                  aria-label={`刷新${item.label}流`}
                  className="side-nav-refresh"
                  data-scene={item.feedScene}
                  data-ui="feed-refresh"
                  title={`刷新${item.label}流`}
                  type="button"
                  onClick={() => requestFeedRefresh(refreshable)}
                >
                  <Icon name="refresh" size={16} />
                </button>
              )}
            </div>
          );
        })}
      </nav>
      <div className="side-nav-footer">
        <span>Frux Web</span>
        <small>沉浸短视频体验</small>
      </div>
    </aside>
  );
}
