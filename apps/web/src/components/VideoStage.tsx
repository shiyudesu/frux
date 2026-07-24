import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from "react";
import { image } from "../constants";
import type { FeedVideo } from "../types";
import type { PlaybackQoSMetrics, PublicProfileInput, VideoQoSState } from "../utils";
import { createVideoQoSState, isVideoSource } from "../utils";
import { FeedActionRail } from "./FeedActionRail";
import { FeedMetadata } from "./FeedMetadata";
import { FeedPlayerControls } from "./FeedPlayerControls";

export interface VideoStageHandle {
  togglePlayback: () => void;
}

export interface VideoStageProps {
  item: FeedVideo;
  active: boolean;
  liked: boolean;
  favorited: boolean;
  following: boolean;
  followBusy: boolean;
  ownVideo: boolean;
  followError: string;
  commentButtonRef?: React.Ref<HTMLButtonElement>;
  onLike: () => void;
  onComment: () => void;
  onFavorite: () => void;
  onFollow: () => void;
  onPlaybackQoS?: (item: FeedVideo, metrics: PlaybackQoSMetrics) => void;
  onOpenAuthor: (profile: PublicProfileInput) => void;
}

export const VideoStage = forwardRef<VideoStageHandle, VideoStageProps>(function VideoStage(
  {
    item,
    active,
    liked,
    favorited,
    following,
    followBusy,
    ownVideo,
    followError,
    commentButtonRef,
    onLike,
    onComment,
    onFavorite,
    onFollow,
    onPlaybackQoS,
    onOpenAuthor
  },
  ref
) {
  const cover = item.cover_url || image.stage;
  const media = item.media_url || cover;
  const showVideo = isVideoSource(media);
  const stageRef = useRef<HTMLElement | null>(null);
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const itemRef = useRef(item);
  const qosRef = useRef<VideoQoSState>(createVideoQoSState(item.video_id));
  const [playing, setPlaying] = useState(false);
  const [muted, setMuted] = useState(true);
  const [currentTime, setCurrentTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [fullscreen, setFullscreen] = useState(false);
  const [playbackError, setPlaybackError] = useState("");

  useEffect(() => {
    itemRef.current = item;
    setCurrentTime(0);
    setDuration(0);
    setPlaybackError("");
  }, [item]);

  const flushQoS = useCallback(() => {
    const state = qosRef.current;
    if (!state || !state.playingStartedAt) return;
    const watchMs = Math.max(0, Math.round(performance.now() - state.playingStartedAt));
    if (watchMs <= 0 && state.stutterCount <= 0 && state.firstFrameMs === undefined) return;
    onPlaybackQoS?.(itemRef.current, {
      firstFrameMs: state.firstFrameMs,
      stutterCount: state.stutterCount,
      watchMs,
      reportID: state.reportID
    });
    qosRef.current = {
      ...createVideoQoSState(itemRef.current.video_id),
      loadStartedAt: performance.now()
    };
  }, [onPlaybackQoS]);

  const playVideo = useCallback(() => {
    const video = videoRef.current;
    if (!video || !showVideo) return;
    video.play().catch(() => {
      setPlaybackError("浏览器暂时无法播放该视频");
      setPlaying(false);
    });
  }, [showVideo]);

  const togglePlayback = useCallback(() => {
    const video = videoRef.current;
    if (!video || !showVideo) return;
    setPlaybackError("");
    if (video.paused) {
      playVideo();
    } else {
      video.pause();
    }
  }, [playVideo, showVideo]);

  useImperativeHandle(ref, () => ({ togglePlayback }), [togglePlayback]);

  useEffect(() => {
    qosRef.current = createVideoQoSState(item.video_id);
    if (active && showVideo) {
      qosRef.current.loadStartedAt = performance.now();
    }
    return () => {
      flushQoS();
    };
  }, [active, flushQoS, item.video_id, showVideo]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !showVideo) return;
    if (active) {
      playVideo();
      return;
    }
    video.pause();
  }, [active, media, playVideo, showVideo]);

  useEffect(() => {
    const handleFullscreenChange = () => {
      setFullscreen(document.fullscreenElement === stageRef.current);
    };
    document.addEventListener("fullscreenchange", handleFullscreenChange);
    return () => document.removeEventListener("fullscreenchange", handleFullscreenChange);
  }, []);

  function handleLoadedData() {
    const state = qosRef.current;
    if (!active || !state || state.firstFrameMs !== undefined) return;
    const startedAt = state.loadStartedAt || performance.now();
    state.firstFrameMs = Math.max(0, Math.round(performance.now() - startedAt));
  }

  function handleLoadedMetadata() {
    const video = videoRef.current;
    if (!video) return;
    setDuration(Number.isFinite(video.duration) ? video.duration : 0);
    setCurrentTime(video.currentTime || 0);
  }

  function handlePlaying() {
    const state = qosRef.current;
    setPlaying(true);
    setPlaybackError("");
    if (!active || !state) return;
    if (!state.loadStartedAt) {
      state.loadStartedAt = performance.now();
    }
    if (!state.playingStartedAt) {
      state.playingStartedAt = performance.now();
    }
  }

  function handlePause() {
    setPlaying(false);
    flushQoS();
  }

  function handleWaiting() {
    const state = qosRef.current;
    if (!active || !state) return;
    state.stutterCount += 1;
  }

  function handleTimeUpdate() {
    const video = videoRef.current;
    if (!video) return;
    setCurrentTime(video.currentTime || 0);
  }

  function handleSeek(value: number) {
    const video = videoRef.current;
    if (!video || !Number.isFinite(value)) return;
    video.currentTime = Math.max(0, Math.min(value, video.duration || value));
    setCurrentTime(video.currentTime);
  }

  function handleToggleMute() {
    const video = videoRef.current;
    if (!video) return;
    video.muted = !video.muted;
    setMuted(video.muted);
  }

  function handleToggleFullscreen() {
    const stage = stageRef.current;
    if (!stage) return;
    setPlaybackError("");
    if (document.fullscreenElement === stage) {
      document.exitFullscreen().catch(() => setPlaybackError("无法退出全屏"));
      return;
    }
    stage.requestFullscreen().catch(() => setPlaybackError("当前浏览器无法进入全屏"));
  }

  return (
    <article className="video-stage" data-ui="feed-stage" ref={stageRef}>
      <img className="stage-backdrop" src={cover} alt="" />
      <div className="stage-vignette" />
      {showVideo ? (
        <video
          ref={videoRef}
          className="stage-media"
          src={media}
          poster={cover}
          autoPlay={active}
          muted={muted}
          loop
          playsInline
          preload={active ? "metadata" : "none"}
          onClick={togglePlayback}
          onDurationChange={handleLoadedMetadata}
          onLoadedData={handleLoadedData}
          onLoadedMetadata={handleLoadedMetadata}
          onPause={handlePause}
          onPlaying={handlePlaying}
          onTimeUpdate={handleTimeUpdate}
          onVolumeChange={(event) => setMuted(event.currentTarget.muted)}
          onWaiting={handleWaiting}
          onEnded={flushQoS}
        />
      ) : (
        <img className="stage-media portrait-media" src={media} alt="" />
      )}
      <FeedMetadata item={item} followError={followError || playbackError} onOpenAuthor={onOpenAuthor} />
      <FeedActionRail
        item={item}
        liked={liked}
        favorited={favorited}
        following={following}
        followBusy={followBusy}
        ownVideo={ownVideo}
        commentButtonRef={commentButtonRef}
        onLike={onLike}
        onComment={onComment}
        onFavorite={onFavorite}
        onFollow={onFollow}
        onOpenAuthor={onOpenAuthor}
      />
      {showVideo && (
        <FeedPlayerControls
          playing={playing}
          muted={muted}
          currentTime={currentTime}
          duration={duration}
          fullscreen={fullscreen}
          onTogglePlayback={togglePlayback}
          onToggleMute={handleToggleMute}
          onSeek={handleSeek}
          onToggleFullscreen={handleToggleFullscreen}
        />
      )}
    </article>
  );
});
