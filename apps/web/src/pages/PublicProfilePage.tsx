import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { fetchPublicProfile, fetchUserVideos } from "../api/account";
import { apiErrorMessage } from "../api/client";
import { fetchPublicCollections } from "../api/creator";
import { fetchPublicLikedVideos } from "../api/library";
import { fetchFollowState, followUser } from "../api/social";
import {
  ProfileCollectionGrid,
  ProfileEmptyState,
  ProfileHero,
  ProfilePrimaryTabs,
  ProfileVideoGrid
} from "../components/ProfileDashboard";
import { WorkViewer } from "../components/WorkViewer";
import { image } from "../constants";
import { useNavigate } from "../router";
import { updateSessionRelationCount, useSession } from "../session";
import type {
  AsyncState,
  LibraryVideoItem,
  PublicProfileTab,
  PublicUserProfile,
  Video,
  VideoCollection
} from "../types";
import { normalizePublicProfile, readPublicProfile, savePublicProfile } from "../utils";

interface CursorState<T> {
  items: T[];
  cursor: string;
  hasMore: boolean;
  state: AsyncState;
  error: string;
}

function emptyCursorState<T>(): CursorState<T> {
  return { items: [], cursor: "", hasMore: false, state: "idle", error: "" };
}

export function PublicProfilePage({ userID }: { userID: number }) {
  return <PublicProfileContent key={userID} userID={userID} />;
}

function PublicProfileContent({ userID }: { userID: number }) {
  const navigate = useNavigate();
  const session = useSession();
  const cached = useMemo(() => readPublicProfile(userID), [userID]);
  const [profile, setProfile] = useState<PublicUserProfile | null>(null);
  const [profileState, setProfileState] = useState<AsyncState>("loading");
  const [profileError, setProfileError] = useState("");
  const [activeTab, setActiveTab] = useState<PublicProfileTab>("works");
  const [videos, setVideos] = useState<Video[]>([]);
  const [videosState, setVideosState] = useState<AsyncState>("loading");
  const [videosError, setVideosError] = useState("");
  const [likes, setLikes] = useState<CursorState<LibraryVideoItem>>(emptyCursorState);
  const [collections, setCollections] = useState<CursorState<VideoCollection>>(emptyCursorState);
  const [selectedWork, setSelectedWork] = useState<Video | null>(null);
  const [following, setFollowing] = useState(false);
  const [followBusy, setFollowBusy] = useState(false);
  const profileRequest = useRef(0);
  const videosRequest = useRef(0);
  const followingRequest = useRef(0);
  const likesRequest = useRef(0);
  const collectionsRequest = useRef(0);

  useEffect(() => {
    const requestID = profileRequest.current + 1;
    profileRequest.current = requestID;
    setProfile(null);
    setProfileState("loading");
    setProfileError("");
    fetchPublicProfile(userID)
      .then((data) => {
        if (profileRequest.current !== requestID) return;
        setProfile(data);
        const stored = normalizePublicProfile(data);
        if (stored) savePublicProfile(stored);
        setProfileState("ready");
      })
      .catch((error: unknown) => {
        if (profileRequest.current !== requestID) return;
        setProfileError(apiErrorMessage(error, "资料加载失败"));
        setProfileState("error");
      });
    return () => {
      if (profileRequest.current === requestID) profileRequest.current += 1;
    };
  }, [userID]);

  const loadVideos = useCallback(async () => {
    const requestID = videosRequest.current + 1;
    videosRequest.current = requestID;
    setVideosState("loading");
    setVideosError("");
    try {
      const data = await fetchUserVideos(userID, 24);
      if (videosRequest.current !== requestID) return;
      setVideos(data.items || []);
      setVideosState("ready");
    } catch (error) {
      if (videosRequest.current !== requestID) return;
      setVideos([]);
      setVideosError(apiErrorMessage(error, "作品加载失败"));
      setVideosState("error");
    }
  }, [userID]);

  useEffect(() => {
    void loadVideos();
    return () => {
      videosRequest.current += 1;
    };
  }, [loadVideos]);

  useEffect(() => {
    const requestID = followingRequest.current + 1;
    followingRequest.current = requestID;
    setFollowing(false);
    if (!session.token) return undefined;
    fetchFollowState(session.token, userID)
      .then((state) => {
        if (followingRequest.current === requestID) setFollowing(state.following);
      })
      .catch(() => {
        if (followingRequest.current === requestID) setFollowing(false);
      });
    return () => {
      if (followingRequest.current === requestID) followingRequest.current += 1;
    };
  }, [session.token, userID]);

  const loadLikes = useCallback(
    async (reset = false) => {
      if (!profile?.liked_videos_public) return;
      const requestID = likesRequest.current + 1;
      likesRequest.current = requestID;
      const shouldReset = reset || likes.state === "idle";
      const cursor = shouldReset ? "" : likes.cursor;
      setLikes((state) => ({ ...state, state: shouldReset ? "loading" : "loadingMore", error: "" }));
      try {
        const data = await fetchPublicLikedVideos(userID, cursor);
        if (likesRequest.current !== requestID) return;
        setLikes((state) => ({
          items: shouldReset ? data.items || [] : [...state.items, ...(data.items || [])],
          cursor: data.next_cursor || "",
          hasMore: Boolean(data.has_more && data.next_cursor),
          state: "ready",
          error: ""
        }));
      } catch (error) {
        if (likesRequest.current !== requestID) return;
        setLikes((state) => ({ ...state, state: "error", error: apiErrorMessage(error, "喜欢列表加载失败") }));
      }
    },
    [likes.cursor, likes.state, profile?.liked_videos_public, userID]
  );

  const loadCollections = useCallback(
    async (reset = false) => {
      const requestID = collectionsRequest.current + 1;
      collectionsRequest.current = requestID;
      const shouldReset = reset || collections.state === "idle";
      const cursor = shouldReset ? "" : collections.cursor;
      setCollections((state) => ({ ...state, state: shouldReset ? "loading" : "loadingMore", error: "" }));
      try {
        const data = await fetchPublicCollections(userID, cursor);
        if (collectionsRequest.current !== requestID) return;
        setCollections((state) => ({
          items: shouldReset ? data.items || [] : [...state.items, ...(data.items || [])],
          cursor: data.next_cursor || "",
          hasMore: Boolean(data.has_more && data.next_cursor),
          state: "ready",
          error: ""
        }));
      } catch (error) {
        if (collectionsRequest.current !== requestID) return;
        setCollections((state) => ({ ...state, state: "error", error: apiErrorMessage(error, "合集加载失败") }));
      }
    },
    [collections.cursor, collections.state, userID]
  );

  useEffect(() => {
    if (activeTab === "likes" && likes.state === "idle") void loadLikes(true);
    if (activeTab === "collections" && collections.state === "idle") void loadCollections(true);
  }, [activeTab, collections.state, likes.state, loadCollections, loadLikes]);

  useEffect(() => {
    if (activeTab === "likes" && profile && !profile.liked_videos_public) setActiveTab("works");
  }, [activeTab, profile]);

  async function toggleFollow() {
    if (!session.token) {
      navigate("/auth");
      return;
    }
    const requestID = followingRequest.current + 1;
    followingRequest.current = requestID;
    setFollowBusy(true);
    try {
      const data = await followUser(session.token, userID, !following, "web-public-profile-follow");
      if (followingRequest.current !== requestID) return;
      setFollowing(data.following);
      updateSessionRelationCount(session, data.following_count);
      setProfile((current) => current ? { ...current, follower_count: data.follower_count } : current);
    } finally {
      if (followingRequest.current === requestID) setFollowBusy(false);
    }
  }

  const display = profile || {
    id: userID,
    account: cached?.account || String(userID),
    nickname: cached?.nickname || "",
    avatar_url: cached?.avatar_url || image.currentUser,
    bio: cached?.bio || "",
    following_count: cached?.following_count || 0,
    follower_count: cached?.follower_count || 0,
    work_count: cached?.work_count || 0,
    gender: cached?.gender || 0,
    public_work_count: cached?.public_work_count || cached?.work_count || 0,
    received_like_count: cached?.received_like_count || 0,
    collection_count: cached?.collection_count || 0,
    liked_videos_public: Boolean(cached?.liked_videos_public)
  };

  const tabs = [
    { id: "works", label: "作品", count: display.public_work_count },
    ...(display.liked_videos_public ? [{ id: "likes" as const, label: "喜欢" }] : []),
    { id: "collections", label: "合集", count: display.collection_count }
  ] satisfies Array<{ id: PublicProfileTab; label: string; count?: number }>;

  return (
    <main className="profile-page" data-ui="profile-page">
      <ProfileHero
        profile={{
          account: display.account,
          nickname: display.nickname,
          avatarURL: display.avatar_url,
          bio: display.bio,
          gender: display.gender,
          followingCount: display.following_count,
          followerCount: display.follower_count,
          workCount: display.public_work_count,
          receivedLikeCount: display.received_like_count
        }}
        actions={
          session.user?.id === userID ? (
            <button className="profile-secondary-action" type="button" onClick={() => navigate("/profile")}>
              查看我的主页
            </button>
          ) : (
            <button className={following ? "profile-secondary-action" : "profile-follow-action"} disabled={followBusy} type="button" onClick={() => void toggleFollow()}>
              {followBusy ? "处理中" : following ? "已关注" : "关注"}
            </button>
          )
        }
      />
      <section className="profile-content">
        {profileState === "error" && <p className="profile-inline-error">{profileError}</p>}
        <ProfilePrimaryTabs active={activeTab} tabs={tabs} onChange={setActiveTab} />
        <section
          id={`profile-panel-${activeTab}`}
          aria-labelledby={`profile-tab-${activeTab}`}
          className="profile-tab-panel"
          role="tabpanel"
          tabIndex={0}
        >
          {activeTab === "works" && (
            <ProfileVideoGrid
              emptyTitle="暂无公开作品"
              error={videosError}
              items={videos.map((video) => ({ video }))}
              state={videosState}
              onRetry={() => void loadVideos()}
              onSelect={setSelectedWork}
            />
          )}
          {activeTab === "likes" && display.liked_videos_public && (
            <ProfileVideoGrid
              emptyTitle="暂无公开喜欢作品"
              error={likes.error}
              hasMore={likes.hasMore}
              items={likes.items}
              state={likes.state}
              onLoadMore={() => void loadLikes(false)}
              onRetry={() => void loadLikes(true)}
              onSelect={setSelectedWork}
            />
          )}
          {activeTab === "collections" && (
            <ProfileCollectionGrid
              collections={collections.items}
              error={collections.error}
              hasMore={collections.hasMore}
              state={collections.state}
              onLoadMore={() => void loadCollections(false)}
              onOpenVideo={setSelectedWork}
              onRetry={() => void loadCollections(true)}
            />
          )}
          {profileState === "loading" && activeTab !== "works" && (
            <ProfileEmptyState title="正在加载资料" />
          )}
        </section>
      </section>
      {selectedWork && <WorkViewer video={selectedWork} onClose={() => setSelectedWork(null)} />}
    </main>
  );
}
