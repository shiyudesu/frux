import { useState } from "react";
import { logoutSession } from "../api/account";
import { image } from "../constants";
import { useNavigate } from "../router";
import { useSession, useUnreadCount } from "../session";
import { formatBadgeCount } from "../utils";
import { Icon } from "./Icon";

export function TopNav() {
  const { token, user, clearAuth } = useSession();
  const { unreadCount } = useUnreadCount();
  const navigate = useNavigate();
  const [logoutBusy, setLogoutBusy] = useState(false);
  const authenticated = Boolean(token && user);

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
        <label className="search-box">
          <Icon name="search" size={20} />
          <input aria-label="搜索" placeholder="搜索你感兴趣的内容" />
          <span className="search-action">搜索</span>
        </label>
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
