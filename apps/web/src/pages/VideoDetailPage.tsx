import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { apiErrorMessage } from "../api/client";
import { fetchPublicProfile, fetchVideo } from "../api/account";
import { ThreadedComments } from "../components/ThreadedComments";
import { VideoDetails } from "../components/VideoDetails";
import { PrivateShareDialog } from "../components/PrivateShareDialog";
import { VideoStage } from "../components/VideoStage";
import { PageMessage } from "../components/StatusMessages";
import { emptyProfile, image } from "../constants";
import { useComments } from "../hooks/useComments";
import { usePlayerPreferences } from "../hooks/usePlayerPreferences";
import { useNavigate } from "../router";
import { useSession } from "../session";
import type { FeedVideo, PublicUserProfile, Video } from "../types";
import { openPublicProfile, publicUserAvatar } from "../utils";

interface VideoDetailPageProps {
  videoID: number;
  commentID: number;
  highlightID: number;
  invalidFocus: boolean;
}

type DetailState = "loading" | "ready" | "unavailable";

export function VideoDetailPage({
  videoID,
  commentID,
  highlightID,
  invalidFocus
}: VideoDetailPageProps) {
  const session = useSession();
  const navigate = useNavigate();
  const discussionRef = useRef<HTMLElement | null>(null);
  const [item, setItem] = useState<FeedVideo | null>(null);
  const [state, setState] = useState<DetailState>("loading");
  const [error, setError] = useState("");
  const [shareOpen, setShareOpen] = useState(false);
  const { preferences, updatePreferences } = usePlayerPreferences();

  const loadVideo = useCallback(() => {
    if (videoID <= 0) {
      setState("unavailable");
      return;
    }
    let live = true;
    setState("loading");
    setError("");
    fetchVideo(videoID)
      .then(async (video) => {
        let profile: PublicUserProfile | null = null;
        try {
          profile = await fetchPublicProfile(video.author_id);
        } catch {
          // Public profile enrichment is optional for video playback.
        }
        if (!live) return;
        setItem(mapVideoDetail(video, profile));
        setState("ready");
      })
      .catch((loadError: unknown) => {
        if (!live) return;
        setItem(null);
        setError(apiErrorMessage(loadError, "视频或讨论已不可用"));
        setState("unavailable");
      });
    return () => {
      live = false;
    };
  }, [videoID]);

  useEffect(() => loadVideo(), [loadVideo]);

  const handleCommentCountChange = useCallback((count: number) => {
    setItem((current) => current ? { ...current, comment_count: count } : current);
  }, []);

  const comments = useComments({
    videoID,
    enabled: state === "ready",
    focusedCommentID: invalidFocus ? 0 : commentID,
    focusedTargetID: invalidFocus ? 0 : highlightID,
    onCommentCountChange: handleCommentCountChange
  });

  const openDiscussion = useCallback(() => {
    discussionRef.current?.scrollIntoView({ block: "start", behavior: "smooth" });
  }, []);

  const stage = useMemo(() => item ? (
    <VideoStage
      item={item}
      active
      showSocialActions={false}
      liked={false}
      favorited={false}
      following={false}
      followBusy={false}
      ownVideo={item.author_id === session.user?.id}
      followError=""
      onLike={() => {}}
      onComment={openDiscussion}
      onFavorite={() => {}}
      onFollow={() => {}}
      onOpenAuthor={(profile) => openPublicProfile(profile, navigate)}
      playerPreferences={preferences}
      onUpdatePlayerPreferences={updatePreferences}
      onContinuousAdvance={() => {}}
    />
  ) : null, [
    item,
    navigate,
    openDiscussion,
    preferences,
    session.user?.id,
    updatePreferences
  ]);

  if (state === "loading") {
    return <main className="video-detail-page"><PageMessage icon="hourglass" title="正在加载视频" /></main>;
  }
  if (state === "unavailable" || !item) {
    return (
      <main className="video-detail-page">
        <PageMessage icon="alert" title={error || "视频或讨论已不可用"} action="返回视频流" onAction={() => navigate("/timeline")} />
      </main>
    );
  }

  return (
    <main className="video-detail-page" data-ui="video-detail-page">
      <section className="video-detail-stage">{stage}</section>
      <section ref={discussionRef} className="video-detail-discussion" aria-label="视频讨论">
        <VideoDetails
          item={item}
          onOpenUser={(profile) => openPublicProfile(profile, navigate)}
          onShare={() => {
            if (!session.token) {
              navigate("/auth");
              return;
            }
            setShareOpen(true);
          }}
        />
        {invalidFocus && (
          <p className="video-detail-unavailable" role="status">评论链接参数无效，已显示可用讨论。</p>
        )}
        <ThreadedComments
          controller={comments}
          authenticated={Boolean(session.token && session.user)}
          user={session.user || emptyProfile}
          canModerateThreads={Boolean(
            session.user &&
            (session.user.id === item.author_id || session.user.role === "admin")
          )}
          onOpenUser={(profile) => openPublicProfile(profile, navigate)}
        />
      </section>
      {shareOpen && <PrivateShareDialog video={item} onClose={() => setShareOpen(false)} />}
    </main>
  );
}

function mapVideoDetail(video: Video, profile: PublicUserProfile | null): FeedVideo {
  return {
    video_id: video.id,
    author_id: video.author_id,
    title: video.title,
    media_url: video.media_url,
    cover_url: video.cover_url || image.stage,
    like_count: video.like_count,
    comment_count: video.comment_count,
    favorite_count: video.favorite_count,
    liked: false,
    favorited: false,
    author: profile?.nickname || `创作者_${video.author_id}`,
    avatar_url: publicUserAvatar(profile?.avatar_url),
    description: video.description || "",
    feed_scene: "video_detail",
    request_id: `video-detail-${video.id}`,
    media_status: video.media_status,
    playback_sources: video.playback_sources
  };
}
