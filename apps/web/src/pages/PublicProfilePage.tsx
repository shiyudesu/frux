import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { fetchPublicProfile, fetchUserVideos } from "../api/account";
import {
  apiErrorMessage,
  currentConsumerSessionEpoch,
  isUnauthorized
} from "../api/client";
import { createChatConversation, fetchChatEligibility } from "../api/chat";
import {
  createChatOperationKey,
  rotateChatOperationKey
} from "../chatOperations";
import { fetchPublicLikedVideos } from "../api/library";
import { fetchFollowState, followUser } from "../api/social";
import {
  ProfileEmptyState,
  ProfileHero,
  ProfilePrimaryTabs,
  ProfileVideoGrid
} from "../components/ProfileDashboard";
import { CollectionQueueViewer } from "../components/CollectionQueueViewer";
import { image } from "../constants";
import { useNavigate } from "../router";
import { updateSessionRelationCount, useSession } from "../session";
import type {
  AsyncState,
  LibraryVideoItem,
  PublicProfileTab,
  PublicUserProfile,
  Video,
  ChatEligibilityResponse
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
  const [videos, setVideos] = useState<CursorState<LibraryVideoItem>>(emptyCursorState);
  const [likes, setLikes] = useState<CursorState<LibraryVideoItem>>(emptyCursorState);
  const [publicQueue, setPublicQueue] = useState<{
    source: "publicWorks" | "publicLikes";
    videoID: number;
  } | null>(null);
  const [following, setFollowing] = useState(false);
  const [followBusy, setFollowBusy] = useState(false);
  const [chatEligibility, setChatEligibility] = useState<ChatEligibilityResponse | null>(null);
  const [chatEligibilityState, setChatEligibilityState] = useState<"idle" | "loading" | "ready" | "error">("idle");
  const [chatEligibilityError, setChatEligibilityError] = useState("");
  const [chatBusy, setChatBusy] = useState(false);
  const profileRequest = useRef(0);
  const videosRequest = useRef(0);
  const videosRef = useRef(videos);
  videosRef.current = videos;
  const followingRequest = useRef(0);
  const likesRequest = useRef(0);
  const chatRequest = useRef(0);
  const chatActionRequest = useRef(0);

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

  const loadVideos = useCallback(async (reset = false) => {
    const current = videosRef.current;
    if (!reset && (
      current.state === "loading"
      || current.state === "loadingMore"
      || !current.hasMore
    )) return;
    const shouldReset = reset || current.state === "idle";
    const offset = shouldReset ? 0 : Math.max(0, Number.parseInt(current.cursor, 10) || 0);
    const requestID = videosRequest.current + 1;
    videosRequest.current = requestID;
    setVideos((state) => ({
      ...state,
      items: shouldReset ? [] : state.items,
      state: shouldReset ? "loading" : "loadingMore",
      error: ""
    }));
    try {
      const data = await fetchUserVideos(userID, 24, offset);
      if (videosRequest.current !== requestID) return;
      const incoming = (data.items || []).map((video) => ({
        video,
        updated_at: video.published_at || video.updated_at || video.created_at
      }));
      setVideos((state) => ({
        items: appendUniqueVideos(shouldReset ? [] : state.items, incoming),
        cursor: String((data.offset || offset) + incoming.length),
        hasMore: incoming.length > 0 && incoming.length >= (data.limit || 24),
        state: "ready",
        error: ""
      }));
    } catch (error) {
      if (videosRequest.current !== requestID) return;
      setVideos((state) => ({
        ...state,
        items: shouldReset ? [] : state.items,
        state: "error",
        error: apiErrorMessage(error, "作品加载失败")
      }));
    }
  }, [userID]);

  useEffect(() => {
    setVideos(emptyCursorState());
    void loadVideos(true);
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

  useEffect(() => {
    const requestID = chatRequest.current + 1;
    chatRequest.current = requestID;
    setChatEligibility(null);
    setChatEligibilityError("");
    if (!session.token || session.user?.id === userID) {
      setChatEligibilityState("idle");
      return undefined;
    }
    setChatEligibilityState("loading");
    fetchChatEligibility(session.token, userID)
      .then((data) => {
        if (chatRequest.current !== requestID) return;
        setChatEligibility(data);
        setChatEligibilityState("ready");
      })
      .catch((error: unknown) => {
        if (chatRequest.current !== requestID) return;
        if (isUnauthorized(error)) {
          session.clearAuth();
          navigate("/auth");
          return;
        }
        setChatEligibilityError(apiErrorMessage(error, "私信权限加载失败"));
        setChatEligibilityState("error");
      });
    return () => {
      if (chatRequest.current === requestID) chatRequest.current += 1;
    };
  }, [navigate, session, session.token, session.user?.id, userID]);

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

  useEffect(() => {
    if (activeTab === "likes" && likes.state === "idle") void loadLikes(true);
  }, [activeTab, likes.state, loadLikes]);

  useEffect(() => {
    if (activeTab === "likes" && profile && !profile.liked_videos_public) setActiveTab("works");
  }, [activeTab, profile]);

  const patchPublicQueueVideo = useCallback((videoID: number, patch: Partial<Video>) => {
    setVideos((state) => patchCursorVideo(state, videoID, patch));
    setLikes((state) => patchCursorVideo(state, videoID, patch));
  }, []);

  const applyPublicQueueVideoAction = useCallback((
    videoID: number,
    action: "like" | "favorite",
    active: boolean,
    counts: Partial<Pick<Video, "like_count" | "favorite_count">>
  ) => {
    patchPublicQueueVideo(videoID, {
      ...counts,
      ...(action === "like" ? { liked: active } : { favorited: active })
    });
  }, [patchPublicQueueVideo]);

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

  async function openPrivateConversation() {
    if (!session.token || !chatEligibility?.eligible || chatBusy) return;
    setChatBusy(true);
    setChatEligibilityError("");
    const requestID = chatActionRequest.current + 1;
    chatActionRequest.current = requestID;
    const requestToken = session.token;
    const requestUserID = session.user?.id || 0;
    const requestEpoch = currentConsumerSessionEpoch();
    const identity = `${requestEpoch}:${requestUserID}:${userID}`;
    const key = createChatOperationKey("conversation", identity);
    const isCurrent = () => (
      chatActionRequest.current === requestID
      && session.token === requestToken
      && session.user?.id === requestUserID
      && currentConsumerSessionEpoch() === requestEpoch
    );
    try {
      const conversation = await createChatConversation(requestToken, userID, key);
      rotateChatOperationKey("conversation", identity);
      if (!isCurrent()) return;
      navigate({ route: `/messages/${conversation.conversation_id}` });
    } catch (error) {
      if (!isCurrent()) return;
      setChatEligibilityError(apiErrorMessage(error, "私信会话创建失败"));
      if (error instanceof Error && "code" in error && error.code === "CHAT_NOT_ELIGIBLE") {
        setChatEligibility(null);
        setChatEligibilityState("loading");
        const requestID = chatRequest.current + 1;
        chatRequest.current = requestID;
        fetchChatEligibility(requestToken, userID)
          .then((data) => {
            if (chatRequest.current !== requestID) return;
            setChatEligibility(data);
            setChatEligibilityState("ready");
          })
          .catch((reloadError: unknown) => {
            if (chatRequest.current !== requestID) return;
            if (isUnauthorized(reloadError)) {
              session.clearAuth();
              navigate("/auth");
              return;
            }
            setChatEligibilityError(apiErrorMessage(reloadError, "私信权限加载失败"));
            setChatEligibilityState("error");
          });
      }
    } finally {
      if (isCurrent()) setChatBusy(false);
    }
  }

  const display = profile || {
    id: userID,
    nickname: cached?.nickname || `用户_${userID}`,
    avatar_url: cached?.avatar_url || image.currentUser,
    bio: cached?.bio || "",
    following_count: cached?.following_count || 0,
    follower_count: cached?.follower_count || 0,
    work_count: cached?.work_count || 0,
    gender: cached?.gender || 0,
    public_work_count: cached?.public_work_count || cached?.work_count || 0,
    received_like_count: cached?.received_like_count || 0,
    liked_videos_public: Boolean(cached?.liked_videos_public)
  };

  const tabs = [
    { id: "works", label: "作品", count: display.public_work_count },
    ...(display.liked_videos_public ? [{ id: "likes" as const, label: "喜欢" }] : [])
  ] satisfies Array<{ id: PublicProfileTab; label: string; count?: number }>;

  return (
    <main className="profile-page" data-ui="profile-page">
      <ProfileHero
        profile={{
          id: display.id,
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
            <div className="public-profile-actions">
              <button className={following ? "profile-secondary-action" : "profile-follow-action"} disabled={followBusy} type="button" onClick={() => void toggleFollow()}>
                {followBusy ? "处理中" : following ? "已关注" : "关注"}
              </button>
              {session.token && (
                <>
                  {chatEligibilityState === "loading" && (
                    <button className="profile-secondary-action" disabled type="button">检查私信权限…</button>
                  )}
                  {chatEligibilityState === "ready" && chatEligibility?.eligible && (
                    <button className="profile-message-action" disabled={chatBusy} type="button" onClick={() => void openPrivateConversation()}>
                      {chatBusy ? "打开中…" : "私信"}
                    </button>
                  )}
                  {chatEligibilityState === "ready" && chatEligibility && !chatEligibility.eligible && (
                    <span className="profile-message-hint" role="status">需要互相关注后才能私信</span>
                  )}
                  {chatEligibilityState === "error" && (
                    <span className="profile-message-hint" role="status">{chatEligibilityError || "暂时无法确认私信权限"}</span>
                  )}
                </>
              )}
            </div>
          )
        }
      />
      <section className="profile-content">
        {profileState === "error" && <p className="profile-inline-error">{profileError}</p>}
        {chatEligibilityError && chatEligibilityState === "ready" && (
          <p className="profile-inline-error">{chatEligibilityError}</p>
        )}
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
              error={videos.error}
              hasMore={videos.hasMore}
              items={videos.items}
              state={videos.state}
              onLoadMore={() => void loadVideos(false)}
              onRetry={() => void loadVideos(true)}
              onSelect={(video) => setPublicQueue({ source: "publicWorks", videoID: video.id })}
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
              onSelect={(video) => setPublicQueue({ source: "publicLikes", videoID: video.id })}
            />
          )}
          {profileState === "loading" && activeTab !== "works" && (
            <ProfileEmptyState title="正在加载资料" />
          )}
        </section>
      </section>
      {publicQueue && (
        <CollectionQueueViewer
          source={publicQueue.source}
          sourceState={publicQueueState(
            publicQueue.source === "publicWorks" ? videos : likes,
            publicQueue.source === "publicWorks" ? display : null
          )}
          selectedVideoID={publicQueue.videoID}
          onClose={() => setPublicQueue(null)}
          onLoadMore={() => {
            if (publicQueue.source === "publicWorks") void loadVideos(false);
            else void loadLikes(false);
          }}
          onPatchVideo={patchPublicQueueVideo}
          onApplyVideoAction={applyPublicQueueVideoAction}
        />
      )}
    </main>
  );
}

function appendUniqueVideos(
  current: LibraryVideoItem[],
  incoming: LibraryVideoItem[]
): LibraryVideoItem[] {
  const seen = new Set(current.map((item) => item.video.id));
  return [
    ...current,
    ...incoming.filter((item) => {
      if (seen.has(item.video.id)) return false;
      seen.add(item.video.id);
      return true;
    })
  ];
}

function patchCursorVideo(
  state: CursorState<LibraryVideoItem>,
  videoID: number,
  patch: Partial<Video>
): CursorState<LibraryVideoItem> {
  return {
    ...state,
    items: state.items.map((item) => item.video.id === videoID
      ? { ...item, video: { ...item.video, ...patch } }
      : item)
  };
}

function publicQueueState(
  state: CursorState<LibraryVideoItem>,
  author: Pick<PublicUserProfile, "nickname" | "avatar_url"> | null
) {
  return {
    items: state.items.map((item) => author
      ? {
          ...item,
          video: {
            ...item.video,
            author_nickname: item.video.author_nickname || author.nickname,
            author_avatar_url: item.video.author_avatar_url || author.avatar_url
          }
        }
      : item),
    nextCursor: state.cursor,
    hasMore: state.hasMore,
    state: state.state,
    error: state.error
  };
}
