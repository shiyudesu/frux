import { useMemo } from "react";
import type { ReactNode } from "react";
import { adminReviewFromRoute, useNavigate, useRoute } from "../router";
import type { Route } from "../router";
import { useSession } from "../session";
import type { AdminPermission } from "../types";
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
  { label: "审核队列", route: "/admin/reviews", permission: "review.read" },
  { label: "视频运营", route: "/admin/videos", permission: "content.enforce" }
];

export default function AdminApp() {
  const route = useRoute();
  const navigate = useNavigate();
  const {
    token, user, adminPrincipal, adminState, refreshAdminPrincipal
  } = useSession();
  const reviewRoute = adminReviewFromRoute(route);
  const requiredPermission: AdminPermission = route === "/admin/videos"
    ? "content.enforce"
    : "review.read";
  const permissions = useMemo(
    () => new Set(adminPrincipal?.permissions || []),
    [adminPrincipal]
  );

  if (!token || !user) {
    return (
      <AdminEntryState
        title="请先登录"
        detail="登录后才能访问运营工作台。"
        action="前往登录"
        onAction={() => navigate("/auth")}
      />
    );
  }
  if (adminState === "loading" || adminState === "idle") {
    return <AdminEntryState title="正在验证运营权限…" />;
  }
  if (adminState === "error") {
    return (
      <AdminEntryState
        title="运营权限暂时无法验证"
        detail="请重试，当前不会展示任何后台数据。"
        action="重新验证"
        onAction={() => void refreshAdminPrincipal()}
      />
    );
  }
  if (adminState === "forbidden" || !permissions.has(requiredPermission)) {
    return (
      <AdminEntryState
        title="当前账号无权访问"
        detail="服务端权限是最终依据。"
        action="返回首页"
        onAction={() => navigate("/timeline")}
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
        <button className="admin-brand" type="button" onClick={() => navigate("/timeline")}>
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
          <strong>{user.nickname}</strong>
          <span>{adminPrincipal?.role}</span>
        </div>
      </aside>
      <main className="admin-main">{page}</main>
    </div>
  );
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
