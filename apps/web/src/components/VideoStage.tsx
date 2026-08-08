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
import {
  createInitialPlayerState,
  type NormalizedPlayerState,
  type FeedPlayerPoolResource,
  type PlayerPreferences,
  type QualitySelection
} from "../player";
import { PlaybackLifecycle } from "../playbackLifecycle";
import {
  buildPlaybackTelemetryContext,
  playbackErrorCategory,
  playbackTelemetrySource,
  PlaybackTelemetrySession
} from "../playbackTelemetry";
import type {
  CreateViewEventRequest,
  FeedVideo,
  PlaybackTelemetryBatch,
  PlaybackTelemetryErrorCategory
} from "../types";
import type { RecommendationFeedbackType } from "../types";
import type { PlaybackQoSMetrics, PublicProfileInput, VideoQoSState } from "../utils";
import { createVideoQoSState, isVideoSource } from "../utils";
import { FeedActionRail, feedbackStateKey } from "./FeedActionRail";
import { FeedMetadata } from "./FeedMetadata";
import { FeedPlayerControls } from "./FeedPlayerControls";
import { StageGestureLayer } from "./StageGestureLayer";

export interface VideoStageHandle {
  togglePlayback: () => void;
}

export interface VideoStageProps {
  item: FeedVideo;
  active: boolean;
  showSocialActions?: boolean;
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
  playerResource?: FeedPlayerPoolResource;
  playerPreferences: PlayerPreferences;
  onUpdatePlayerPreferences: (patch: Partial<PlayerPreferences>) => void;
  onContinuousAdvance: () => void;
  onRecommendationFeedback?: (item: FeedVideo, type: RecommendationFeedbackType) => Promise<void>;
  watchLaterAction?: "add" | "remove";
  onWatchLater?: () => Promise<void>;
}

export const VideoStage = forwardRef<VideoStageHandle, VideoStageProps>(function VideoStage(
  {
    item,
    active,
    showSocialActions = true,
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
    preloadPolicy,
    playerResource,
    playerPreferences,
    onUpdatePlayerPreferences,
    onContinuousAdvance,
    onRecommendationFeedback,
    watchLaterAction,
    onWatchLater
  },
  ref
) {
  const cover = item.cover_url || image.stage;
  const media = item.media_url || cover;
  const showVideo = isVideoSource(media)
    || Boolean(item.playback_sources?.some((source) => source.type === "mp4" || source.type === "dash"));
  const stageRef = useRef<HTMLElement | null>(null);
  const mediaHostRef = useRef<HTMLDivElement | null>(null);
  const mediaRef = useRef<FeedPreloadMediaResource | null>(null);
  const itemRef = useRef(item);
  const activeRef = useRef(active);
  const playerPreferencesRef = useRef(playerPreferences);
  const onContinuousAdvanceRef = useRef(onContinuousAdvance);
  const qosRef = useRef<VideoQoSState>(createVideoQoSState(item.video_id));
  const lifecycleRef = useRef<PlaybackLifecycle | null>(null);
  const telemetryRef = useRef<PlaybackTelemetrySession | null>(null);
  const telemetrySourceKeyRef = useRef("");
  const telemetryErrorKeyRef = useRef("");
  const onViewEventRef = useRef(onViewEvent);
  const completionPollRef = useRef<number | null>(null);
  const [playerState, setPlayerState] = useState<Readonly<NormalizedPlayerState>>(() => createInitialPlayerState());
  const playerStateRef = useRef<Readonly<NormalizedPlayerState>>(playerState);
  const [fullscreen, setFullscreen] = useState(false);
  const [playbackError, setPlaybackError] = useState("");
  activeRef.current = active;
  playerPreferencesRef.current = playerPreferences;
  onContinuousAdvanceRef.current = onContinuousAdvance;
  playerStateRef.current = playerState;
  itemRef.current = item;

  useEffect(() => {
    telemetrySourceKeyRef.current = "";
    telemetryErrorKeyRef.current = "";
    setPlayerState(createInitialPlayerState());
    setPlaybackError("");
  }, [item.media_status, item.playback_sources, item.video_id, media]);

  useEffect(() => {
    onViewEventRef.current = onViewEvent;
  }, [onViewEvent]);

  useEffect(() => {
    const telemetry = telemetryRef.current;
    if (!active || !playerState.source || !telemetry) return;
    const activeQuality = playerState.qualities.find((quality) => quality.active);
    const sourceKey = `${playerState.source.id}:${playerState.effectiveQualityId || ""}`;
    if (telemetrySourceKeyRef.current === sourceKey) return;
    const previousSourceKey = telemetrySourceKeyRef.current;
    telemetrySourceKeyRef.current = sourceKey;
    const source = playbackTelemetrySource(itemRef.current, playerState.source.url);
    safelyRecordTelemetry(telemetry, (session) => {
      const renditionLabel = activeQuality?.label || playerState.source?.qualityLabel || source.renditionLabel;
      if (previousSourceKey.startsWith(`${playerState.source?.id}:`)) {
        session.qualityChanged(renditionLabel);
        return;
      }
      session.sourceChanged({
        ...source,
        renditionLabel,
        playerAdapter: playerState.source?.type === "dash" ? "dash" : "native_mp4"
      });
    });
  }, [
    active,
    playerState.effectiveQualityId,
    playerState.qualities,
    playerState.source
  ]);

  useEffect(() => {
    const telemetry = telemetryRef.current;
    const error = playerState.error;
    if (!active || playerState.status !== "error" || !error || !telemetry) return;
    const errorKey = `${error.category}:${error.code}:${playerState.source?.id || ""}`;
    if (telemetryErrorKeyRef.current === errorKey) return;
    telemetryErrorKeyRef.current = errorKey;
    safelyRecordTelemetry(telemetry, (session) => {
      session.playFailed(telemetryErrorCategory(error.category));
    });
  }, [active, playerState.error, playerState.source?.id, playerState.status]);

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
    if (playerState.source) {
      const activeQuality = playerState.qualities.find((quality) => quality.active);
      telemetrySourceKeyRef.current = `${playerState.source.id}:${playerState.effectiveQualityId || ""}`;
      const source = playbackTelemetrySource(item, playerState.source.url);
      telemetry.sourceChanged({
        ...source,
        renditionLabel: activeQuality?.label || playerState.source.qualityLabel || source.renditionLabel,
        playerAdapter: playerState.source.type === "dash" ? "dash" : "native_mp4"
      });
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
      .then(() => setPlaybackError(""))
      .catch(() => {
        setPlaybackError("浏览器暂时无法播放该视频");
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
      !playerResource && preloadCandidate && preloadController && preloadPolicy
        ? preloadController.acquireCandidate(preloadCandidate, preloadPolicy)
        : undefined;
    const resource = playerResource?.media || acquired?.media || createNativeFeedPreloadMediaResource();
    if (activeRef.current) {
      safelyRecordTelemetry(telemetryRef.current, (telemetry) => {
        telemetry.sourceLoadStarted(resource, playbackTelemetrySource(itemRef.current, media));
      });
    }
    if (!playerResource && !acquired) {
      resource.configure(media, cover, "buffer", itemRef.current);
      resource.load();
    }
    const effectivePreferences = resource.getPlayerPreferences?.() || playerPreferences;
    if (
      effectivePreferences.quality !== playerPreferences.quality ||
      effectivePreferences.playbackRate !== playerPreferences.playbackRate ||
      effectivePreferences.continuousPlay !== playerPreferences.continuousPlay
    ) {
      onUpdatePlayerPreferences(effectivePreferences);
    }
    resource.muted = playerState.muted;
    resource.setPlaybackRate?.(effectivePreferences.playbackRate);
    resource.setQuality?.(effectivePreferences.quality);
    resource.setContinuousPlay?.(effectivePreferences.continuousPlay);
    resource.mount(host, "stage-media");
    mediaRef.current = resource;
    const unsubscribe = resource.subscribe(handleMediaEvent);
    const unsubscribePlayer = resource.subscribePlayerState?.(setPlayerState);
    if (resource.getPlayerState) setPlayerState(resource.getPlayerState());
    if (resource.readyState >= 1) {
      handleLoadedMetadata();
    }
    if (resource.readyState >= 2) {
      handleLoadedData();
    }

    return () => {
      unsubscribe();
      unsubscribePlayer?.();
      resource.pause();
      if (mediaRef.current === resource) {
        mediaRef.current = null;
      }
      if (acquired) {
        acquired.release();
      } else if (!playerResource) {
        resource.destroy();
      }
    };
  }, [
    cover,
    item.media_status,
    item.playback_sources,
    item.video_id,
    media,
    playerResource?.media,
    preloadController,
    preloadKey,
    preloadPolicy,
    showVideo
  ]);

  useEffect(() => {
    const video = mediaRef.current;
    if (!video || !showVideo) return;
    if (active) {
      playVideo();
      return;
    }
    video.pause();
  }, [active, item.video_id, media, playVideo, playerResource?.media, showVideo]);

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
    safelyRecordTelemetry(telemetryRef.current, (telemetry) => {
      telemetry.metadataReady();
    });
  }

  function handlePlaying() {
    const state = qosRef.current;
    const video = mediaRef.current;
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
    emitViewEvents(lifecycleRef.current?.timeUpdate(performance.now(), video.currentTime, video.duration) || []);
    safelyRecordTelemetry(telemetryRef.current, (telemetry) => telemetry.timeUpdated());
  }

  function handleSeek(value: number) {
    const video = mediaRef.current;
    if (!video || !Number.isFinite(value)) return;
    video.currentTime = Math.max(0, Math.min(value, video.duration || value));
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
    if (playerPreferencesRef.current.continuousPlay) onContinuousAdvanceRef.current();
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
        break;
      case "error":
        if (!(mediaRef.current?.getPlayerState?.().error || playerStateRef.current.error)?.recoverable) {
          safelyRecordTelemetry(telemetryRef.current, (telemetry) => {
            telemetry.terminalError(playbackErrorCategory(mediaRef.current?.mediaErrorCode?.()));
          });
        }
        setPlaybackError("视频加载失败，请稍后重试");
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

  function handleSelectQuality(selection: QualitySelection) {
    onUpdatePlayerPreferences({ quality: selection });
    mediaRef.current?.setQuality?.(selection);
  }

  function handleSelectRate(rate: number) {
    onUpdatePlayerPreferences({ playbackRate: rate });
    mediaRef.current?.setPlaybackRate?.(rate);
  }

  function handleToggleContinuousPlay() {
    const enabled = !playerPreferences.continuousPlay;
    onUpdatePlayerPreferences({ continuousPlay: enabled });
    mediaRef.current?.setContinuousPlay?.(enabled);
  }

  function handleRetry() {
    setPlaybackError("");
    void mediaRef.current?.retry?.().catch(() => {
      setPlaybackError("重试失败，请稍后再试");
    });
  }

  return (
    <article className="video-stage" data-ui="feed-stage" ref={stageRef}>
      <img className="stage-backdrop" src={cover} alt="" />
      <div className="stage-vignette" />
      {showVideo ? (
        <StageGestureLayer
          ref={mediaHostRef}
          active={active}
          canLike={showSocialActions}
          videoID={item.video_id}
          liked={liked}
          onLike={onLike}
          onTogglePlayback={togglePlayback}
        />
      ) : (
        <img className="stage-media portrait-media" src={media} alt="" />
      )}
      <FeedMetadata item={item} followError={followError || playbackError} onOpenAuthor={onOpenAuthor} />
      <FeedActionRail
        key={feedbackStateKey(item)}
        item={item}
        showSocialActions={showSocialActions}
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
        onRecommendationFeedback={onRecommendationFeedback ? (type) => onRecommendationFeedback(item, type) : undefined}
        watchLaterAction={watchLaterAction}
        onWatchLater={onWatchLater}
      />
      {showVideo && (
        <FeedPlayerControls
          state={playerState}
          fullscreen={fullscreen}
          continuousPlay={playerPreferences.continuousPlay}
          onTogglePlayback={togglePlayback}
          onToggleMute={handleToggleMute}
          onSeek={handleSeek}
          onSelectQuality={handleSelectQuality}
          onSelectRate={handleSelectRate}
          onToggleContinuousPlay={handleToggleContinuousPlay}
          onRetry={handleRetry}
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

function telemetryErrorCategory(category: string): PlaybackTelemetryErrorCategory {
  if (category === "network") return "network";
  if (category === "decode") return "decode";
  if (category === "unsupported_codec" || category === "source_unavailable" || category === "manifest") {
    return "unsupported";
  }
  if (category === "autoplay") return "autoplay";
  return "unknown";
}
