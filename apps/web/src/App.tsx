import { lazy, Suspense, useEffect } from "react";
import { AppShell } from "./components/AppShell";
import { FeedPage } from "./pages/FeedPage";
import { LoginPage } from "./pages/LoginPage";
import { MessagesPage } from "./pages/MessagesPage";
import { ProfilePage } from "./pages/ProfilePage";
import { PublicProfilePage } from "./pages/PublicProfilePage";
import { UploadPage } from "./pages/UploadPage";
import { VideoDetailPage } from "./pages/VideoDetailPage";
import { SearchPage } from "./pages/SearchPage";
import {
  RouterProvider,
  feedSceneFromRoute,
  useMessageRoute,
  publicUserIDFromRoute,
  useNavigate,
  useRoute,
  useSearchRoute,
  useVideoDiscussionRoute
} from "./router";
import { SessionProvider, useSession } from "./session";
import { PageMessage } from "./components/StatusMessages";
import { FeedRefreshProvider } from "./feedRefresh";

const AdminApp = lazy(() => import("./admin/AdminApp"));

export default function App() {
  return (
    <RouterProvider>
      <SessionProvider>
        <FeedRefreshProvider>
          <AppRoutes />
        </FeedRefreshProvider>
      </SessionProvider>
    </RouterProvider>
  );
}

function AppRoutes() {
  const route = useRoute();
  const videoDiscussion = useVideoDiscussionRoute();
  const messageRoute = useMessageRoute();
  const searchRoute = useSearchRoute();
  const navigate = useNavigate();
  const { token, user, status } = useSession();
  const bootstrapping = status === "bootstrapping";
  const sessionKey = user?.id ? `user-${user.id}` : "guest";

  useEffect(() => {
    if (bootstrapping) return;
    if (route === "/") {
      navigate(token ? "/recommend" : "/timeline");
    }
    if (route === "/profile" && !(token && user)) {
      navigate("/timeline");
    }
    if ((route === "/messages" || messageRoute) && !(token && user)) {
      navigate("/auth");
    }
  }, [bootstrapping, messageRoute, navigate, route, token, user]);

  if (route === "/auth") {
    return <LoginPage />;
  }

  if (route.startsWith("/admin/")) {
    return (
      <Suspense fallback={<div className="admin-entry-state">正在加载运营工作台…</div>}>
        <AdminApp />
      </Suspense>
    );
  }

  if (route === "/not-found") {
    return (
      <AppShell key={sessionKey}>
        <PageMessage icon="alert" title="页面不存在" action="返回首页" onAction={() => navigate("/timeline")} />
      </AppShell>
    );
  }

  if (bootstrapping && (route === "/" || route === "/profile" || route === "/messages" || messageRoute)) {
    return (
      <AppShell key={sessionKey}>
        <PageMessage icon="hourglass" title="正在恢复登录状态" />
      </AppShell>
    );
  }

  if (route === "/profile") {
    return (
      <AppShell key={sessionKey}>
        <ProfilePage />
      </AppShell>
    );
  }

  if (searchRoute) {
    return (
      <AppShell key={sessionKey}>
        <SearchPage {...searchRoute} />
      </AppShell>
    );
  }

  if (videoDiscussion) {
    return (
      <AppShell key={sessionKey}>
        <VideoDetailPage {...videoDiscussion} />
      </AppShell>
    );
  }

  const publicUserID = publicUserIDFromRoute(route);
  if (publicUserID > 0) {
    return (
      <AppShell key={sessionKey}>
        <PublicProfilePage userID={publicUserID} />
      </AppShell>
    );
  }

  if (route === "/upload") {
    return (
      <AppShell key={sessionKey}>
        <UploadPage />
      </AppShell>
    );
  }

  if (route === "/messages" || messageRoute) {
    return (
      <AppShell key={sessionKey}>
        <MessagesPage conversationID={messageRoute?.conversationID} />
      </AppShell>
    );
  }

  return (
    <AppShell key={sessionKey}>
      <FeedPage feedScene={feedSceneFromRoute(route)} />
    </AppShell>
  );
}
