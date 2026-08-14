// 发布视频页：视频 + 封面上传后创建作品。
import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { createVideo } from "../api/account";
import {
  UserFacingError,
  apiErrorMessage,
  currentConsumerSessionEpoch,
  requireConsumerSessionEpoch
} from "../api/client";
import { uploadMediaFile } from "../api/upload";
import type { MediaUploadResult, UploadKind } from "../api/upload";
import { useNavigate } from "../router";
import { useSession } from "../session";
import { Icon } from "../components/Icon";

interface UploadForm {
  title: string;
  description: string;
}

interface SelectedUpload {
  file: File;
  attemptID: string;
  result: MediaUploadResult | null;
  resultEpoch: number;
  resultUserID: number;
}

export function UploadPage() {
  const session = useSession();
  const navigate = useNavigate();
  const [form, setForm] = useState<UploadForm>({
    title: "",
    description: ""
  });
  const [videoFile, setVideoFile] = useState<File | null>(null);
  const [coverFile, setCoverFile] = useState<File | null>(null);
  const [videoPreview, setVideoPreview] = useState("");
  const [coverPreview, setCoverPreview] = useState("");
  const [previewError, setPreviewError] = useState("");
  const [status, setStatus] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [videoProgress, setVideoProgress] = useState(0);
  const [coverProgress, setCoverProgress] = useState(0);
  const videoUploadRef = useRef<SelectedUpload | null>(null);
  const coverUploadRef = useRef<SelectedUpload | null>(null);
  const creationAttemptRef = useRef("");

  useEffect(() => {
    if (!coverFile) {
      setCoverPreview("");
      return;
    }
    const objectURL = URL.createObjectURL(coverFile);
    setCoverPreview(objectURL);
    return () => URL.revokeObjectURL(objectURL);
  }, [coverFile]);

  useEffect(() => {
    setPreviewError("");
    if (!videoFile) {
      setVideoPreview("");
      return;
    }
    const objectURL = URL.createObjectURL(videoFile);
    setVideoPreview(objectURL);
    return () => URL.revokeObjectURL(objectURL);
  }, [videoFile]);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setStatus("");
    setVideoProgress(0);
    setCoverProgress(0);
    try {
      const initiatingEpoch = currentConsumerSessionEpoch();
      const initiatingUserID = session.user?.id ?? 0;
      if (initiatingUserID <= 0) {
        throw new UserFacingError("请先登录后再发布");
      }
      const title = form.title.trim();
      if (!title) {
        throw new UserFacingError("请输入视频标题");
      }
      if (utf8ByteLength(title) > 128) {
        throw new UserFacingError("视频标题过长，请缩短后重试");
      }
      const description = form.description.trim();
      if (utf8ByteLength(description) > 512) {
        throw new UserFacingError("视频简介过长，请缩短后重试");
      }
      if (!videoFile) {
        throw new UserFacingError("请选择视频文件");
      }
      if (!coverFile) {
        throw new UserFacingError("请选择封面文件");
      }
      validateMediaFile(videoFile, "video");
      validateMediaFile(coverFile, "cover");
      setStatus("正在计算校验和并上传");
      const [videoUploadResult, coverUploadResult] = await Promise.allSettled([
        uploadSelectedFile(
          videoUploadRef, videoFile, "video", session.token,
          initiatingEpoch, initiatingUserID, setVideoProgress
        ),
        uploadSelectedFile(
          coverUploadRef, coverFile, "cover", session.token,
          initiatingEpoch, initiatingUserID, setCoverProgress
        )
      ]);
      if (videoUploadResult.status === "rejected") {
        throw videoUploadResult.reason;
      }
      if (coverUploadResult.status === "rejected") {
        throw coverUploadResult.reason;
      }
      const videoUpload = videoUploadResult.value;
      const coverUpload = coverUploadResult.value;
      requireConsumerSessionEpoch(initiatingEpoch);
      const uploadReferences =
        videoUpload.mode === "direct" && coverUpload.mode === "direct"
          ? { media_asset_id: videoUpload.assetID, cover_asset_id: coverUpload.assetID }
          : videoUpload.mode === "multipart" && coverUpload.mode === "multipart"
            ? { media_url: videoUpload.url, cover_url: coverUpload.url }
            : null;
      if (!uploadReferences) {
        throw new UserFacingError("视频和封面上传模式不一致，请重试");
      }
      if (!creationAttemptRef.current) {
        creationAttemptRef.current = crypto.randomUUID();
      }
      const creationKey = `web-video-${creationAttemptRef.current}`;
      setStatus("正在创建作品");
      requireConsumerSessionEpoch(initiatingEpoch);
      const video = await createVideo(session.token, {
        title,
        description,
        ...uploadReferences
      }, creationKey);
      requireConsumerSessionEpoch(initiatingEpoch);
      setStatus(video.media_status === "processing"
        ? "上传完成，视频处理中并等待审核"
        : "上传成功，等待审核");
      videoUploadRef.current = null;
      coverUploadRef.current = null;
      creationAttemptRef.current = "";
      navigate("/profile");
    } catch (error) {
      setStatus(apiErrorMessage(error, "发布失败"));
    } finally {
      setSubmitting(false);
    }
  }

  if (!session.token) {
    return (
      <main className="upload-page" data-ui="upload-page">
        <section className="upload-card">
          <div className="upload-empty">
            <span className="status-icon"><Icon name="lock" /></span>
            <h1>登录后发布视频</h1>
            <button className="primary-button" onClick={() => navigate("/auth")}>
              <Icon name="login" size={18} />
              登录
            </button>
          </div>
        </section>
      </main>
    );
  }

  return (
    <main className="upload-page" data-ui="upload-page">
      <section className="upload-card">
        <header>
          <div>
            <p className="eyebrow">发布</p>
            <h1>发布视频</h1>
          </div>
          <button className="ghost-button compact" onClick={() => navigate("/timeline")}>
            <Icon name="home" size={17} />
            最新视频
          </button>
        </header>

        <div className="upload-grid">
          <form className="upload-form" onSubmit={handleSubmit}>
            <label>
              <span>标题</span>
              <input
                value={form.title}
                onChange={(event) => setForm({ ...form, title: event.target.value })}
                placeholder="输入视频标题"
                required
                maxLength={128}
              />
            </label>
            <label>
              <span>简介</span>
              <textarea
                value={form.description}
                onChange={(event) => setForm({ ...form, description: event.target.value })}
                placeholder="输入视频简介"
                rows={4}
                maxLength={512}
              />
            </label>
            <label>
              <span>视频</span>
              <span className="file-picker">
                <span className="file-picker-icon"><Icon name="film" /></span>
                <span className="file-picker-copy">
                  <strong>{videoFile ? videoFile.name : "选择视频文件"}</strong>
                  <small>本地视频上传</small>
                </span>
                <input
                  type="file"
                  accept=".mp4,.mov,.webm,video/mp4,video/quicktime,video/webm"
                  disabled={submitting}
                  onChange={(event) => {
                    const file = event.target.files?.[0] || null;
                    setStatus("");
                    setVideoProgress(0);
                    setVideoFile(file);
                    videoUploadRef.current = file ? newSelectedUpload(file) : null;
                    creationAttemptRef.current = "";
                  }}
                />
              </span>
            </label>
            <label>
              <span>封面</span>
              <span className="file-picker">
                <span className="file-picker-icon"><Icon name="image" /></span>
                <span className="file-picker-copy">
                  <strong>{coverFile ? coverFile.name : "选择封面文件"}</strong>
                  <small>本地图片上传</small>
                </span>
                <input
                  type="file"
                  accept=".jpg,.jpeg,.png,.webp,image/jpeg,image/png,image/webp"
                  disabled={submitting}
                  onChange={(event) => {
                    const file = event.target.files?.[0] || null;
                    setStatus("");
                    setCoverProgress(0);
                    setCoverFile(file);
                    coverUploadRef.current = file ? newSelectedUpload(file) : null;
                    creationAttemptRef.current = "";
                  }}
                />
              </span>
            </label>
            {submitting && (
              <div className="upload-progress-list" aria-live="polite">
                <UploadProgress label="视频" value={videoProgress} />
                <UploadProgress label="封面" value={coverProgress} />
              </div>
            )}
            {status && <p className={`form-message ${status.startsWith("上传完成") || status.startsWith("上传成功") ? "success" : ""}`}>{status}</p>}
            <button className="primary-button" disabled={submitting}>
              <Icon name="publish" size={18} />
              {submitting ? "发布中" : "发布"}
            </button>
          </form>

          <aside className="upload-preview">
            <div className="preview-frame">
              {videoPreview ? (
                <video
                  src={videoPreview}
                  poster={coverPreview || undefined}
                  controls
                  muted
                  playsInline
                  preload="metadata"
                  onCanPlay={() => setPreviewError("")}
                  onError={() => setPreviewError("浏览器无法预览该本地视频，但仍可保留文件并尝试上传。")}
                />
              ) : coverPreview ? (
                <img src={coverPreview} alt="" />
              ) : (
                <Icon name="film" size={44} />
              )}
            </div>
            <div>
              <h2>{form.title || "视频预览"}</h2>
              <p>{form.description || (videoFile ? videoFile.name : "选择本地视频和封面后会提交到后端视频接口。")}</p>
              {coverPreview && (
                <span className="upload-cover-preview">
                  <img src={coverPreview} alt="" />
                  已选封面
                </span>
              )}
              {previewError && <p className="form-message">{previewError}</p>}
            </div>
          </aside>
        </div>
      </section>
    </main>
  );
}

function utf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).length;
}

function newSelectedUpload(file: File): SelectedUpload {
  return {
    file,
    attemptID: crypto.randomUUID(),
    result: null,
    resultEpoch: -1,
    resultUserID: 0
  };
}

async function uploadSelectedFile(
  selectionRef: { current: SelectedUpload | null },
  file: File,
  kind: UploadKind,
  token: string,
  sessionEpoch: number,
  userID: number,
  onProgress: (progress: number) => void
): Promise<MediaUploadResult> {
  const selection = selectionRef.current;
  if (!selection || selection.file !== file) {
    throw new UserFacingError("文件选择已变化，请重新发布");
  }
  if (selection.result) {
    if (selection.resultEpoch === sessionEpoch &&
      selection.resultUserID === userID) {
      onProgress(100);
      return selection.result;
    }
    selection.result = null;
    selection.resultEpoch = -1;
    selection.resultUserID = 0;
    selection.attemptID = crypto.randomUUID();
  }
  const result = await uploadMediaFile(file, kind, token, selection.attemptID, onProgress);
  if (selectionRef.current === selection) {
    selection.result = result;
    selection.resultEpoch = sessionEpoch;
    selection.resultUserID = userID;
  }
  return result;
}

function validateMediaFile(file: File, kind: UploadKind) {
  const isVideo = kind === "video";
  const label = isVideo ? "视频" : "封面";
  const maxBytes = isVideo ? 512 * 1024 * 1024 : 20 * 1024 * 1024;
  if (file.size <= 0) {
    throw new UserFacingError(`${label}文件为空，请重新选择`);
  }
  if (file.size > maxBytes) {
    throw new UserFacingError(`${label}不能超过 ${isVideo ? 512 : 20} MB`);
  }

  const extension = file.name.toLowerCase().match(/\.[^.]+$/)?.[0] || "";
  const contentType = file.type.toLowerCase().split(";", 1)[0].trim();
  const allowedExtensions = isVideo
    ? new Set([".mp4", ".mov", ".webm"])
    : new Set([".jpg", ".jpeg", ".png", ".webp"]);
  const allowedContentTypes = isVideo
    ? new Set(["video/mp4", "video/quicktime", "video/webm"])
    : new Set(["image/jpeg", "image/png", "image/webp"]);
  if (!allowedExtensions.has(extension) || (contentType && !allowedContentTypes.has(contentType))) {
    throw new UserFacingError(
      isVideo ? "视频仅支持 MP4、MOV 或 WebM 格式" : "封面仅支持 JPG、PNG 或 WebP 格式"
    );
  }
}

function UploadProgress({ label, value }: { label: string; value: number }) {
  const progress = Math.max(0, Math.min(100, value));
  return (
    <div className="upload-progress">
      <span>{label}</span>
      <progress max="100" value={progress} />
      <strong>{progress}%</strong>
    </div>
  );
}
