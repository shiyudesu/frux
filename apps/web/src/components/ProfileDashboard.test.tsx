// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fetchProtectedAssetAccess } from "../api/upload";
import type { Video } from "../types";
import { ProfileHero, ProfileVideoGrid } from "./ProfileDashboard";

vi.mock("../api/upload", () => ({
  fetchProtectedAssetAccess: vi.fn()
}));

describe("ProfileVideoGrid protected covers", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    vi.mocked(fetchProtectedAssetAccess).mockReset();
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
  });

  it("loads a pending owner's protected cover", async () => {
    vi.mocked(fetchProtectedAssetAccess).mockResolvedValue({
      url: "https://protected.example/cover.jpg",
      expires_at: "2099-01-01T00:00:00Z"
    });

    await act(async () => root.render(
      <ProfileVideoGrid
        emptyTitle="暂无作品"
        items={[{ video: pendingVideo() }]}
        protectedCoverToken="owner-token"
        state="ready"
        onSelect={() => {}}
      />
    ));
    await flush();

    expect(fetchProtectedAssetAccess).toHaveBeenCalledWith("owner-token", 12);
    expect(container.querySelector<HTMLImageElement>(".profile-video-cover img")?.src)
      .toBe("https://protected.example/cover.jpg");
  });

  it("shows account only for owners and preserves public gender and neutral fallback", () => {
    const shared = {
      id: 7,
      nickname: "Owner",
      avatarURL: "",
      bio: "",
      gender: 1 as const,
      followingCount: 0,
      followerCount: 0,
      workCount: 0,
      receivedLikeCount: 0
    };
    act(() => root.render(<ProfileHero owner profile={{ ...shared, account: "owner-login" }} />));
    expect(container.textContent).toContain("账号：owner-login");

    act(() => root.render(
      <ProfileHero profile={{ ...shared, id: 9, nickname: "", gender: 2 }} />
    ));
    expect(container.textContent).toContain("用户_9");
    expect(container.textContent).toContain("女");
    expect(container.textContent).not.toContain("账号：");
    expect(container.textContent).not.toContain("owner-login");
  });

  it("does not request protected access for public grids", async () => {
    await act(async () => root.render(
      <ProfileVideoGrid
        emptyTitle="暂无作品"
        items={[{ video: pendingVideo() }]}
        state="ready"
        onSelect={() => {}}
      />
    ));

    expect(fetchProtectedAssetAccess).not.toHaveBeenCalled();
    expect(container.innerHTML).not.toContain("protected.example");
  });

  it("drops a resolved protected cover when the owner token is removed", async () => {
    vi.mocked(fetchProtectedAssetAccess).mockResolvedValue({
      url: "https://protected.example/cover.jpg",
      expires_at: "2099-01-01T00:00:00Z"
    });
    const video = pendingVideo();

    await act(async () => root.render(
      <ProfileVideoGrid
        emptyTitle="暂无作品"
        items={[{ video }]}
        protectedCoverToken="owner-token"
        state="ready"
        onSelect={() => {}}
      />
    ));
    await flush();
    await act(async () => root.render(
      <ProfileVideoGrid
        emptyTitle="暂无作品"
        items={[{ video }]}
        state="ready"
        onSelect={() => {}}
      />
    ));

    expect(container.innerHTML).not.toContain("protected.example");
  });

  it("does not reuse a protected cover after the owner token changes", async () => {
    vi.mocked(fetchProtectedAssetAccess)
      .mockResolvedValueOnce({
        url: "https://protected.example/first-owner.jpg",
        expires_at: "2099-01-01T00:00:00Z"
      })
      .mockImplementationOnce(() => new Promise(() => {}));
    const video = pendingVideo();

    await act(async () => root.render(
      <ProfileVideoGrid
        emptyTitle="暂无作品"
        items={[{ video }]}
        protectedCoverToken="first-owner-token"
        state="ready"
        onSelect={() => {}}
      />
    ));
    await flush();
    await act(async () => root.render(
      <ProfileVideoGrid
        emptyTitle="暂无作品"
        items={[{ video }]}
        protectedCoverToken="second-owner-token"
        state="ready"
        onSelect={() => {}}
      />
    ));

    expect(container.innerHTML).not.toContain("first-owner.jpg");
    expect(fetchProtectedAssetAccess).toHaveBeenLastCalledWith("second-owner-token", 12);
  });
});

function pendingVideo(): Video {
  return {
    id: 1,
    author_id: 7,
    title: "待审作品",
    description: "",
    media_url: "",
    cover_url: "",
    status: 5,
    visibility: "public",
    like_count: 0,
    comment_count: 0,
    favorite_count: 0,
    created_at: "2026-08-08T00:00:00Z",
    updated_at: "2026-08-08T00:00:00Z",
    media_asset_id: 11,
    cover_asset_id: 12,
    media_status: "processing"
  };
}

async function flush() {
  await Promise.resolve();
  await Promise.resolve();
}
