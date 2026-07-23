// VideoStage：单个 Feed 视频的舞台（封面/视频、作者行、操作栏、QoS 采集）。
import { useCallback, useEffect, useRef } from "react";
import { image } from "../constants";
import type { FeedVideo } from "../types";
import type { PlaybackQoSMetrics, PublicProfileInput } from "../utils";
import { createVideoQoSState, formatMetric, isVideoSource, profileFromFeedItem } from "../utils";
import type { VideoQoSState } from "../utils";
import { ActionButton } from "./ActionButton";

export interface VideoStageProps {
  item: FeedVideo;
  active: boolean;
  liked: boolean;
  favorited: boolean;
  following: boolean;
  followBusy: boolean;
  ownVideo: boolean;
  followError: string;
  onLike: () => void;
  onComment: () => void;
  onFavorite: () => void;
  onFollow: () => void;
  onPlaybackQoS?: (item: FeedVideo, metrics: PlaybackQoSMetrics) => void;
  onOpenAuthor: (profile: PublicProfileInput) => void;
}

export function VideoStage({
  item,
  active,
  liked,
  favorited,
  following,
  followBusy,
  ownVideo,
  followError,
  onLike,
  onComment,
  onFavorite,
  onFollow,
  onPlaybackQoS,
  onOpenAuthor
}: VideoStageProps) {
  const cover = item.cover_url || image.stage;
  const media = item.media_url || cover;
  const showVideo = isVideoSource(media);
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const itemRef = useRef(item);
  const qosRef = useRef<VideoQoSState>(createVideoQoSState(item.video_id));

  useEffect(() => {
    itemRef.current = item;
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
      const playback = video.play();
      if (playback?.catch) {
        playback.catch(() => {});
      }
      return;
    }
    video.pause();
  }, [active, media, showVideo]);

  function handleLoadedData() {
    const state = qosRef.current;
    if (!active || !state || state.firstFrameMs !== undefined) return;
    const startedAt = state.loadStartedAt || performance.now();
    state.firstFrameMs = Math.max(0, Math.round(performance.now() - startedAt));
  }

  function handlePlaying() {
    const state = qosRef.current;
    if (!active || !state) return;
    if (!state.loadStartedAt) {
      state.loadStartedAt = performance.now();
    }
    if (!state.playingStartedAt) {
      state.playingStartedAt = performance.now();
    }
  }

  function handleWaiting() {
    const state = qosRef.current;
    if (!active || !state) return;
    state.stutterCount += 1;
  }

  return (
    <article className="video-stage">
      <img className="stage-backdrop" src={cover} alt="" />
      <div className="stage-vignette" />
      {showVideo ? (
        <video
          ref={videoRef}
          className="stage-media"
          src={media}
          poster={cover}
          autoPlay={active}
          muted
          loop
          playsInline
          preload={active ? "metadata" : "none"}
          onLoadedData={handleLoadedData}
          onPlaying={handlePlaying}
          onWaiting={handleWaiting}
          onPause={flushQoS}
          onEnded={flushQoS}
        />
      ) : (
        <img className="stage-media portrait-media" src={media} alt="" />
      )}
      <div className="stage-copy">
        <div className="creator-row">
          <button className="creator-profile-button" type="button" onClick={() => onOpenAuthor(profileFromFeedItem(item))}>
            <img src={item.avatar_url || image.creator} alt="" />
            <strong>@{item.author}</strong>
          </button>
          <button
            className={`follow-button ${following ? "active" : ""}`}
            type="button"
            onClick={onFollow}
            disabled={followBusy || ownVideo}
          >
            {ownVideo ? "本人" : followBusy ? "处理中" : following ? "已关注" : "关注"}
          </button>
        </div>
        {followError && <p className="stage-notice">{followError}</p>}
        <h1>{item.title}</h1>
        <p>{item.description}</p>
      </div>
      <div className="action-rail">
        <ActionButton icon="favorite" label={formatMetric(item.like_count)} active={liked} onClick={onLike} />
        <ActionButton icon="chat_bubble" label={formatMetric(item.comment_count)} onClick={onComment} />
        <ActionButton icon="bookmark" label={formatMetric(item.favorite_count)} active={favorited} onClick={onFavorite} />
        <ActionButton icon="share" label="" compact />
      </div>
      <div className="progress-track">
        <span style={{ width: "34%" }} />
      </div>
    </article>
  );
}
