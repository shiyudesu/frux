import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from "react";
import { image } from "../constants";
import {
  feedPreloadResourceKey,
  type EffectiveFeedPreloadPolicy,
  type FeedPreloadCandidate,
  type FeedPreloadMediaEvent,
  type FeedPreloadMediaResource
} from "../feedPreload";
import {
  createNativeFeedPreloadMediaResource,
  type FeedPreloadController
} from "../feedPreloadController";
import { PlaybackLifecycle } from "../playbackLifecycle";
import {
  buildPlaybackTelemetryContext,
  playbackErrorCategory,
  playbackTelemetrySource,
  PlaybackTelemetrySession
} from "../playbackTelemetry";
import type { CreateViewEventRequest, FeedVideo, PlaybackTelemetryBatch } from "../types";
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
  telemetryEnabled?: boolean;
  onPlaybackTelemetryBatch?: (batch: PlaybackTelemetryBatch, keepalive: boolean) => Promise<void>;
  onViewEvent?: (event: CreateViewEventRequest, keepalive?: boolean) => void;
  onOpenAuthor: (profile: PublicProfileInput) => void;
  preloadCandidate?: FeedPreloadCandidate;
  preloadController?: FeedPreloadController;
  preloadPolicy?: EffectiveFeedPreloadPolicy;
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
    telemetryEnabled,
    onPlaybackTelemetryBatch,
    onViewEvent,
    onOpenAuthor,
    preloadCandidate,
    preloadController,
    preloadPolicy
  },
  ref
) {
  const cover = item.cover_url || image.stage;
  const media = item.media_url || cover;
  const showVideo = isVideoSource(media);
  const stageRef = useRef<HTMLElement | null>(null);
  const mediaHostRef = useRef<HTMLDivElement | null>(null);
  const mediaRef = useRef<FeedPreloadMediaResource | null>(null);
  const itemRef = useRef(item);
  const activeRef = useRef(active);
  const qosRef = useRef<VideoQoSState>(createVideoQoSState(item.video_id));
  const lifecycleRef = useRef<PlaybackLifecycle | null>(null);
  const telemetryRef = useRef<PlaybackTelemetrySession | null>(null);
  const onViewEventRef = useRef(onViewEvent);
  const completionPollRef = useRef<number | null>(null);
  const [playing, setPlaying] = useState(false);
  const [muted, setMuted] = useState(true);
  const [currentTime, setCurrentTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [fullscreen, setFullscreen] = useState(false);
  const [playbackError, setPlaybackError] = useState("");
  activeRef.current = active;

  useEffect(() => {
    itemRef.current = item;
    setCurrentTime(0);
    setDuration(0);
    setPlaybackError("");
  }, [item]);

  useEffect(() => {
    onViewEventRef.current = onViewEvent;
  }, [onViewEvent]);

  useEffect(() => {
    if (!active || !showVideo || !telemetryEnabled || !onPlaybackTelemetryBatch) return undefined;
    const telemetry = new PlaybackTelemetrySession(buildPlaybackTelemetryContext(item, media), {
      send: (batch, keepalive) => onPlaybackTelemetryBatch(batch, keepalive)
    });
    telemetryRef.current = telemetry;
    const resource = mediaRef.current;
    if (resource) {
      const sourceURL = resource.currentSource?.() || media;
      telemetry.sourceLoadStarted(resource, playbackTelemetrySource(item, sourceURL));
      if (resource.readyState >= 1) telemetry.metadataReady();
    }
    return () => {
      void telemetry.finish(true);
      telemetry.dispose();
      if (telemetryRef.current === telemetry) telemetryRef.current = null;
    };
  }, [
    active,
    item.feed_scene,
    item.playback_sources,
    item.request_id,
    item.video_id,
    media,
    onPlaybackTelemetryBatch,
    showVideo,
    telemetryEnabled
  ]);

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
    const video = mediaRef.current;
    if (!video || !showVideo) return;
    const telemetry = telemetryRef.current;
    safelyRecordTelemetry(telemetry, (session) => session.playAttempted());
    video
      .play()
      .then(() => {
        if (telemetryRef.current === telemetry) {
          safelyRecordTelemetry(telemetry, (session) => session.playSucceededEvent());
        }
      })
      .catch(() => {
        if (telemetryRef.current === telemetry) {
          safelyRecordTelemetry(telemetry, (session) => session.playFailed("autoplay"));
        }
        setPlaybackError("浏览器暂时无法播放该视频");
        setPlaying(false);
      });
  }, [showVideo]);

  const togglePlayback = useCallback(() => {
    const video = mediaRef.current;
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
        const video = mediaRef.current;
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

  const preloadKey = preloadCandidate ? feedPreloadResourceKey(preloadCandidate.key) : "";

  useEffect(() => {
    const host = mediaHostRef.current;
    if (!host || !showVideo) return undefined;

    const acquired =
      preloadCandidate && preloadController && preloadPolicy
        ? preloadController.acquireCandidate(preloadCandidate, preloadPolicy)
        : undefined;
    const resource = acquired?.media || createNativeFeedPreloadMediaResource();
    if (activeRef.current) {
      safelyRecordTelemetry(telemetryRef.current, (telemetry) => {
        telemetry.sourceLoadStarted(resource, playbackTelemetrySource(itemRef.current, media));
      });
    }
    if (!acquired) {
      resource.configure(media, cover, "buffer");
      resource.load();
    }
    resource.muted = muted;
    resource.mount(host, "stage-media");
    mediaRef.current = resource;
    const unsubscribe = resource.subscribe(handleMediaEvent);
    if (resource.readyState >= 1) {
      handleLoadedMetadata();
    }
    if (resource.readyState >= 2) {
      handleLoadedData();
    }

    return () => {
      unsubscribe();
      resource.pause();
      if (mediaRef.current === resource) {
        mediaRef.current = null;
      }
      if (acquired) {
        acquired.release();
      } else {
        resource.destroy();
      }
    };
  }, [cover, item.video_id, media, preloadController, preloadKey, preloadPolicy, showVideo]);

  useEffect(() => {
    const video = mediaRef.current;
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
      const video = mediaRef.current;
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
      if (document.visibilityState !== "visible") {
        void telemetryRef.current?.visibilityHidden();
      }
    };
    const handlePageHide = (event: PageTransitionEvent) => {
      const video = mediaRef.current;
      const lifecycle = lifecycleRef.current;
      if (!lifecycle) return;
      const events = event.persisted
        ? lifecycle.flush(performance.now(), video?.currentTime || 0, video?.duration || 0)
        : lifecycle.finish(performance.now(), video?.currentTime || 0, video?.duration || 0);
      emitViewEvents(events, true);
      if (event.persisted) {
        void telemetryRef.current?.flush(true);
      } else {
        void telemetryRef.current?.pageExit();
      }
    };
    const handlePageShow = (event: PageTransitionEvent) => {
      if (!event.persisted) return;
      const video = mediaRef.current;
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
    if (!activeRef.current || !state || state.firstFrameMs !== undefined) return;
    const startedAt = state.loadStartedAt || performance.now();
    state.firstFrameMs = Math.max(0, Math.round(performance.now() - startedAt));
  }

  function handleLoadedMetadata() {
    const video = mediaRef.current;
    if (!video) return;
    setDuration(Number.isFinite(video.duration) ? video.duration : 0);
    setCurrentTime(video.currentTime || 0);
    safelyRecordTelemetry(telemetryRef.current, (telemetry) => {
      telemetry.metadataReady();
      const source = playbackTelemetrySource(itemRef.current, video.currentSource?.() || media);
      telemetry.sourceChanged(source);
      telemetry.qualityChanged(source.renditionLabel);
    });
  }

  function handlePlaying() {
    const state = qosRef.current;
    const video = mediaRef.current;
    setPlaying(true);
    setPlaybackError("");
    startCompletionPoll();
    if (video) {
      emitViewEvents(lifecycleRef.current?.playing(performance.now(), video.currentTime, video.duration) || []);
    }
    safelyRecordTelemetry(telemetryRef.current, (telemetry) => telemetry.playing());
    if (!activeRef.current || !state) return;
    if (!state.loadStartedAt) {
      state.loadStartedAt = performance.now();
    }
    if (state.firstFrameMs === undefined) {
      state.firstFrameMs = Math.max(0, Math.round(performance.now() - state.loadStartedAt));
    }
    if (!state.playingStartedAt) {
      state.playingStartedAt = performance.now();
    }
  }

  function handlePause() {
    const video = mediaRef.current;
    setPlaying(false);
    stopCompletionPoll();
    if (video) {
      emitViewEvents(lifecycleRef.current?.pause(performance.now(), video.currentTime, video.duration) || []);
    }
    safelyRecordTelemetry(telemetryRef.current, (telemetry) => telemetry.pause());
    flushQoS();
  }

  function handleWaiting() {
    const state = qosRef.current;
    const video = mediaRef.current;
    stopCompletionPoll();
    if (video) {
      lifecycleRef.current?.waiting(performance.now(), video.currentTime, video.duration);
    }
    safelyRecordTelemetry(telemetryRef.current, (telemetry) => telemetry.waiting());
    if (!activeRef.current || !state) return;
    state.stutterCount += 1;
  }

  function handleTimeUpdate() {
    const video = mediaRef.current;
    if (!video) return;
    setCurrentTime(video.currentTime || 0);
    emitViewEvents(lifecycleRef.current?.timeUpdate(performance.now(), video.currentTime, video.duration) || []);
    safelyRecordTelemetry(telemetryRef.current, (telemetry) => telemetry.timeUpdated());
  }

  function handleSeek(value: number) {
    const video = mediaRef.current;
    if (!video || !Number.isFinite(value)) return;
    video.currentTime = Math.max(0, Math.min(value, video.duration || value));
    setCurrentTime(video.currentTime);
  }

  function handleSeeking() {
    const video = mediaRef.current;
    if (!video) return;
    lifecycleRef.current?.seeking(performance.now(), video.currentTime, video.duration);
    safelyRecordTelemetry(telemetryRef.current, (telemetry) => telemetry.seeking());
  }

  function handleSeeked() {
    const video = mediaRef.current;
    if (!video) return;
    emitViewEvents(
      lifecycleRef.current?.seeked(
        performance.now(),
        video.currentTime,
        video.duration,
        !video.paused && !video.ended
      ) || []
    );
    safelyRecordTelemetry(telemetryRef.current, (telemetry) => telemetry.seeked());
  }

  function handleEnded() {
    const video = mediaRef.current;
    stopCompletionPoll();
    if (video) {
      emitViewEvents(lifecycleRef.current?.timeUpdate(performance.now(), video.duration, video.duration) || []);
    }
    safelyRecordTelemetry(telemetryRef.current, (telemetry) => telemetry.ended());
    flushQoS();
  }

  function startCompletionPoll() {
    stopCompletionPoll();
    if (!activeRef.current) return;
    completionPollRef.current = window.setInterval(() => {
      const video = mediaRef.current;
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
    const video = mediaRef.current;
    if (!video) return;
    video.muted = !video.muted;
    setMuted(video.muted);
  }

  function handleMediaEvent(event: FeedPreloadMediaEvent) {
    switch (event) {
      case "durationchange":
      case "loadedmetadata":
        handleLoadedMetadata();
        break;
      case "loadeddata":
        handleLoadedData();
        break;
      case "playing":
        handlePlaying();
        break;
      case "pause":
        handlePause();
        break;
      case "waiting":
      case "stalled":
        handleWaiting();
        break;
      case "timeupdate":
        handleTimeUpdate();
        break;
      case "seeking":
        handleSeeking();
        break;
      case "seeked":
        handleSeeked();
        break;
      case "ended":
        handleEnded();
        break;
      case "volumechange":
        setMuted(mediaRef.current?.muted ?? true);
        break;
      case "error":
        safelyRecordTelemetry(telemetryRef.current, (telemetry) => {
          telemetry.terminalError(playbackErrorCategory(mediaRef.current?.mediaErrorCode?.()));
        });
        setPlaybackError("视频加载失败，请稍后重试");
        setPlaying(false);
        break;
      case "canplay":
      case "progress":
        break;
    }
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
        <div ref={mediaHostRef} className="stage-media-host" onClick={togglePlayback} />
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

function safelyRecordTelemetry(
  telemetry: PlaybackTelemetrySession | null,
  record: (session: PlaybackTelemetrySession) => void
): void {
  if (!telemetry) return;
  try {
    record(telemetry);
  } catch {
    // Playback telemetry is best-effort and must never affect media controls.
  }
}
