// 应用外壳：TopNav + 侧边栏（Feed 场景导航 + 消息入口）+ 页面内容。
// 迁移后通过 hooks 自取 route/session/unreadCount，不再接收透传 props。
import type { ReactNode } from "react";
import { FEED_SCENES } from "../constants";
import { useNavigate, useRoute } from "../router";
import { useSession, useUnreadCount } from "../session";
import { formatBadgeCount } from "../utils";
import { TopNav } from "./TopNav";

export function AppShell({ children }: { children: ReactNode }) {
  const route = useRoute();
  const navigate = useNavigate();
  const { token, user } = useSession();
  const { unreadCount } = useUnreadCount();
  const authenticated = Boolean(token && user);

  return (
    <div className="app-shell">
      <TopNav />
      <div className="app-body">
        <aside className="sidebar">
          {FEED_SCENES.map((scene) => (
            <button
              className={`sidebar-link ${route === scene.route ? "active" : ""}`}
              key={scene.key}
              onClick={() => navigate(scene.route)}
            >
              <span className="material-symbols-outlined filled">{scene.icon}</span>
              <span>{scene.label}</span>
            </button>
          ))}
          <button
            className={`sidebar-link ${route === "/messages" ? "active" : ""}`}
            onClick={() => navigate(authenticated ? "/messages" : "/auth")}
          >
            <span className="material-symbols-outlined filled">notifications</span>
            <span>消息</span>
            {authenticated && unreadCount > 0 && <span className="nav-badge">{formatBadgeCount(unreadCount)}</span>}
          </button>
        </aside>
        {children}
      </div>
    </div>
  );
}
