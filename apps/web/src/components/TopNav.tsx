import { useEffect, useRef, useState } from "react";
import { logoutSession } from "../api/account";
import { image } from "../constants";
import { useNavigate, useSearchRoute } from "../router";
import { useSession, useUnreadCount } from "../session";
import { formatBadgeCount } from "../utils";
import { Icon } from "./Icon";

export function TopNav() {
  const {
    token, user, beginLogout, completeLogout, runCredentialMutation, clearAuth
  } = useSession();
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
    beginLogout();
    navigate("/timeline");
    try {
      await runCredentialMutation(async () => {
        beginLogout();
        try {
          await logoutSession();
          clearAuth();
          completeLogout();
        } catch (error) {
          beginLogout();
          throw error;
        }
      });
    } catch {
      beginLogout();
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
          <span>投稿</span>
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
            <TopNavOverflow busy={logoutBusy} onLogout={() => void handleLogout()} />
          </>
        )}
      </div>
    </header>
  );
}

interface TopNavOverflowProps {
  busy: boolean;
  onLogout: () => void;
}

function TopNavOverflow({ busy, onLogout }: TopNavOverflowProps) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const logoutRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (!open) return undefined;
    logoutRef.current?.focus();
    const handlePointerDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      setOpen(false);
      triggerRef.current?.focus();
    };
    document.addEventListener("pointerdown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [open]);

  return (
    <div className="top-overflow" ref={rootRef}>
      <button
        ref={triggerRef}
        className="icon-button"
        type="button"
        aria-label="更多账户操作"
        aria-expanded={open}
        aria-haspopup="menu"
        onClick={() => setOpen((current) => !current)}
      >
        <Icon name="more" />
      </button>
      {open && (
        <div className="top-overflow-menu" role="menu" aria-label="账户操作">
          <button
            ref={logoutRef}
            disabled={busy}
            type="button"
            role="menuitem"
            onClick={() => {
              setOpen(false);
              onLogout();
            }}
          >
            <Icon name="logout" size={17} />
            <span>{busy ? "正在退出登录" : "退出登录"}</span>
          </button>
        </div>
      )}
    </div>
  );
}
