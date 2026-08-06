import { useEffect } from "react";
import type { ReactNode } from "react";
import {
  adminReviewFromRoute,
  useNavigate,
  useRoute
} from "../router";
import type { AdminProtectedRoute, Route } from "../router";
import type { AdminPermission } from "../types";
import { AdminLoginPage } from "./AdminLoginPage";
import { AdminSessionProvider, useAdminSession } from "./adminSession";
import { ReviewDetailPage } from "./ReviewDetailPage";
import { ReviewQueuePage } from "./ReviewQueuePage";
import { VideoOperationsPage } from "./VideoOperationsPage";
import "./admin.css";

interface AdminDestination {
  label: string;
  route: Route;
  permission: AdminPermission;
}

const destinations: AdminDestination[] = [
  { label: "审核任务", route: "/admin/reviews", permission: "review.read" },
  { label: "视频运营", route: "/admin/videos", permission: "content.enforce" }
];

export default function AdminApp() {
  return (
    <AdminSessionProvider>
      <AdminRoutes />
    </AdminSessionProvider>
  );
}

function AdminRoutes() {
  const route = useRoute();
  const navigate = useNavigate();
  const { principal, state, refresh, logout } = useAdminSession();
  const reviewRoute = adminReviewFromRoute(route);
  const returnTo = adminProtectedRoute(route);

  useEffect(() => {
    if (route !== "/admin/login" && state === "unauthenticated") {
      navigate({ route: "/admin/login", returnTo });
    }
  }, [navigate, returnTo, route, state]);

  if (route === "/admin/login") {
    return <AdminLoginPage />;
  }
  if (state === "unauthenticated" || state === "loading") {
    return <AdminEntryState title="正在验证后台会话…" />;
  }
  if (state === "error") {
    return (
      <AdminEntryState
        title="后台会话暂时无法验证"
        detail="请重试，当前不会展示任何后台数据。"
        action="重新验证"
        onAction={() => void refresh()}
      />
    );
  }
  const permissions = new Set(principal?.permissions || []);
  const requiredPermission: AdminPermission = route === "/admin/videos"
    ? "content.enforce"
    : "review.read";
  if (state === "forbidden" || !permissions.has(requiredPermission)) {
    return (
      <AdminEntryState
        title="当前管理员无权访问"
        detail="服务端当前账号权限是最终依据。"
        action="退出后台"
        onAction={logout}
      />
    );
  }

  let page: ReactNode;
  if (route === "/admin/videos") {
    page = <VideoOperationsPage />;
  } else if (reviewRoute) {
    page = <ReviewDetailPage reviewID={reviewRoute.reviewID} />;
  } else {
    page = <ReviewQueuePage />;
  }
  return (
    <div className="admin-app">
      <aside className="admin-sidebar">
        <button className="admin-brand" type="button" onClick={() => navigate("/admin/reviews")}>
          Frux <span>Operations</span>
        </button>
        <nav aria-label="运营工作台导航">
          {destinations
            .filter((destination) => permissions.has(destination.permission))
            .map((destination) => (
              <button
                className={route === destination.route ||
                  (destination.route === "/admin/reviews" && Boolean(reviewRoute))
                  ? "active"
                  : ""}
                type="button"
                key={destination.route}
                onClick={() => navigate(destination.route)}
              >
                {destination.label}
              </button>
            ))}
        </nav>
        <div className="admin-principal">
          <strong>管理员 #{principal?.user_id}</strong>
          <span>{principal?.role}</span>
          <button className="admin-logout" type="button" onClick={logout}>退出后台</button>
        </div>
      </aside>
      <main className="admin-main">{page}</main>
    </div>
  );
}

function adminProtectedRoute(route: Route): AdminProtectedRoute {
  if (route === "/admin/videos" || route === "/admin/reviews") return route;
  if (/^\/admin\/reviews\/[1-9]\d*$/.test(route)) {
    return route as `/admin/reviews/${number}`;
  }
  return "/admin/reviews";
}

function AdminEntryState({
  title,
  detail,
  action,
  onAction
}: {
  title: string;
  detail?: string;
  action?: string;
  onAction?: () => void;
}) {
  return (
    <main className="admin-entry-state" role="status">
      <strong>{title}</strong>
      {detail && <p>{detail}</p>}
      {action && <button type="button" onClick={onAction}>{action}</button>}
    </main>
  );
}
