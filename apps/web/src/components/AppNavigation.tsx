import { FEED_SCENES } from "../constants";
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
      icon: scene.icon
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
  const items = useNavigationItems();
  const homeRoute = token && user ? "/recommend" : "/timeline";

  return (
    <aside className="side-nav" data-ui="side-nav">
      <button className="brand-button" type="button" onClick={() => navigate(homeRoute)} aria-label="返回视频首页">
        <BrandMark />
        <BrandMark compact />
      </button>
      <nav className="side-nav-list" aria-label="主导航">
        {items.map((item, index) => (
          <button
            className={`side-nav-link ${isActiveRoute(route, item) ? "active" : ""} ${index === 4 ? "section-start" : ""}`}
            data-active={isActiveRoute(route, item) ? "true" : "false"}
            key={`${item.label}-${item.route}`}
            type="button"
            onClick={() => navigate(item.route)}
          >
            <Icon name={item.icon} filled={isActiveRoute(route, item)} />
            <span>{item.label}</span>
            {Boolean(item.badge) && <span className="nav-badge">{formatBadgeCount(item.badge || 0)}</span>}
          </button>
        ))}
      </nav>
      <div className="side-nav-footer">
        <span>GCFeed Web</span>
        <small>沉浸短视频体验</small>
      </div>
    </aside>
  );
}
