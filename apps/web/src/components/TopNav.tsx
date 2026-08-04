import { useEffect, useState } from "react";
import { logoutSession } from "../api/account";
import { image } from "../constants";
import { useNavigate, useSearchRoute } from "../router";
import { useSession, useUnreadCount } from "../session";
import { formatBadgeCount } from "../utils";
import { Icon } from "./Icon";

export function TopNav() {
  const { token, user, clearAuth } = useSession();
  const { unreadCount } = useUnreadCount();
  const navigate = useNavigate();
  const searchRoute = useSearchRoute();
  const [logoutBusy, setLogoutBusy] = useState(false);
  const [searchQuery, setSearchQuery] = useState(searchRoute?.query || "");
  const authenticated = Boolean(token && user);

  useEffect(() => {
    if (searchRoute) setSearchQuery(searchRoute.query);
  }, [searchRoute]);

  async function handleLogout() {
    if (logoutBusy) return;
    setLogoutBusy(true);
    const currentToken = token;
    clearAuth();
    navigate("/timeline");
    try {
      await logoutSession(currentToken || undefined);
    } catch {
      // Local logout and private-asset deactivation are authoritative when offline.
    } finally {
      setLogoutBusy(false);
    }
  }

  return (
    <header className="top-nav" data-ui="top-nav">
      <div className="top-center">
        <form
          className="search-box"
          role="search"
          onSubmit={(event) => {
            event.preventDefault();
            navigate({ route: "/search", query: searchQuery });
          }}
        >
          <Icon name="search" size={20} />
          <input
            aria-label="搜索"
            placeholder="搜索视频或用户"
            value={searchQuery}
            onChange={(event) => setSearchQuery(event.target.value)}
          />
          <button className="search-action" type="submit">搜索</button>
        </form>
      </div>
      <div className="top-actions">
        <button className="top-action-button upload-button" onClick={() => navigate(authenticated ? "/upload" : "/auth")}>
          <Icon name="upload" size={18} />
          投稿
        </button>
        <button className="icon-button badge-button" type="button" aria-label="通知" onClick={() => navigate(authenticated ? "/messages" : "/auth")}>
          <Icon name="bell" />
          {authenticated && unreadCount > 0 && <span className="nav-badge floating">{formatBadgeCount(unreadCount)}</span>}
        </button>
        <button
          className={`avatar-button ${authenticated ? "" : "guest"}`}
          onClick={() => navigate(authenticated ? "/profile" : "/auth")}
          aria-label={authenticated ? "个人资料" : "登录"}
        >
          {authenticated ? (
            <img src={user?.avatar_url || image.currentUser} alt="" />
          ) : (
            <>
              <Icon name="user" size={18} />
              <span>登录</span>
            </>
          )}
        </button>
        {authenticated && (
          <>
            <button
              className="icon-button desktop-logout"
              disabled={logoutBusy}
              type="button"
              onClick={() => void handleLogout()}
              aria-label={logoutBusy ? "正在退出登录" : "退出登录"}
            >
              <Icon name="logout" />
            </button>
          </>
        )}
      </div>
    </header>
  );
}
