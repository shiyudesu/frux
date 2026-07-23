// 顶部导航栏：wordmark、搜索、发布、通知、头像、退出登录。
// 迁移后通过 useSession/useUnreadCount/useNavigate 自取状态，不再接收透传 props。
import { logoutSession } from "../api/account";
import { image } from "../constants";
import { useNavigate } from "../router";
import { useSession, useUnreadCount } from "../session";
import { formatBadgeCount } from "../utils";

export function TopNav() {
  const { token, user, clearAuth } = useSession();
  const { unreadCount } = useUnreadCount();
  const navigate = useNavigate();
  const authenticated = Boolean(token && user);

  function handleLogout() {
    if (token) {
      logoutSession(token).catch(() => {});
    }
    clearAuth();
    navigate("/timeline");
  }

  return (
    <header className="top-nav">
      <div className="top-left">
        <button className="wordmark" onClick={() => navigate(authenticated ? "/recommend" : "/timeline")}>
          GCFeed
        </button>
      </div>
      <div className="top-center">
        <label className="search-box">
          <span className="material-symbols-outlined">search</span>
          <input placeholder="搜索" />
        </label>
      </div>
      <div className="top-actions">
        <button className="upload-button" onClick={() => navigate(authenticated ? "/upload" : "/auth")}>
          <span className="material-symbols-outlined">upload</span>
          发布
        </button>
        <button className="icon-button badge-button" aria-label="通知" onClick={() => navigate(authenticated ? "/messages" : "/auth")}>
          <span className="material-symbols-outlined">notifications</span>
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
              <span className="material-symbols-outlined">person</span>
              <span>登录</span>
            </>
          )}
        </button>
        {authenticated && (
          <button className="icon-button" onClick={handleLogout} aria-label="退出登录">
            <span className="material-symbols-outlined">logout</span>
          </button>
        )}
      </div>
    </header>
  );
}
