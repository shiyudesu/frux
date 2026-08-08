import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { fetchMyProfile, updateMyProfile } from "../api/account";
import { resolveCreatorVideoTarget } from "../api/creator";
import { apiErrorMessage, isUnauthorized, uploadFile } from "../api/client";
import { fetchRelationList, followUser, loadFollowingMap } from "../api/social";
import type { RelationTab } from "../api/social";
import { ProfileCollectionEditor } from "../components/ProfileCollectionEditor";
import {
  CreatorWorkTabs,
  CreatorWorkToolbar,
  ProfileCollectionGrid,
  ProfileHero,
  ProfilePrimaryTabs,
  ProfileVideoGrid
} from "../components/ProfileDashboard";
import type { ProfileGridItem } from "../components/ProfileDashboard";
import { ProfileEditor } from "../components/ProfileEditor";
import type { ProfileEditorValue } from "../components/ProfileEditor";
import { RelationModal } from "../components/RelationModal";
import { WorkViewer } from "../components/WorkViewer";
import { CollectionQueueViewer } from "../components/CollectionQueueViewer";
import { emptyProfile } from "../constants";
import { useCreatorContent } from "../hooks/useCreatorContent";
import type { CreatorFilters } from "../hooks/useCreatorContent";
import { useProfileLibrary } from "../hooks/useProfileLibrary";
import type { ProfileLibraryTab } from "../hooks/useProfileLibrary";
import { useNavigate, useProfileVideoTarget } from "../router";
import { updateSessionRelationCount, useSession } from "../session";
import type {
  BatchVideoAction,
  CreatorWorkTab,
  ProfilePrimaryTab,
  RelationUser,
  Video,
  VideoCollection
} from "../types";

type RelationState = "idle" | "loading" | "loadingMore" | "ready" | "error";

export const PROFILE_PRIMARY_TABS = [
  { id: "works", label: "作品" },
  { id: "likes", label: "喜欢" },
  { id: "favorites", label: "收藏" },
  { id: "history", label: "观看历史" },
  { id: "watchLater", label: "稍后再看" }
] satisfies Array<{ id: ProfilePrimaryTab; label: string }>;

const defaultFilters: Record<"published" | "private", CreatorFilters> = {
  published: { query: "", createdFrom: "", createdTo: "" },
  private: { query: "", createdFrom: "", createdTo: "" }
};

export function ProfilePage() {
  const session = useSession();
  const navigate = useNavigate();
  const targetVideoID = useProfileVideoTarget();
  const { clearAuth, token, updateUser } = session;
  const profileRequest = useRef(0);
  const refreshCurrentProfile = useCallback(async () => {
    if (!token) return;
    const requestID = profileRequest.current + 1;
    profileRequest.current = requestID;
    try {
      const profile = await fetchMyProfile(token);
      if (profileRequest.current === requestID) {
        updateUser(token, profile);
      }
    } catch (error) {
      if (profileRequest.current === requestID && isUnauthorized(error)) {
        clearAuth();
        navigate("/auth");
      }
    }
  }, [clearAuth, navigate, token, updateUser]);
  const baseUser = session.user || emptyProfile;
  const creator = useCreatorContent(token, refreshCurrentProfile);
  const library = useProfileLibrary(token);
  const [primaryTab, setPrimaryTab] = useState<ProfilePrimaryTab>("works");
  const [workTab, setWorkTab] = useState<CreatorWorkTab>("published");
  const [filters, setFilters] = useState(defaultFilters);
  const [selectionMode, setSelectionMode] = useState(false);
  const [selectedIDs, setSelectedIDs] = useState<Set<number>>(new Set());
  const [selectedWork, setSelectedWork] = useState<Video | null>(null);
  const [libraryQueue, setLibraryQueue] = useState<{ source: ProfileLibraryTab; videoID: number } | null>(null);
  const [editing, setEditing] = useState(false);
  const [targetWork, setTargetWork] = useState<{
    video: Video;
    tab: "published" | "private";
  } | null>(null);
  const [targetWorkState, setTargetWorkState] = useState<
    "idle" | "loading" | "ready" | "unavailable"
  >("idle");
  const [targetRevision, setTargetRevision] = useState(0);
  const [editorBusy, setEditorBusy] = useState(false);
  const [editorMessage, setEditorMessage] = useState("");
  const [editingCollectionID, setEditingCollectionID] = useState<number | null | "new">(null);
  const [collectionBusy, setCollectionBusy] = useState(false);
  const [collectionMessage, setCollectionMessage] = useState("");
  const [relationTab, setRelationTab] = useState<RelationTab>("following");
  const [relationModalOpen, setRelationModalOpen] = useState(false);
  const [relationItems, setRelationItems] = useState<RelationUser[]>([]);
  const [relationCursor, setRelationCursor] = useState("");
  const [relationHasMore, setRelationHasMore] = useState(false);
  const [relationState, setRelationState] = useState<RelationState>("idle");
  const [relationError, setRelationError] = useState("");
  const [relationFollowing, setRelationFollowing] = useState<Record<number, boolean>>({});
  const [relationBusyID, setRelationBusyID] = useState(0);
  const relationRequest = useRef(0);
  const relationFollowingRequest = useRef(0);
  useEffect(() => {
    if (targetVideoID <= 0 || !token) {
      setTargetWork(null);
      setTargetWorkState("idle");
      return;
    }
    let active = true;
    setPrimaryTab("works");
    setTargetWork(null);
    setTargetWorkState("loading");
    void resolveCreatorVideoTarget(token, targetVideoID).then((match) => {
      if (!active) return;
      if (!match) {
        setTargetWorkState("unavailable");
        return;
      }
      setWorkTab(match.tab);
      setTargetWork(match);
      setTargetWorkState("ready");
    }).catch(() => {
      if (active) setTargetWorkState("unavailable");
    });
    return () => {
      active = false;
    };
  }, [targetRevision, targetVideoID, token]);
  const relationTabRef = useRef<RelationTab>(relationTab);
  const relationModalOpenRef = useRef(relationModalOpen);
  relationTabRef.current = relationTab;
  relationModalOpenRef.current = relationModalOpen;

  useEffect(() => {
    void refreshCurrentProfile();
    return () => {
      profileRequest.current += 1;
    };
  }, [refreshCurrentProfile]);

  useEffect(() => {
    if (primaryTab === "works") creator.ensureTab(workTab);
    else library.ensureTab(primaryTab);
  }, [creator.ensureTab, library.ensureTab, primaryTab, workTab]);

  useEffect(() => {
    setSelectionMode(false);
    setSelectedIDs(new Set());
  }, [workTab]);

  useEffect(() => {
    if (!token) return undefined;
    const requestID = relationFollowingRequest.current + 1;
    relationFollowingRequest.current = requestID;
    loadFollowingMap(token)
      .then((map) => {
        if (relationFollowingRequest.current === requestID) setRelationFollowing(map);
      })
      .catch((error: unknown) => {
        if (relationFollowingRequest.current !== requestID) return;
        if (isUnauthorized(error)) {
          clearAuth();
          navigate("/auth");
        }
      });
    return () => {
      if (relationFollowingRequest.current === requestID) relationFollowingRequest.current += 1;
    };
  }, [clearAuth, navigate, token]);

  const loadRelationPage = useCallback(
    async ({ reset = false, cursor = "" }: { reset?: boolean; cursor?: string } = {}) => {
      if (!token || !relationModalOpenRef.current) return;
      const tab = relationTabRef.current;
      const requestID = relationRequest.current + 1;
      relationRequest.current = requestID;
      setRelationState(reset ? "loading" : "loadingMore");
      setRelationError("");
      try {
        const data = await fetchRelationList(tab, token, reset ? "" : cursor);
        if (
          relationRequest.current !== requestID
          || !relationModalOpenRef.current
          || relationTabRef.current !== tab
        ) return;
        const items = data.items || [];
        setRelationItems((state) => (reset ? items : [...state, ...items]));
        setRelationCursor(data.next_cursor || "");
        setRelationHasMore(Boolean(data.has_more));
        if (tab === "following") {
          setRelationFollowing((state) => {
            const next = { ...state };
            for (const item of items) next[item.user_id] = true;
            return next;
          });
        }
        setRelationState("ready");
      } catch (error) {
        if (
          relationRequest.current !== requestID
          || !relationModalOpenRef.current
          || relationTabRef.current !== tab
        ) return;
        if (isUnauthorized(error)) {
          clearAuth();
          navigate("/auth");
          return;
        }
        setRelationError(apiErrorMessage(error, "关系列表加载失败"));
        setRelationState("error");
      }
    },
    [clearAuth, navigate, token]
  );

  useEffect(() => {
    relationRequest.current += 1;
    setRelationItems([]);
    setRelationCursor("");
    setRelationHasMore(false);
    if (relationModalOpen) void loadRelationPage({ reset: true });
    return () => {
      relationRequest.current += 1;
    };
  }, [loadRelationPage, relationModalOpen, relationTab]);

  function openRelationModal(tab: RelationTab) {
    relationRequest.current += 1;
    relationTabRef.current = tab;
    relationModalOpenRef.current = true;
    setRelationTab(tab);
    setRelationModalOpen(true);
  }

  function closeRelationModal() {
    relationRequest.current += 1;
    relationModalOpenRef.current = false;
    setRelationModalOpen(false);
  }

  function changeRelationTab(tab: RelationTab) {
    if (tab === relationTabRef.current) return;
    relationRequest.current += 1;
    relationTabRef.current = tab;
    setRelationTab(tab);
  }

  async function toggleRelationFollow(targetUserID: number) {
    if (!token || targetUserID === baseUser.id) return;
    const currentFollowing = Boolean(relationFollowing[targetUserID]);
    relationFollowingRequest.current += 1;
    setRelationBusyID(targetUserID);
    setRelationError("");
    try {
      const data = await followUser(token, targetUserID, !currentFollowing, "web-profile-follow");
      setRelationFollowing((state) => ({ ...state, [targetUserID]: Boolean(data.following) }));
      if (relationTab === "following" && !data.following) {
        setRelationItems((state) => state.filter((item) => item.user_id !== targetUserID));
      }
      updateSessionRelationCount(session, data.following_count);
      setRelationState("ready");
    } catch (error) {
      setRelationError(apiErrorMessage(error, "关注操作失败"));
      setRelationState("error");
    } finally {
      setRelationBusyID(0);
    }
  }

  const hero = {
    account: baseUser.account,
    nickname: baseUser.nickname,
    avatarURL: baseUser.avatar_url,
    bio: baseUser.bio,
    gender: baseUser.gender,
    followingCount: baseUser.following_count ?? baseUser.followingCount ?? 0,
    followerCount: baseUser.follower_count,
    workCount: baseUser.public_work_count,
    receivedLikeCount: baseUser.received_like_count
  };

  const editorValue = useMemo<ProfileEditorValue>(() => ({
    nickname: baseUser.nickname,
    avatarURL: baseUser.avatar_url,
    bio: baseUser.bio,
    gender: baseUser.gender,
    settings: {
      liked_visibility: baseUser.profile_settings?.liked_visibility || "private",
      favorite_visibility: "private"
    }
  }), [
    baseUser.avatar_url,
    baseUser.bio,
    baseUser.gender,
    baseUser.nickname,
    baseUser.profile_settings?.liked_visibility
  ]);

  async function saveProfile(value: ProfileEditorValue, avatarFile: File | null) {
    if (!token) return;
    setEditorBusy(true);
    setEditorMessage("");
    try {
      let avatarURL = value.avatarURL;
      if (avatarFile) {
        avatarURL = (await uploadFile(avatarFile, "avatar", token)).url;
      }
      const profile = await updateMyProfile(token, {
        nickname: value.nickname,
        avatar_url: avatarURL,
        bio: value.bio,
        gender: value.gender,
        profile_settings: {
          liked_visibility: value.settings.liked_visibility,
          favorite_visibility: "private"
        }
      });
      profileRequest.current += 1;
      updateUser(token, profile);
      setEditing(false);
    } catch (error) {
      setEditorMessage(apiErrorMessage(error, "保存失败"));
    } finally {
      setEditorBusy(false);
    }
  }

  function updateFilter(field: keyof CreatorFilters, value: string) {
    if (workTab === "collections") return;
    setFilters((state) => ({ ...state, [workTab]: { ...state[workTab], [field]: value } }));
  }

  function applyFilters() {
    if (workTab === "collections") return;
    void creator.loadVideos(workTab, { reset: true, filters: filters[workTab] });
  }

  function toggleSelected(videoID: number) {
    setSelectedIDs((current) => {
      const next = new Set(current);
      if (next.has(videoID)) next.delete(videoID);
      else next.add(videoID);
      return next;
    });
  }

  async function runBatch(action: BatchVideoAction) {
    if (workTab === "collections" || selectedIDs.size === 0) return;
    if (action === "delete" && !window.confirm("确定删除所选作品吗？")) return;
    const affectedTarget = targetVideoID > 0 && selectedIDs.has(targetVideoID);
    await creator.runBatchAction(workTab, [...selectedIDs], action);
    if (affectedTarget) {
      setTargetWork(null);
      setTargetRevision((current) => current + 1);
    }
    setSelectedIDs(new Set());
    setSelectionMode(false);
  }

  const collectionEditor = useMemo<VideoCollection | null>(() => {
    if (editingCollectionID === null || editingCollectionID === "new") return null;
    return creator.collections.items.find((collection) => collection.id === editingCollectionID) || null;
  }, [creator.collections.items, editingCollectionID]);

  async function saveCollection(body: {
    title?: string;
    description?: string;
    visibility?: "public" | "private";
  }) {
    setCollectionBusy(true);
    setCollectionMessage("");
    try {
      if (editingCollectionID === "new") {
        await creator.createCollection({
          title: body.title || "",
          description: body.description || "",
          visibility: body.visibility || "public"
        });
      } else if (collectionEditor) {
        await creator.editCollection(collectionEditor.id, body);
      }
      setEditingCollectionID(null);
    } catch (error) {
      setCollectionMessage(apiErrorMessage(error, "合集保存失败"));
    } finally {
      setCollectionBusy(false);
    }
  }

  async function deleteCollection() {
    if (!collectionEditor || !window.confirm("确定删除这个合集吗？")) return;
    setCollectionBusy(true);
    try {
      await creator.removeCollection(collectionEditor.id);
      setEditingCollectionID(null);
    } catch (error) {
      setCollectionMessage(apiErrorMessage(error, "合集删除失败"));
    } finally {
      setCollectionBusy(false);
    }
  }

  function openCollectionEditor(collection: VideoCollection | null) {
    setCollectionMessage("");
    setEditingCollectionID(collection ? collection.id : "new");
    if (collection) void creator.loadCollectionVideos("", true);
  }

  function renderWorks() {
    if (workTab === "collections") {
      return (
        <ProfileCollectionGrid
          collections={creator.collections.items}
          error={creator.collections.error}
          hasMore={creator.collections.hasMore}
          owner
          state={creator.collections.state}
          onCreate={() => openCollectionEditor(null)}
          onLoadMore={() => void creator.loadCollections(false)}
          onManage={openCollectionEditor}
          onOpenVideo={setSelectedWork}
          onRetry={() => void creator.loadCollections(true)}
        />
      );
    }
    const current = creator.videos[workTab];
    const currentItems = current.items.some((video) => video.id === targetWork?.video.id)
      ? current.items
      : targetWork?.tab === workTab
        ? [targetWork.video, ...current.items]
        : current.items;
    const draft = filters[workTab];
    return (
      <>
        <CreatorWorkToolbar
          busy={current.state === "mutating"}
          createdFrom={draft.createdFrom}
          createdTo={draft.createdTo}
          query={draft.query}
          selectedCount={selectedIDs.size}
          selectionMode={selectionMode}
          onBatchDelete={() => void runBatch("delete")}
          onBatchPrivate={() => void runBatch("make_private")}
          onBatchPublic={() => void runBatch("make_public")}
          onCreatedFromChange={(value) => updateFilter("createdFrom", value)}
          onCreatedToChange={(value) => updateFilter("createdTo", value)}
          onQueryChange={(value) => updateFilter("query", value)}
          onSubmit={applyFilters}
          onToggleSelection={() => {
            setSelectionMode((value) => !value);
            setSelectedIDs(new Set());
          }}
        />
        <ProfileVideoGrid
          emptyDescription={workTab === "published" ? "公开、待审和未通过作品会显示在这里" : "设为私密的作品会显示在这里"}
          emptyTitle={workTab === "published" ? "暂无公开作品" : "暂无私密作品"}
          error={current.error}
          hasMore={current.hasMore}
          items={currentItems.map((video) => ({ video }))}
          selectedIDs={selectedIDs}
          selectionMode={selectionMode}
          state={current.state}
          statusLabels
          targetVideoID={targetVideoID}
          protectedCoverToken={token}
          onLoadMore={() => void creator.loadVideos(workTab)}
          onRetry={() => void creator.loadVideos(workTab, { reset: true })}
          onSelect={setSelectedWork}
          onToggleSelected={toggleSelected}
        />
        {targetWorkState === "loading" && <p className="profile-target-message">正在定位目标作品…</p>}
        {targetWorkState === "unavailable" && (
          <p className="profile-target-message">目标作品已删除或暂不可用。</p>
        )}
      </>
    );
  }

  function renderLibrary() {
    if (primaryTab === "works") return null;
    const current = library.tabs[primaryTab];
    const labels: Record<typeof primaryTab, [string, string]> = {
      likes: ["暂无喜欢作品", "点赞过的作品会显示在这里"],
      favorites: ["暂无收藏作品", "收藏过的作品会显示在这里"],
      history: ["暂无观看历史", "观看过的作品会显示在这里"],
      watchLater: ["暂无稍后再看", "加入稍后再看的作品会显示在这里"]
    };
    const action =
      primaryTab === "history"
        ? (item: ProfileGridItem) => void library.removeHistory(item.video.id)
        : primaryTab === "watchLater"
          ? (item: ProfileGridItem) => void library.removeWatchLater(item.video.id)
          : undefined;
    return (
      <ProfileVideoGrid
        emptyDescription={labels[primaryTab][1]}
        emptyTitle={labels[primaryTab][0]}
        error={current.error}
        hasMore={current.hasMore}
        itemAction={action}
        itemActionLabel={primaryTab === "history" ? "从观看历史移除" : "从稍后再看移除"}
        items={current.items}
        state={current.state}
        onLoadMore={() => void library.loadTab(primaryTab)}
        onRetry={() => void library.loadTab(primaryTab, true)}
        onSelect={(video) => setLibraryQueue({ source: primaryTab, videoID: video.id })}
      />
    );
  }

  return (
    <main className="profile-page" data-ui="profile-page">
      <ProfileHero
        owner
        profile={hero}
        onEdit={() => setEditing(true)}
        onOpenFollowers={() => openRelationModal("followers")}
        onOpenFollowing={() => openRelationModal("following")}
      />
      <section className="profile-content">
        <ProfilePrimaryTabs
          active={primaryTab}
          tabs={PROFILE_PRIMARY_TABS.map((tab) => tab.id === "works" ? { ...tab, count: baseUser.public_work_count } : tab)}
          actions={
            primaryTab === "history" && library.tabs.history.items.length > 0 ? (
              <button className="profile-manage-button" type="button" onClick={() => void library.clearHistory()}>
                清空历史
              </button>
            ) : undefined
          }
          onChange={setPrimaryTab}
        />
        <section
          id={`profile-panel-${primaryTab}`}
          aria-labelledby={`profile-tab-${primaryTab}`}
          className="profile-tab-panel"
          role="tabpanel"
          tabIndex={0}
        >
          {primaryTab === "works" ? (
            <>
              <CreatorWorkTabs active={workTab} onChange={setWorkTab} />
              {renderWorks()}
            </>
          ) : renderLibrary()}
        </section>
      </section>
      {relationModalOpen && (
        <RelationModal
          busyID={relationBusyID}
          currentUserID={baseUser.id}
          error={relationError}
          following={relationFollowing}
          hasMore={relationHasMore}
          items={relationItems}
          state={relationState}
          tab={relationTab}
          onClose={closeRelationModal}
          onLoadMore={() => void loadRelationPage({ cursor: relationCursor })}
          onRetry={() => void loadRelationPage({ reset: true })}
          onTabChange={changeRelationTab}
          onToggleFollow={(userID) => void toggleRelationFollow(userID)}
        />
      )}
      {selectedWork && (
        <WorkViewer
          video={selectedWork}
          token={token}
          onClose={() => setSelectedWork(null)}
        />
      )}
      {libraryQueue && (
        <CollectionQueueViewer
          source={libraryQueue.source}
          sourceState={library.tabs[libraryQueue.source]}
          selectedVideoID={libraryQueue.videoID}
          onClose={() => setLibraryQueue(null)}
          onLoadMore={() => void library.loadTab(libraryQueue.source)}
          onPatchVideo={(videoID, patch) => library.patchVideo(libraryQueue.source, videoID, patch)}
          onApplyVideoAction={(videoID, action, active, counts) => {
            library.applyVideoAction(libraryQueue.source, videoID, action, active, counts);
          }}
          onAddWatchLater={library.addWatchLater}
          onRemoveWatchLater={library.removeWatchLater}
        />
      )}
      {editing && (
        <ProfileEditor
          key={baseUser.id}
          busy={editorBusy}
          message={editorMessage}
          value={editorValue}
          onClose={() => setEditing(false)}
          onSave={saveProfile}
        />
      )}
      {editingCollectionID !== null && (
        <ProfileCollectionEditor
          availableVideos={creator.collectionVideos.items}
          availableVideosError={creator.collectionVideos.error}
          availableVideosHasMore={
            creator.collectionVideos.pages.public.hasMore
            || creator.collectionVideos.pages.private.hasMore
          }
          availableVideosLoading={
            creator.collectionVideos.state === "loading"
            || creator.collectionVideos.state === "loadingMore"
          }
          busy={collectionBusy}
          collection={collectionEditor}
          message={collectionMessage}
          onClose={() => setEditingCollectionID(null)}
          onDelete={collectionEditor ? deleteCollection : undefined}
          onLoadMoreAvailableVideos={async () => {
            await creator.loadCollectionVideos();
          }}
          onSave={saveCollection}
          onSearchAvailableVideos={async (query) => {
            await creator.loadCollectionVideos(query, true);
          }}
          onSetMembership={
            collectionEditor
              ? async (videoID, active) => {
                  setCollectionBusy(true);
                  try {
                    await creator.setMembership(collectionEditor.id, videoID, active);
                  } catch (error) {
                    setCollectionMessage(apiErrorMessage(error, "合集作品更新失败"));
                  } finally {
                    setCollectionBusy(false);
                  }
                }
              : undefined
          }
        />
      )}
    </main>
  );
}
