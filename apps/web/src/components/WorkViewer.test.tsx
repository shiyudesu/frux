// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fetchProtectedAssetAccess } from "../api/upload";
import type { Video } from "../types";
import { WorkViewer } from "./WorkViewer";

vi.mock("../api/upload", () => ({
  fetchProtectedAssetAccess: vi.fn()
}));

describe("protected WorkViewer", () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.useRealTimers();
    vi.resetAllMocks();
  });

  it("loads protected media and cover for an owned pending work", async () => {
    vi.mocked(fetchProtectedAssetAccess)
      .mockResolvedValueOnce({
        url: "https://protected.example/baseline.mp4",
        expires_at: "2099-01-01T00:00:00Z"
      })
      .mockResolvedValueOnce({
        url: "https://protected.example/cover.jpg",
        expires_at: "2099-01-01T00:00:00Z"
      });
    await act(async () => root.render(
      <WorkViewer video={pendingVideo()} token="owner-token" onClose={() => {}} />
    ));
    await flush();
    const video = container.querySelector<HTMLVideoElement>(".work-viewer-stage video");
    expect(video?.src).toContain("https://protected.example/baseline.mp4");
    expect(video?.poster).toContain("https://protected.example/cover.jpg");
    expect(fetchProtectedAssetAccess).toHaveBeenNthCalledWith(1, "owner-token", 11);
    expect(fetchProtectedAssetAccess).toHaveBeenNthCalledWith(2, "owner-token", 12);
  });

  it("keeps a cover-only preview and offers retry when media access fails", async () => {
    vi.mocked(fetchProtectedAssetAccess)
      .mockRejectedValueOnce(new Error("media denied"))
      .mockResolvedValueOnce({
        url: "https://protected.example/cover.jpg",
        expires_at: "2099-01-01T00:00:00Z"
      });
    await act(async () => root.render(
      <WorkViewer video={pendingVideo()} token="owner-token" onClose={() => {}} />
    ));
    await flush();
    expect(container.querySelector(".work-viewer-stage img")).not.toBeNull();
    expect(container.textContent).toContain("作品预览暂时不可用");
    expect(container.textContent).toContain("重试");
  });

  it("does not request protected access for a public viewer", async () => {
    const video = pendingVideo({
      media_url: "https://cdn.example/public.mp4",
      cover_url: "https://cdn.example/cover.jpg"
    });
    await act(async () => root.render(
      <WorkViewer video={video} onClose={() => {}} />
    ));
    expect(fetchProtectedAssetAccess).not.toHaveBeenCalled();
    expect(container.querySelector<HTMLVideoElement>("video")?.src)
      .toContain("https://cdn.example/public.mp4");
  });

  it("reports browser playback failure without discarding the cover", async () => {
    vi.mocked(fetchProtectedAssetAccess)
      .mockResolvedValueOnce({
        url: "https://protected.example/source.mov",
        expires_at: "2099-01-01T00:00:00Z"
      })
      .mockResolvedValueOnce({
        url: "https://protected.example/cover.jpg",
        expires_at: "2099-01-01T00:00:00Z"
      })
      .mockResolvedValueOnce({
        url: "https://protected.example/ready-baseline.mp4",
        expires_at: "2099-01-01T00:05:00Z"
      });
    await act(async () => root.render(
      <WorkViewer video={pendingVideo()} token="owner-token" onClose={() => {}} />
    ));
    await flush();
    const video = container.querySelector<HTMLVideoElement>("video");
    await act(async () => video?.dispatchEvent(new Event("error")));
    expect(container.textContent).toContain("当前源暂时无法在浏览器中播放");
    const retry = [...container.querySelectorAll("button")]
      .find((item) => item.textContent === "重新获取预览");
    await act(async () => retry?.click());
    await flush();
    expect(container.querySelector<HTMLVideoElement>("video")?.src)
      .toContain("https://protected.example/ready-baseline.mp4");
  });

  it("keeps the earlier retained expiry when one background refresh fails", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-07T12:00:00Z"));
    vi.mocked(fetchProtectedAssetAccess)
      .mockResolvedValueOnce({
        url: "https://protected.example/initial.mp4",
        expires_at: "2026-08-07T12:00:40Z"
      })
      .mockResolvedValueOnce({
        url: "https://protected.example/initial.jpg",
        expires_at: "2026-08-07T12:02:00Z"
      })
      .mockRejectedValueOnce(new Error("media refresh failed"))
      .mockResolvedValueOnce({
        url: "https://protected.example/refreshed.jpg",
        expires_at: "2026-08-07T12:03:00Z"
      })
      .mockImplementation(() => new Promise(() => {}));
    await act(async () => root.render(
      <WorkViewer video={pendingVideo()} token="owner-token" onClose={() => {}} />
    ));
    await flush();
    await act(async () => vi.advanceTimersByTime(10_000));
    await flush();
    expect(fetchProtectedAssetAccess).toHaveBeenCalledTimes(3);
    await act(async () => vi.advanceTimersByTime(4_000));
    expect(fetchProtectedAssetAccess).toHaveBeenCalledTimes(3);
    await act(async () => vi.advanceTimersByTime(1_000));
    expect(fetchProtectedAssetAccess).toHaveBeenCalledTimes(4);
  });

  it("clears protected URLs when the owner token disappears", async () => {
    vi.mocked(fetchProtectedAssetAccess)
      .mockResolvedValueOnce({
        url: "https://protected.example/baseline.mp4",
        expires_at: "2099-01-01T00:00:00Z"
      })
      .mockResolvedValueOnce({
        url: "https://protected.example/cover.jpg",
        expires_at: "2099-01-01T00:00:00Z"
      });
    const video = pendingVideo();
    await act(async () => root.render(
      <WorkViewer video={video} token="owner-token" onClose={() => {}} />
    ));
    await flush();
    expect(container.querySelector<HTMLVideoElement>("video")?.src)
      .toContain("https://protected.example/baseline.mp4");
    await act(async () => root.render(
      <WorkViewer video={video} onClose={() => {}} />
    ));
    await flush();
    expect(container.querySelector(".work-viewer-stage video")).toBeNull();
    expect(container.innerHTML).not.toContain("protected.example");
  });

  it("reloads a public video when playback retry is activated", async () => {
    const load = vi.spyOn(window.HTMLMediaElement.prototype, "load")
      .mockImplementation(() => {});
    const video = pendingVideo({
      media_url: "https://cdn.example/public.mp4",
      cover_url: "https://cdn.example/cover.jpg"
    });
    await act(async () => root.render(
      <WorkViewer video={video} token="owner-token" onClose={() => {}} />
    ));
    const element = container.querySelector<HTMLVideoElement>("video");
    await act(async () => element?.dispatchEvent(new Event("error")));
    const retry = [...container.querySelectorAll("button")]
      .find((item) => item.textContent === "重新获取预览");
    await act(async () => retry?.click());
    expect(load).toHaveBeenCalledTimes(1);
    expect(fetchProtectedAssetAccess).not.toHaveBeenCalled();
  });
});

function pendingVideo(patch: Partial<Video> = {}): Video {
  return {
    id: 7,
    author_id: 2,
    title: "待审作品",
    description: "",
    media_url: "",
    cover_url: "",
    media_asset_id: 11,
    cover_asset_id: 12,
    media_status: "processing",
    media_error_code: "",
    status: 5,
    visibility: "public",
    like_count: 0,
    comment_count: 0,
    favorite_count: 0,
    created_at: "2026-08-07T00:00:00Z",
    updated_at: "2026-08-07T00:00:00Z",
    ...patch
  };
}

async function flush() {
  for (let index = 0; index < 6; index++) {
    await act(async () => Promise.resolve());
  }
}
