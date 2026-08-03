// 根组件：Provider 组装 + 路由分发。
import { useEffect } from "react";
import { AppShell } from "./components/AppShell";
import { FeedPage } from "./pages/FeedPage";
import { LoginPage } from "./pages/LoginPage";
import { MessagesPage } from "./pages/MessagesPage";
import { ProfilePage } from "./pages/ProfilePage";
import { PublicProfilePage } from "./pages/PublicProfilePage";
import { UploadPage } from "./pages/UploadPage";
import { VideoDetailPage } from "./pages/VideoDetailPage";
import {
  RouterProvider,
  feedSceneFromRoute,
  publicUserIDFromRoute,
  useNavigate,
  useRoute,
  useVideoDiscussionRoute
} from "./router";
import { SessionProvider, useSession } from "./session";

export default function App() {
  return (
    <RouterProvider>
      <SessionProvider>
        <AppRoutes />
      </SessionProvider>
    </RouterProvider>
  );
}

function AppRoutes() {
  const route = useRoute();
  const videoDiscussion = useVideoDiscussionRoute();
  const navigate = useNavigate();
  const { token, user } = useSession();

  // 路由守卫："/" 重定向到默认场景；需登录页面未登录时重定向。
  // （"/login"、"/me" 已在 normalizeRoute 归一化为 "/auth"、"/profile"，无需在此判断。）
  useEffect(() => {
    if (route === "/") {
      navigate(token ? "/recommend" : "/timeline");
    }
    if (route === "/profile" && !(token && user)) {
      navigate("/timeline");
    }
    if (route === "/messages" && !(token && user)) {
      navigate("/auth");
    }
  }, [navigate, route, token, user]);

  if (route === "/auth") {
    return <LoginPage />;
  }

  if (route === "/profile") {
    return (
      <AppShell>
        <ProfilePage />
      </AppShell>
    );
  }

  if (videoDiscussion) {
    return (
      <AppShell>
        <VideoDetailPage {...videoDiscussion} />
      </AppShell>
    );
  }

  const publicUserID = publicUserIDFromRoute(route);
  if (publicUserID > 0) {
    return (
      <AppShell>
        <PublicProfilePage userID={publicUserID} />
      </AppShell>
    );
  }

  if (route === "/upload") {
    return (
      <AppShell>
        <UploadPage />
      </AppShell>
    );
  }

  if (route === "/messages") {
    return (
      <AppShell>
        <MessagesPage />
      </AppShell>
    );
  }

  return (
    <AppShell>
      <FeedPage feedScene={feedSceneFromRoute(route)} />
    </AppShell>
  );
}
