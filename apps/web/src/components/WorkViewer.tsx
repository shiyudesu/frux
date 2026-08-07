// 作品查看弹窗。
import { useCallback, useEffect, useRef, useState } from "react";
import { fetchProtectedAssetAccess } from "../api/upload";
import { apiErrorMessage } from "../api/client";
import { image } from "../constants";
import { useDialogFocus } from "../hooks/useDialogFocus";
import type { Video } from "../types";
import { formatMetric } from "../utils";
import { Icon } from "./Icon";

interface WorkViewerProps {
  video: Video;
  token?: string;
  onClose: () => void;
}

export function WorkViewer({ video, token = "", onClose }: WorkViewerProps) {
  const generation = useRef(0);
  const videoElement = useRef<HTMLVideoElement | null>(null);
  const activeVideoID = useRef(video.id);
  const resolved = useRef({
    mediaURL: video.media_url || "",
    coverURL: video.cover_url || ""
  });
  const expiryByKind = useRef({ media: "", cover: "" });
  const failedKinds = useRef(new Set<"media" | "cover">());
  const retryDelay = useRef(5_000);
  const mediaProtected = useRef(false);
  const resumePlayback = useRef<{ time: number; paused: boolean } | null>(null);
  const [mediaURL, setMediaURL] = useState(video.media_url || "");
  const [coverURL, setCoverURL] = useState(video.cover_url || "");
  const [expiresAt, setExpiresAt] = useState("");
  const [retryAt, setRetryAt] = useState("");
  const [loading, setLoading] = useState(false);
  const [accessError, setAccessError] = useState("");
  const [playbackError, setPlaybackError] = useState("");
  const closeButtonRef = useDialogFocus<HTMLButtonElement>(true, onClose);
  const loadAccess = useCallback(async () => {
    const requestGeneration = ++generation.current;
    const videoChanged = activeVideoID.current !== video.id;
    if (videoChanged) {
      activeVideoID.current = video.id;
      resolved.current = {
        mediaURL: video.media_url || "",
        coverURL: video.cover_url || ""
      };
      expiryByKind.current = { media: "", cover: "" };
      failedKinds.current.clear();
      retryDelay.current = 5_000;
      mediaProtected.current = false;
      setMediaURL(resolved.current.mediaURL);
      setCoverURL(resolved.current.coverURL);
      setExpiresAt("");
      setRetryAt("");
    }
    if (!token) {
      resolved.current = {
        mediaURL: video.media_url || "",
        coverURL: video.cover_url || ""
      };
      expiryByKind.current = { media: "", cover: "" };
      failedKinds.current.clear();
      retryDelay.current = 5_000;
      mediaProtected.current = false;
      setMediaURL(resolved.current.mediaURL);
      setCoverURL(resolved.current.coverURL);
      setExpiresAt("");
      setRetryAt("");
      setLoading(false);
      return;
    }
    const needsMedia = Boolean(token && !video.media_url && video.media_asset_id);
    const needsCover = Boolean(token && !video.cover_url && video.cover_asset_id);
    if (!needsMedia) {
      expiryByKind.current.media = "";
      if (video.media_url) mediaProtected.current = false;
    }
    if (!needsCover) expiryByKind.current.cover = "";
    setPlaybackError("");
    setAccessError("");
    if (!needsMedia && !needsCover) {
      setLoading(false);
      setExpiresAt("");
      return;
    }
    const backgroundRefresh = !videoChanged &&
      Boolean(resolved.current.mediaURL || resolved.current.coverURL);
    if (!backgroundRefresh) setLoading(true);
    const now = Date.now();
    const refreshMedia = needsMedia && shouldRefreshProtectedKind(
      resolved.current.mediaURL,
      expiryByKind.current.media,
      failedKinds.current.has("media"),
      now
    );
    const refreshCover = needsCover && shouldRefreshProtectedKind(
      resolved.current.coverURL,
      expiryByKind.current.cover,
      failedKinds.current.has("cover"),
      now
    );
    const [media, cover] = await Promise.allSettled([
      refreshMedia
        ? fetchProtectedAssetAccess(token, video.media_asset_id || 0)
        : Promise.resolve(null),
      refreshCover
        ? fetchProtectedAssetAccess(token, video.cover_asset_id || 0)
        : Promise.resolve(null)
    ]);
    if (generation.current !== requestGeneration) return;
    const expiries: string[] = [];
    let nextMedia = video.media_url || resolved.current.mediaURL;
    let nextCover = video.cover_url || resolved.current.coverURL;
    const failures: unknown[] = [];
    if (media.status === "fulfilled" && media.value) {
      nextMedia = media.value.url;
      expiryByKind.current.media = media.value.expires_at;
      failedKinds.current.delete("media");
      mediaProtected.current = true;
    } else if (media.status === "rejected") {
      failures.push(media.reason);
      failedKinds.current.add("media");
    }
    if (cover.status === "fulfilled" && cover.value) {
      nextCover = cover.value.url;
      expiryByKind.current.cover = cover.value.expires_at;
      failedKinds.current.delete("cover");
    } else if (cover.status === "rejected") {
      failures.push(cover.reason);
      failedKinds.current.add("cover");
    }
    if (nextMedia !== resolved.current.mediaURL && videoElement.current) {
      resumePlayback.current = {
        time: videoElement.current.currentTime,
        paused: videoElement.current.paused
      };
    }
    resolved.current = { mediaURL: nextMedia, coverURL: nextCover };
    setMediaURL(nextMedia);
    setCoverURL(nextCover);
    if (!failedKinds.current.has("media")) expiries.push(expiryByKind.current.media);
    if (!failedKinds.current.has("cover")) expiries.push(expiryByKind.current.cover);
    setExpiresAt(earliestExpiry(expiries));
    setLoading(false);
    if (failures.length > 0) {
      setAccessError(apiErrorMessage(failures[0], "作品预览暂时不可用"));
      const delay = retryDelay.current;
      retryDelay.current = Math.min(60_000, delay * 2);
      setRetryAt(new Date(Date.now() + delay).toISOString());
    } else if (failedKinds.current.size === 0) {
      retryDelay.current = 5_000;
      setRetryAt("");
    }
  }, [
    token,
    video.cover_asset_id,
    video.cover_url,
    video.id,
    video.media_asset_id,
    video.media_url
  ]);

  useEffect(() => {
    void loadAccess();
    return () => {
      generation.current++;
    };
  }, [loadAccess]);

  useEffect(() => {
    const candidates = [
      expiresAt ? new Date(expiresAt).getTime() - 30_000 : Number.POSITIVE_INFINITY,
      retryAt ? new Date(retryAt).getTime() : Number.POSITIVE_INFINITY
    ].filter(Number.isFinite);
    if (candidates.length === 0) return;
    const refreshIn = Math.min(...candidates) - Date.now();
    const timer = window.setTimeout(
      () => void loadAccess(),
      Math.min(2_147_000_000, Math.max(1_000, refreshIn))
    );
    return () => window.clearTimeout(timer);
  }, [expiresAt, loadAccess, retryAt]);

  const cover = coverURL || image.stage;
  const playable = Boolean(mediaURL);
  const retryPlayback = () => {
    if (token && mediaProtected.current && video.media_asset_id) {
      if (video.media_asset_id) failedKinds.current.add("media");
      void loadAccess();
      return;
    }
    setPlaybackError("");
    videoElement.current?.load();
  };

  return (
    <div className="modal-backdrop work-viewer-backdrop" role="presentation" onClick={onClose}>
      <section
        aria-modal="true"
        className="work-viewer"
        data-ui="work-viewer"
        role="dialog"
        onClick={(event) => event.stopPropagation()}
      >
        <header>
          <div>
            <h2>{video.title || "作品"}</h2>
            <p>{formatMetric(video.like_count || 0)} 点赞 · {formatMetric(video.comment_count || 0)} 评论</p>
          </div>
          <button ref={closeButtonRef} className="icon-button small" type="button" onClick={onClose} aria-label="关闭">
            <Icon name="close" size={19} />
          </button>
        </header>
        <div className="work-viewer-stage">
          {loading ? (
            <div className="work-viewer-state">正在获取受保护预览…</div>
          ) : playable ? (
            <video
              ref={videoElement}
              src={mediaURL}
              poster={cover}
              controls
              autoPlay
              playsInline
              onLoadedMetadata={(event) => {
                const resume = resumePlayback.current;
                if (!resume) return;
                event.currentTarget.currentTime = resume.time;
                if (!resume.paused) void event.currentTarget.play();
                resumePlayback.current = null;
              }}
              onCanPlay={() => setPlaybackError("")}
              onError={() => setPlaybackError("当前源暂时无法在浏览器中播放，视频可能仍在处理中。")}
            />
          ) : (
            <img src={cover} alt="" />
          )}
        </div>
        {!loading && !mediaURL && (
          <p className="work-viewer-message">视频仍在处理或当前只有封面可预览。</p>
        )}
        {playbackError && (
          <p className="work-viewer-message warning">
            {playbackError}
            <button type="button" onClick={retryPlayback}>重新获取预览</button>
          </p>
        )}
        {accessError && (
          <p className="work-viewer-message warning">
            {accessError}
            <button type="button" onClick={() => void loadAccess()}>重试</button>
          </p>
        )}
      </section>
    </div>
  );
}

function shouldRefreshProtectedKind(
  currentURL: string,
  expiresAt: string,
  failed: boolean,
  now: number
): boolean {
  if (!currentURL || failed) return true;
  const expiry = new Date(expiresAt).getTime();
  return !Number.isFinite(expiry) || expiry <= now + 30_000;
}

function earliestExpiry(values: string[]): string {
  return values
    .filter((value) => Number.isFinite(new Date(value).getTime()))
    .sort((left, right) => new Date(left).getTime() - new Date(right).getTime())[0] || "";
}
