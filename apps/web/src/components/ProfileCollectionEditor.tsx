import { useEffect, useMemo, useState } from "react";
import type { FormEvent } from "react";
import { useDialogFocus } from "../hooks/useDialogFocus";
import type {
  CollectionVisibility,
  CreateVideoCollectionRequest,
  UpdateVideoCollectionRequest,
  Video,
  VideoCollection
} from "../types";
import { image } from "../constants";
import { Icon } from "./Icon";

interface ProfileCollectionEditorProps {
  collection: VideoCollection | null;
  availableVideos: Video[];
  availableVideosHasMore?: boolean;
  availableVideosLoading?: boolean;
  availableVideosError?: string;
  busy: boolean;
  message?: string;
  onClose: () => void;
  onSave: (body: CreateVideoCollectionRequest | UpdateVideoCollectionRequest) => Promise<void>;
  onDelete?: () => Promise<void>;
  onLoadMoreAvailableVideos?: () => Promise<void>;
  onSearchAvailableVideos?: (query: string) => Promise<void>;
  onSetMembership?: (videoID: number, active: boolean) => Promise<void>;
}

export function ProfileCollectionEditor({
  collection,
  availableVideos,
  availableVideosHasMore = false,
  availableVideosLoading = false,
  availableVideosError = "",
  busy,
  message,
  onClose,
  onSave,
  onDelete,
  onLoadMoreAvailableVideos,
  onSearchAvailableVideos,
  onSetMembership
}: ProfileCollectionEditorProps) {
  const [title, setTitle] = useState(collection?.title || "");
  const [description, setDescription] = useState(collection?.description || "");
  const [visibility, setVisibility] = useState<CollectionVisibility>(collection?.visibility || "public");
  const [videoQuery, setVideoQuery] = useState("");
  const closeRef = useDialogFocus<HTMLButtonElement>(true, onClose);
  const memberIDs = useMemo(() => new Set((collection?.items || []).map((item) => item.video_id)), [collection]);
  const displayedVideos = useMemo(() => {
    const videos = new Map<number, Video>();
    for (const item of collection?.items || []) videos.set(item.video.id, item.video);
    for (const video of availableVideos) videos.set(video.id, video);
    return [...videos.values()];
  }, [availableVideos, collection]);

  useEffect(() => {
    setTitle(collection?.title || "");
    setDescription(collection?.description || "");
    setVisibility(collection?.visibility || "public");
    setVideoQuery("");
  }, [collection?.id]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    await onSave({ title, description, visibility });
  }

  return (
    <div className="modal-backdrop collection-editor-backdrop" role="presentation" onMouseDown={onClose}>
      <form
        aria-labelledby="collection-editor-title"
        aria-modal="true"
        className="profile-modal collection-editor"
        role="dialog"
        onMouseDown={(event) => event.stopPropagation()}
        onSubmit={submit}
      >
        <header>
          <h2 id="collection-editor-title">{collection ? "管理合集" : "创建合集"}</h2>
          <button ref={closeRef} className="icon-button small" type="button" onClick={onClose} aria-label="关闭">
            <Icon name="close" size={19} />
          </button>
        </header>
        <label className="profile-editor-field">
          <span>标题 <small>{title.length}/80</small></span>
          <input maxLength={80} required value={title} onChange={(event) => setTitle(event.target.value)} />
        </label>
        <label className="profile-editor-field">
          <span>描述 <small>{description.length}/500</small></span>
          <textarea maxLength={500} rows={3} value={description} onChange={(event) => setDescription(event.target.value)} />
        </label>
        <label className="profile-editor-field">
          <span>可见性</span>
          <select value={visibility} onChange={(event) => setVisibility(event.target.value as CollectionVisibility)}>
            <option value="public">公开</option>
            <option value="private">私密</option>
          </select>
        </label>
        {collection && (
          <fieldset className="collection-members">
            <legend>合集作品</legend>
            {onSearchAvailableVideos && (
              <label className="profile-editor-field">
                <span>搜索作品</span>
                <div className="profile-inline-fields">
                  <input
                    placeholder="按标题或描述搜索"
                    value={videoQuery}
                    onChange={(event) => setVideoQuery(event.target.value)}
                    onKeyDown={(event) => {
                      if (event.key !== "Enter") return;
                      event.preventDefault();
                      void onSearchAvailableVideos(videoQuery);
                    }}
                  />
                  <button
                    disabled={availableVideosLoading || busy}
                    type="button"
                    onClick={() => void onSearchAvailableVideos(videoQuery)}
                  >
                    搜索
                  </button>
                </div>
              </label>
            )}
            {displayedVideos.length === 0 ? (
              <p>{availableVideosLoading ? "作品加载中" : "暂无可添加作品"}</p>
            ) : (
              <div className="collection-member-list">
                {displayedVideos.map((video) => (
                  <label key={video.id}>
                    <img src={video.cover_url || image.stage} alt="" />
                    <span>{video.title}</span>
                    <input
                      checked={memberIDs.has(video.id)}
                      disabled={busy}
                      type="checkbox"
                      onChange={(event) => void onSetMembership?.(video.id, event.target.checked)}
                    />
                  </label>
                ))}
              </div>
            )}
            {availableVideosError && <p className="form-message">{availableVideosError}</p>}
            {availableVideosHasMore && onLoadMoreAvailableVideos && (
              <button
                className="profile-manage-button"
                disabled={availableVideosLoading || busy}
                type="button"
                onClick={() => void onLoadMoreAvailableVideos()}
              >
                {availableVideosLoading ? "加载中" : "加载更多作品"}
              </button>
            )}
          </fieldset>
        )}
        {message && <p className="form-message">{message}</p>}
        <footer className="profile-editor-actions">
          {collection && onDelete && (
            <button className="danger-button" disabled={busy} type="button" onClick={() => void onDelete()}>
              删除合集
            </button>
          )}
          <span />
          <button type="button" onClick={onClose}>取消</button>
          <button className="primary-button" disabled={busy} type="submit">{busy ? "保存中" : "保存"}</button>
        </footer>
      </form>
    </div>
  );
}
