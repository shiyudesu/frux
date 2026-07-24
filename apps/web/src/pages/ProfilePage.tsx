// 个人资料页：资料展示/编辑、头像上传、我的作品、关注/粉丝弹窗。
import { useCallback, useEffect, useState } from "react";
import type { FormEvent } from "react";
import { fetchMyVideos, updateMyProfile } from "../api/account";
import { apiErrorMessage, isUnauthorized, uploadFile } from "../api/client";
import { fetchRelationList, followUser, loadFollowingMap } from "../api/social";
import type { RelationTab } from "../api/social";
import { RelationModal } from "../components/RelationModal";
import { VideoGrid } from "../components/VideoGrid";
import { WorkViewer } from "../components/WorkViewer";
import { emptyProfile, image } from "../constants";
import { useNavigate } from "../router";
import { updateSessionRelationCount, useSession } from "../session";
import { useDialogFocus } from "../hooks/useDialogFocus";
import type { RelationUser, SessionUser, Video } from "../types";
import { formatMetric } from "../utils";
import { Icon } from "../components/Icon";

interface ProfileForm {
  nickname: string;
  avatar_url: string;
  bio: string;
}

type RelationState = "idle" | "loading" | "loadingMore" | "ready" | "error";

export function ProfilePage() {
  const session = useSession();
  const navigate = useNavigate();
  const baseUser = session.user || emptyProfile;
  const [form, setForm] = useState<ProfileForm>({
    nickname: baseUser.nickname || "",
    avatar_url: baseUser.avatar_url || "",
    bio: baseUser.bio || ""
  });
  const [avatarFile, setAvatarFile] = useState<File | null>(null);
  const [avatarPreview, setAvatarPreview] = useState("");
  const [editing, setEditing] = useState(false);
  const [selectedWork, setSelectedWork] = useState<Video | null>(null);
  const [status, setStatus] = useState("");
  const [videos, setVideos] = useState<Video[]>([]);
  const [videosState, setVideosState] = useState("loading");
  const [relationTab, setRelationTab] = useState<RelationTab>("following");
  const [relationModalOpen, setRelationModalOpen] = useState(false);
  const [relationItems, setRelationItems] = useState<RelationUser[]>([]);
  const [relationCursor, setRelationCursor] = useState("");
  const [relationHasMore, setRelationHasMore] = useState(false);
  const [relationState, setRelationState] = useState<RelationState>("idle");
  const [relationError, setRelationError] = useState("");
  const [relationFollowing, setRelationFollowing] = useState<Record<number, boolean>>({});
  const [relationBusyID, setRelationBusyID] = useState(0);
  const editCloseButtonRef = useDialogFocus<HTMLButtonElement>(editing, () => setEditing(false));
  const followingCount = baseUser.following_count ?? baseUser.followingCount ?? 0;
  // SessionUser 没有 followerCount camelCase 副本（迁移前读取恒为 undefined），行为等价
  const followerCount = baseUser.follower_count ?? 0;

  useEffect(() => {
    setForm({
      nickname: baseUser.nickname || "",
      avatar_url: baseUser.avatar_url || "",
      bio: baseUser.bio || ""
    });
    setAvatarFile(null);
    setAvatarPreview("");
  }, [baseUser.avatar_url, baseUser.bio, baseUser.nickname]);

  useEffect(() => {
    if (!avatarFile) {
      setAvatarPreview("");
      return;
    }
    const objectURL = URL.createObjectURL(avatarFile);
    setAvatarPreview(objectURL);
    return () => URL.revokeObjectURL(objectURL);
  }, [avatarFile]);

  useEffect(() => {
    if (!session.token) {
      setVideosState("ready");
      return;
    }
    setVideosState("loading");
    fetchMyVideos(session.token)
      .then((data) => {
        setVideos(data.items || []);
        setVideosState("ready");
      })
      .catch((error: unknown) => {
        if (isUnauthorized(error)) {
          session.clearAuth();
          navigate("/auth");
          return;
        }
        setVideos([]);
        setVideosState(apiErrorMessage(error, "作品加载失败"));
      });
  }, [navigate, session]);

  useEffect(() => {
    if (!session.token) {
      setRelationItems([]);
      setRelationCursor("");
      setRelationHasMore(false);
      setRelationFollowing({});
      setRelationState("ready");
      return undefined;
    }

    let live = true;
    loadFollowingMap(session.token)
      .then((map) => {
        if (live) {
          setRelationFollowing(map);
        }
      })
      .catch((error: unknown) => {
        if (isUnauthorized(error)) {
          session.clearAuth();
          navigate("/auth");
        }
      });
    return () => {
      live = false;
    };
  }, [navigate, session]);

  const loadRelationPage = useCallback(
    async ({ reset = false, cursor = "" }: { reset?: boolean; cursor?: string } = {}) => {
      if (!session.token) return;
      const requestCursor = reset ? "" : cursor;
      setRelationState(reset ? "loading" : "loadingMore");
      setRelationError("");
      try {
        const data = await fetchRelationList(relationTab, session.token, requestCursor);
        const items = data.items || [];
        setRelationItems((state) => (reset ? items : [...state, ...items]));
        setRelationCursor(data.next_cursor || "");
        setRelationHasMore(Boolean(data.has_more));
        if (relationTab === "following") {
          setRelationFollowing((state) => {
            const next = { ...state };
            for (const item of items) {
              next[item.user_id] = true;
            }
            return next;
          });
        }
        setRelationState("ready");
      } catch (error) {
        if (isUnauthorized(error)) {
          session.clearAuth();
          navigate("/auth");
          return;
        }
        setRelationError(apiErrorMessage(error, "关系列表加载失败"));
        setRelationState("error");
      }
    },
    [navigate, relationTab, session]
  );

  useEffect(() => {
    setRelationItems([]);
    setRelationCursor("");
    setRelationHasMore(false);
    if (!session.token || !relationModalOpen) return;
    loadRelationPage({ reset: true });
  }, [loadRelationPage, relationModalOpen, relationTab, session.token]);

  function openRelationModal(tab: RelationTab) {
    setRelationTab(tab);
    setRelationModalOpen(true);
  }

  async function toggleRelationFollow(targetUserID: number) {
    if (!session.token) {
      navigate("/auth");
      return;
    }
    if (!targetUserID || targetUserID === baseUser.id) return;

    const currentFollowing = Boolean(relationFollowing[targetUserID]);
    setRelationBusyID(targetUserID);
    setRelationError("");
    try {
      const data = await followUser(session.token, targetUserID, !currentFollowing, "web-profile-follow");
      setRelationFollowing((state) => ({ ...state, [targetUserID]: Boolean(data.following) }));
      if (relationTab === "following" && !data.following) {
        setRelationItems((state) => state.filter((item) => item.user_id !== targetUserID));
      }
      updateSessionRelationCount(session, data.following_count);
      setRelationState("ready");
    } catch (error) {
      if (isUnauthorized(error)) {
        session.clearAuth();
        navigate("/auth");
        return;
      }
      setRelationError(apiErrorMessage(error, "关注操作失败"));
      setRelationState("error");
    } finally {
      setRelationBusyID(0);
    }
  }

  async function handleSave(event: FormEvent) {
    event.preventDefault();
    setStatus("保存中");
    try {
      let avatarURL = form.avatar_url;
      if (avatarFile && session.token) {
        const uploaded = await uploadFile(avatarFile, "avatar", session.token);
        avatarURL = uploaded.url;
      }
      let profile: SessionUser = { ...baseUser, ...form };
      if (session.token) {
        profile = await updateMyProfile(session.token, {
          nickname: form.nickname,
          avatar_url: avatarURL,
          bio: form.bio
        });
      }
      session.setAuth(session.token, profile);
      setAvatarFile(null);
      setStatus("已保存");
      setEditing(false);
    } catch (error) {
      setStatus(apiErrorMessage(error, "保存失败"));
    }
  }

  return (
    <main className="profile-page" data-ui="profile-page">
      <section className="profile-hero" data-ui="profile-hero">
        <div className="profile-summary">
          <img className="profile-avatar" src={avatarPreview || form.avatar_url || image.currentUser} alt="" />
          <div>
            <p className="eyebrow">创作者资料</p>
            <h1>{form.nickname || baseUser.account}</h1>
            <p>{form.bio || "作品、关注和互动资料会显示在这里。"}</p>
          </div>
          <div className="profile-stats" aria-label="资料统计">
            <button
              className={relationModalOpen && relationTab === "following" ? "active" : ""}
              type="button"
              onClick={() => openRelationModal("following")}
            >
              <strong>{formatMetric(followingCount)}</strong>
              关注
            </button>
            <button
              className={relationModalOpen && relationTab === "followers" ? "active" : ""}
              type="button"
              onClick={() => openRelationModal("followers")}
            >
              <strong>{formatMetric(followerCount)}</strong>
              粉丝
            </button>
            <button type="button">
              <strong>{formatMetric(videos.length)}</strong>
              作品
            </button>
          </div>
          <button className="profile-edit-button" onClick={() => setEditing(true)} aria-label="编辑资料">
            <Icon name="user-edit" />
          </button>
        </div>
      </section>

      <nav className="profile-tabs" aria-label="个人主页内容">
        <button className="active" type="button">
          作品 <span>{formatMetric(videos.length)}</span>
        </button>
        <button type="button" onClick={() => openRelationModal("following")}>
          关系
        </button>
        <button type="button" onClick={() => setEditing(true)}>
          编辑资料
        </button>
      </nav>

      <section className="profile-grid">
        <section className="profile-card works-card">
          <header>
            <h2>我的作品</h2>
            <button className="ghost-button compact" onClick={() => navigate("/timeline")}>
              <Icon name="home" size={17} />
              最新视频
            </button>
          </header>
          <VideoGrid videos={videos} state={videosState} onSelect={setSelectedWork} />
        </section>
      </section>
      {relationModalOpen && (
        <RelationModal
          tab={relationTab}
          items={relationItems}
          state={relationState}
          error={relationError}
          hasMore={relationHasMore}
          following={relationFollowing}
          busyID={relationBusyID}
          currentUserID={baseUser.id}
          onTabChange={setRelationTab}
          onClose={() => setRelationModalOpen(false)}
          onRetry={() => loadRelationPage({ reset: true })}
          onLoadMore={() => loadRelationPage({ reset: false, cursor: relationCursor })}
          onToggleFollow={toggleRelationFollow}
        />
      )}
      {selectedWork && <WorkViewer video={selectedWork} onClose={() => setSelectedWork(null)} />}
      {editing && (
        <div className="modal-backdrop" role="presentation">
          <form aria-modal="true" className="profile-modal profile-form" role="dialog" onSubmit={handleSave}>
            <header>
              <h2>资料编辑</h2>
              <button
                ref={editCloseButtonRef}
                className="icon-button small"
                type="button"
                onClick={() => setEditing(false)}
                aria-label="关闭"
              >
                <Icon name="close" size={19} />
              </button>
            </header>
            <label>
              <span>昵称</span>
              <input value={form.nickname} onChange={(event) => setForm({ ...form, nickname: event.target.value })} />
            </label>
            <label>
              <span>头像</span>
              <span className="file-picker avatar-picker">
                <span className="avatar-upload-preview">
                  {avatarPreview || form.avatar_url ? (
                    <img src={avatarPreview || form.avatar_url} alt="" />
                  ) : (
                    <Icon name="user" />
                  )}
                </span>
                <span className="file-picker-copy">
                  <strong>{avatarFile ? avatarFile.name : "选择头像文件"}</strong>
                  <small>本地图片上传</small>
                </span>
                <input type="file" accept="image/*" onChange={(event) => setAvatarFile(event.target.files?.[0] || null)} />
              </span>
            </label>
            <label>
              <span>简介</span>
              <textarea value={form.bio} onChange={(event) => setForm({ ...form, bio: event.target.value })} rows={4} />
            </label>
            {status && <p className={`form-message ${status === "已保存" ? "success" : ""}`}>{status}</p>}
            <button className="primary-button">
              <Icon name="save" size={18} />
              保存
            </button>
          </form>
        </div>
      )}
    </main>
  );
}
