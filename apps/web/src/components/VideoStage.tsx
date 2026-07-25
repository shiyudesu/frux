import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from "react";
import { image } from "../constants";
import { PlaybackLifecycle } from "../playbackLifecycle";
import type { CreateViewEventRequest, FeedVideo } from "../types";
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
  onViewEvent?: (event: CreateViewEventRequest, keepalive?: boolean) => void;
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
    onViewEvent,
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
  const lifecycleRef = useRef<PlaybackLifecycle | null>(null);
  const onViewEventRef = useRef(onViewEvent);
  const completionPollRef = useRef<number | null>(null);
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

  useEffect(() => {
    onViewEventRef.current = onViewEvent;
  }, [onViewEvent]);

  const emitViewEvents = useCallback((events: CreateViewEventRequest[], keepalive = false) => {
    for (const event of events) {
      onViewEventRef.current?.(event, keepalive);
    }
  }, []);

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
    const lifecycle = new PlaybackLifecycle(item);
    lifecycleRef.current = lifecycle;
    if (document.visibilityState === "hidden") {
      lifecycle.setVisibility(performance.now(), false, 0, 0);
    }
    return () => {
      stopCompletionPoll();
      emitViewEvents(lifecycle.finish(performance.now()), true);
      if (lifecycleRef.current === lifecycle) {
        lifecycleRef.current = null;
      }
    };
  }, [emitViewEvents, item.feed_scene, item.request_id, item.video_id]);

  useEffect(() => {
    if (!active || !showVideo) return undefined;
    const lifecycle = lifecycleRef.current;
    const timer = window.setTimeout(() => {
      if (lifecycleRef.current === lifecycle && lifecycle) {
        const video = videoRef.current;
        const now = performance.now();
        const events = lifecycle.activate(now);
        if (video && !video.paused && !video.ended) {
          events.push(...lifecycle.playing(now, video.currentTime, video.duration));
        }
        emitViewEvents(events);
      }
    }, 0);
    return () => window.clearTimeout(timer);
  }, [active, emitViewEvents, item.feed_scene, item.request_id, item.video_id, showVideo]);

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

  useEffect(() => {
    if (!showVideo) return undefined;
    const handleVisibilityChange = () => {
      const video = videoRef.current;
      const lifecycle = lifecycleRef.current;
      if (!lifecycle) return;
      emitViewEvents(
        lifecycle.setVisibility(
          performance.now(),
          document.visibilityState === "visible",
          video?.currentTime || 0,
          video?.duration || 0
        ),
        document.visibilityState !== "visible"
      );
    };
    const handlePageHide = (event: PageTransitionEvent) => {
      const video = videoRef.current;
      const lifecycle = lifecycleRef.current;
      if (!lifecycle) return;
      const events = event.persisted
        ? lifecycle.flush(performance.now(), video?.currentTime || 0, video?.duration || 0)
        : lifecycle.finish(performance.now(), video?.currentTime || 0, video?.duration || 0);
      emitViewEvents(events, true);
    };
    const handlePageShow = (event: PageTransitionEvent) => {
      if (!event.persisted) return;
      const video = videoRef.current;
      const lifecycle = lifecycleRef.current;
      if (!lifecycle) return;
      const now = performance.now();
      emitViewEvents(lifecycle.setVisibility(now, true, video?.currentTime || 0, video?.duration || 0));
      if (video && !video.paused && !video.ended) {
        emitViewEvents(lifecycle.playing(now, video.currentTime, video.duration));
      }
    };
    document.addEventListener("visibilitychange", handleVisibilityChange);
    window.addEventListener("pagehide", handlePageHide);
    window.addEventListener("pageshow", handlePageShow);
    return () => {
      document.removeEventListener("visibilitychange", handleVisibilityChange);
      window.removeEventListener("pagehide", handlePageHide);
      window.removeEventListener("pageshow", handlePageShow);
    };
  }, [emitViewEvents, showVideo]);

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
    const video = videoRef.current;
    setPlaying(true);
    setPlaybackError("");
    startCompletionPoll();
    if (video) {
      emitViewEvents(lifecycleRef.current?.playing(performance.now(), video.currentTime, video.duration) || []);
    }
    if (!active || !state) return;
    if (!state.loadStartedAt) {
      state.loadStartedAt = performance.now();
    }
    if (!state.playingStartedAt) {
      state.playingStartedAt = performance.now();
    }
  }

  function handlePause() {
    const video = videoRef.current;
    setPlaying(false);
    stopCompletionPoll();
    if (video) {
      emitViewEvents(lifecycleRef.current?.pause(performance.now(), video.currentTime, video.duration) || []);
    }
    flushQoS();
  }

  function handleWaiting() {
    const state = qosRef.current;
    const video = videoRef.current;
    stopCompletionPoll();
    if (video) {
      lifecycleRef.current?.waiting(performance.now(), video.currentTime, video.duration);
    }
    if (!active || !state) return;
    state.stutterCount += 1;
  }

  function handleTimeUpdate() {
    const video = videoRef.current;
    if (!video) return;
    setCurrentTime(video.currentTime || 0);
    emitViewEvents(lifecycleRef.current?.timeUpdate(performance.now(), video.currentTime, video.duration) || []);
  }

  function handleSeek(value: number) {
    const video = videoRef.current;
    if (!video || !Number.isFinite(value)) return;
    video.currentTime = Math.max(0, Math.min(value, video.duration || value));
    setCurrentTime(video.currentTime);
  }

  function handleSeeking() {
    const video = videoRef.current;
    if (!video) return;
    lifecycleRef.current?.seeking(performance.now(), video.currentTime, video.duration);
  }

  function handleSeeked() {
    const video = videoRef.current;
    if (!video) return;
    emitViewEvents(
      lifecycleRef.current?.seeked(
        performance.now(),
        video.currentTime,
        video.duration,
        !video.paused && !video.ended
      ) || []
    );
  }

  function handleEnded() {
    const video = videoRef.current;
    stopCompletionPoll();
    if (video) {
      emitViewEvents(lifecycleRef.current?.timeUpdate(performance.now(), video.duration, video.duration) || []);
    }
    flushQoS();
  }

  function startCompletionPoll() {
    stopCompletionPoll();
    if (!active) return;
    completionPollRef.current = window.setInterval(() => {
      const video = videoRef.current;
      if (!video || video.paused || video.ended) return;
      emitViewEvents(lifecycleRef.current?.timeUpdate(performance.now(), video.currentTime, video.duration) || []);
    }, 250);
  }

  function stopCompletionPoll() {
    if (completionPollRef.current === null) return;
    window.clearInterval(completionPollRef.current);
    completionPollRef.current = null;
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
          onSeeked={handleSeeked}
          onSeeking={handleSeeking}
          onTimeUpdate={handleTimeUpdate}
          onVolumeChange={(event) => setMuted(event.currentTarget.muted)}
          onWaiting={handleWaiting}
          onEnded={handleEnded}
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
