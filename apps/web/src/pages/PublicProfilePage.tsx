// 公开用户主页（/users/:id）：公开资料 + TA 的作品。
import { useEffect, useState } from "react";
import { fetchPublicProfile, fetchUserVideos } from "../api/account";
import { apiErrorMessage } from "../api/client";
import { VideoGrid } from "../components/VideoGrid";
import { WorkViewer } from "../components/WorkViewer";
import { image } from "../constants";
import { useNavigate } from "../router";
import type { StoredPublicProfile, Video } from "../types";
import { formatOptionalMetric, normalizePublicProfile, readPublicProfile, savePublicProfile } from "../utils";

export function PublicProfilePage({ userID }: { userID: number }) {
  const navigate = useNavigate();
  const [profile, setProfile] = useState<StoredPublicProfile | null>(() => readPublicProfile(userID));
  const [profileState, setProfileState] = useState("idle");
  const [videos, setVideos] = useState<Video[]>([]);
  const [videosState, setVideosState] = useState("loading");
  const [selectedWork, setSelectedWork] = useState<Video | null>(null);

  useEffect(() => {
    setProfile(readPublicProfile(userID));
  }, [userID]);

  useEffect(() => {
    let live = true;
    setProfileState("loading");
    fetchPublicProfile(userID)
      .then((data) => {
        if (!live) return;
        const nextProfile = normalizePublicProfile(data);
        setProfile(nextProfile);
        if (nextProfile) {
          savePublicProfile(nextProfile);
        }
        setProfileState("ready");
      })
      .catch((error: unknown) => {
        if (!live) return;
        setProfileState(apiErrorMessage(error, "资料加载失败"));
      });
    return () => {
      live = false;
    };
  }, [userID]);

  useEffect(() => {
    let live = true;
    setVideosState("loading");
    fetchUserVideos(userID)
      .then((data) => {
        if (!live) return;
        setVideos(data.items || []);
        setVideosState("ready");
      })
      .catch((error: unknown) => {
        if (!live) return;
        setVideos([]);
        setVideosState(apiErrorMessage(error, "作品加载失败"));
      });
    return () => {
      live = false;
    };
  }, [userID]);

  const displayProfile: StoredPublicProfile = profile || {
    id: userID,
    nickname: `用户_${userID}`,
    avatar_url: image.currentUser,
    bio: "这个用户的资料会显示在这里。"
  };

  return (
    <main className="profile-page">
      <section className="profile-hero">
        <div className="profile-summary public-profile-summary">
          <img className="profile-avatar" src={displayProfile.avatar_url || image.currentUser} alt="" />
          <div>
            <p className="eyebrow">用户主页</p>
            <h1>{displayProfile.nickname || `用户_${userID}`}</h1>
            <p>{displayProfile.bio || "这个用户还没有填写简介。"}</p>
            {profileState !== "idle" && profileState !== "loading" && profileState !== "ready" && (
              <p className="form-message">{profileState}</p>
            )}
          </div>
          <div className="profile-stats public-profile-stats" aria-label="资料统计">
            <button type="button">
              <strong>{formatOptionalMetric(displayProfile.following_count)}</strong>
              关注
            </button>
            <button type="button">
              <strong>{formatOptionalMetric(displayProfile.follower_count)}</strong>
              粉丝
            </button>
            <button type="button">
              <strong>{formatOptionalMetric(displayProfile.work_count)}</strong>
              作品
            </button>
          </div>
          <button className="ghost-button compact public-back-button" type="button" onClick={() => navigate("/timeline")}>
            <span className="material-symbols-outlined">home</span>
            最新视频
          </button>
        </div>
      </section>

      <section className="profile-grid">
        <section className="profile-card works-card">
          <header>
            <h2>他的作品</h2>
          </header>
          <VideoGrid videos={videos} state={videosState} onSelect={setSelectedWork} />
        </section>
      </section>
      {selectedWork && <WorkViewer video={selectedWork} onClose={() => setSelectedWork(null)} />}
    </main>
  );
}
