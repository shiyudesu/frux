// 发布视频页：视频 + 封面上传后创建作品。
import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { createVideo } from "../api/account";
import { UserFacingError, apiErrorMessage } from "../api/client";
import { uploadMediaFile } from "../api/upload";
import { useNavigate } from "../router";
import { useSession } from "../session";
import { Icon } from "../components/Icon";

interface UploadForm {
  title: string;
  description: string;
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
  const [coverPreview, setCoverPreview] = useState("");
  const [status, setStatus] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [videoProgress, setVideoProgress] = useState(0);
  const [coverProgress, setCoverProgress] = useState(0);
  const uploadAttemptRef = useRef("");

  useEffect(() => {
    if (!coverFile) {
      setCoverPreview("");
      return;
    }
    const objectURL = URL.createObjectURL(coverFile);
    setCoverPreview(objectURL);
    return () => URL.revokeObjectURL(objectURL);
  }, [coverFile]);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setStatus("");
    setVideoProgress(0);
    setCoverProgress(0);
    try {
      if (!videoFile) {
        throw new UserFacingError("请选择视频文件");
      }
      if (!coverFile) {
        throw new UserFacingError("请选择封面文件");
      }
      if (!uploadAttemptRef.current) {
        uploadAttemptRef.current = crypto.randomUUID();
      }
      const uploadAttemptID = uploadAttemptRef.current;
      setStatus("正在计算校验和并上传");
      const [videoUpload, coverUpload] = await Promise.all([
        uploadMediaFile(videoFile, "video", session.token, uploadAttemptID, setVideoProgress),
        uploadMediaFile(coverFile, "cover", session.token, uploadAttemptID, setCoverProgress)
      ]);
      const uploadReferences =
        videoUpload.mode === "direct" && coverUpload.mode === "direct"
          ? { media_asset_id: videoUpload.assetID, cover_asset_id: coverUpload.assetID }
          : videoUpload.mode === "multipart" && coverUpload.mode === "multipart"
            ? { media_url: videoUpload.url, cover_url: coverUpload.url }
            : null;
      if (!uploadReferences) {
        throw new UserFacingError("视频和封面上传模式不一致，请重试");
      }
      const videoReference = videoUpload.mode === "direct" ? String(videoUpload.assetID) : videoUpload.url;
      const coverReference = coverUpload.mode === "direct" ? String(coverUpload.assetID) : coverUpload.url;
      const creationKey = `web-video-${uploadAttemptID}-${videoReference}-${coverReference}`.slice(0, 128);
      setStatus("正在创建作品");
      const video = await createVideo(session.token, {
        title: form.title.trim(),
        description: form.description.trim(),
        ...uploadReferences
      }, creationKey);
      setStatus(video.media_status === "processing" ? "上传完成，视频正在处理中" : "发布成功");
      uploadAttemptRef.current = "";
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
              />
            </label>
            <label>
              <span>简介</span>
              <textarea
                value={form.description}
                onChange={(event) => setForm({ ...form, description: event.target.value })}
                placeholder="输入视频简介"
                rows={4}
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
                <input type="file" accept="video/*" onChange={(event) => {
                  uploadAttemptRef.current = "";
                  setVideoFile(event.target.files?.[0] || null);
                }} />
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
                <input type="file" accept="image/*" onChange={(event) => {
                  uploadAttemptRef.current = "";
                  setCoverFile(event.target.files?.[0] || null);
                }} />
              </span>
            </label>
            {submitting && (
              <div className="upload-progress-list" aria-live="polite">
                <UploadProgress label="视频" value={videoProgress} />
                <UploadProgress label="封面" value={coverProgress} />
              </div>
            )}
            {status && <p className={`form-message ${status === "发布成功" ? "success" : ""}`}>{status}</p>}
            <button className="primary-button" disabled={submitting}>
              <Icon name="publish" size={18} />
              {submitting ? "发布中" : "发布"}
            </button>
          </form>

          <aside className="upload-preview">
            <div className="preview-frame">
              {coverPreview ? <img src={coverPreview} alt="" /> : <Icon name="film" size={44} />}
            </div>
            <div>
              <h2>{form.title || "视频预览"}</h2>
              <p>{form.description || (videoFile ? videoFile.name : "选择本地视频和封面后会提交到后端视频接口。")}</p>
            </div>
          </aside>
        </div>
      </section>
    </main>
  );
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
