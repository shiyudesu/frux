// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createVideo } from "../api/account";
import { NetworkError } from "../api/client";
import { uploadMediaFile } from "../api/upload";
import type { MediaUploadResult } from "../api/upload";
import type { Video } from "../types";
import { UploadPage } from "./UploadPage";

vi.mock("../api/account", () => ({
  createVideo: vi.fn()
}));

vi.mock("../api/upload", () => ({
  uploadMediaFile: vi.fn()
}));

vi.mock("../router", () => ({
  useNavigate: () => vi.fn()
}));

vi.mock("../session", () => ({
  useSession: () => ({ token: "upload-token", user: { id: 7 } })
}));

describe("upload page validation", () => {
  let container: HTMLDivElement;
  let root: Root;
  let objectURLs: string[];
  let revokedURLs: string[];

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    objectURLs = [];
    revokedURLs = [];
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      value: vi.fn((file: File) => {
        const value = `blob:${file.name}:${objectURLs.length}`;
        objectURLs.push(value);
        return value;
      })
    });
    Object.defineProperty(URL, "revokeObjectURL", {
      configurable: true,
      value: vi.fn((value: string) => revokedURLs.push(value))
    });
  });

  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.clearAllMocks();
  });

  it("rejects a missing title before creating upload sessions", async () => {
    await act(async () => root.render(<UploadPage />));
    const title = container.querySelector<HTMLInputElement>('input[placeholder="输入视频标题"]');
    const description = container.querySelector<HTMLTextAreaElement>(
      'textarea[placeholder="输入视频简介"]'
    );
    expect(title?.required).toBe(true);
    expect(title?.maxLength).toBe(128);
    expect(description?.maxLength).toBe(512);

    const form = container.querySelector("form");
    await act(async () => {
      form?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });

    expect(container.textContent).toContain("请输入视频标题");
    expect(uploadMediaFile).not.toHaveBeenCalled();
    expect(createVideo).not.toHaveBeenCalled();
  });

  it("rejects an oversized UTF-8 title before uploading files", async () => {
    await act(async () => root.render(<UploadPage />));
    const title = container.querySelector<HTMLInputElement>('input[placeholder="输入视频标题"]');
    const setter = Object.getOwnPropertyDescriptor(
      window.HTMLInputElement.prototype,
      "value"
    )?.set;
    await act(async () => {
      setter?.call(title, "中".repeat(43));
      title?.dispatchEvent(new Event("input", { bubbles: true }));
    });
    const form = container.querySelector("form");
    await act(async () => {
      form?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });
    expect(container.textContent).toContain("视频标题过长");
    expect(uploadMediaFile).not.toHaveBeenCalled();
    expect(createVideo).not.toHaveBeenCalled();
  });

  it("previews selected video and cover locally without uploading", async () => {
    await act(async () => root.render(<UploadPage />));
    const inputs = container.querySelectorAll<HTMLInputElement>('input[type="file"]');
    await selectFile(inputs[0], new File(["video"], "preview.mp4", { type: "video/mp4" }));
    await selectFile(inputs[1], new File(["cover"], "cover.jpg", { type: "image/jpeg" }));

    const preview = container.querySelector<HTMLVideoElement>(".preview-frame video");
    expect(preview?.getAttribute("src")).toBe("blob:preview.mp4:0");
    expect(preview?.getAttribute("poster")).toBe("blob:cover.jpg:1");
    expect(preview?.controls).toBe(true);
    expect(preview?.muted).toBe(true);
    expect(container.textContent).toContain("已选封面");
    expect(uploadMediaFile).not.toHaveBeenCalled();

    await act(async () => preview?.dispatchEvent(new Event("error")));
    expect(container.textContent).toContain("浏览器无法预览该本地视频");
  });

  it("submits normally after the user corrects a missing cover", async () => {
    vi.mocked(uploadMediaFile).mockImplementation(async (_file, kind) =>
      kind === "video" ? directUpload(101) : directUpload(202)
    );
    vi.mocked(createVideo).mockResolvedValue(createdVideo());

    await act(async () => root.render(<UploadPage />));
    await setTitle(container, "missing cover retry");
    const inputs = container.querySelectorAll<HTMLInputElement>('input[type="file"]');
    await selectFile(inputs[0], new File(["video"], "preview.mp4", { type: "video/mp4" }));

    await submit(container);
    expect(container.textContent).toContain("请选择封面文件");
    expect(uploadMediaFile).not.toHaveBeenCalled();

    await selectFile(inputs[1], new File(["cover"], "cover.jpg", { type: "image/jpeg" }));
    await submit(container);

    expect(uploadMediaFile).toHaveBeenCalledTimes(2);
    expect(createVideo).toHaveBeenCalledTimes(1);
  });

  it("rejects an oversized cover before either upload starts", async () => {
    await act(async () => root.render(<UploadPage />));
    await setTitle(container, "oversized cover");
    const inputs = container.querySelectorAll<HTMLInputElement>('input[type="file"]');
    await selectFile(inputs[0], new File(["video"], "preview.mp4", { type: "video/mp4" }));
    const cover = new File(["cover"], "cover.png", { type: "image/png" });
    Object.defineProperty(cover, "size", {
      configurable: true,
      value: 20 * 1024 * 1024 + 1
    });
    await selectFile(inputs[1], cover);

    await submit(container);

    expect(container.textContent).toContain("封面不能超过 20 MB");
    expect(uploadMediaFile).not.toHaveBeenCalled();
    expect(createVideo).not.toHaveBeenCalled();
  });

  it("reuses a completed video while retrying only the failed cover", async () => {
    let coverAttempts = 0;
    vi.mocked(uploadMediaFile).mockImplementation(async (_file, kind) => {
      if (kind === "video") return directUpload(101);
      coverAttempts++;
      if (coverAttempts === 1) throw new NetworkError();
      return directUpload(202);
    });
    vi.mocked(createVideo).mockResolvedValue(createdVideo());

    await act(async () => root.render(<UploadPage />));
    await setTitle(container, "partial retry");
    const inputs = container.querySelectorAll<HTMLInputElement>('input[type="file"]');
    await selectFile(inputs[0], new File(["video"], "preview.mp4", { type: "video/mp4" }));
    await selectFile(inputs[1], new File(["cover"], "cover.jpg", { type: "image/jpeg" }));

    await submit(container);
    expect(container.textContent).toContain("网络连接失败");
    await submit(container);

    const uploadKinds = vi.mocked(uploadMediaFile).mock.calls.map((call) => call[1]);
    expect(uploadKinds).toEqual(["video", "cover", "cover"]);
    expect(createVideo).toHaveBeenCalledTimes(1);
  });

  it("preserves the completed video when the failed cover is replaced", async () => {
    const coverAttemptIDs: string[] = [];
    vi.mocked(uploadMediaFile).mockImplementation(async (_file, kind, _token, attemptID) => {
      if (kind === "video") return directUpload(101);
      coverAttemptIDs.push(attemptID);
      if (coverAttemptIDs.length === 1) throw new NetworkError();
      return directUpload(202);
    });
    vi.mocked(createVideo).mockResolvedValue(createdVideo());

    await act(async () => root.render(<UploadPage />));
    await setTitle(container, "replace failed cover");
    const inputs = container.querySelectorAll<HTMLInputElement>('input[type="file"]');
    await selectFile(inputs[0], new File(["video"], "preview.mp4", { type: "video/mp4" }));
    await selectFile(inputs[1], new File(["bad"], "first.jpg", { type: "image/jpeg" }));
    await submit(container);

    await selectFile(inputs[1], new File(["good"], "second.jpg", { type: "image/jpeg" }));
    await submit(container);

    const uploadKinds = vi.mocked(uploadMediaFile).mock.calls.map((call) => call[1]);
    expect(uploadKinds).toEqual(["video", "cover", "cover"]);
    expect(coverAttemptIDs[1]).not.toBe(coverAttemptIDs[0]);
  });

  it("reuses both uploaded assets and the creation key after a transient create failure", async () => {
    vi.mocked(uploadMediaFile).mockImplementation(async (_file, kind) =>
      kind === "video" ? directUpload(101) : directUpload(202)
    );
    vi.mocked(createVideo)
      .mockRejectedValueOnce(new NetworkError())
      .mockResolvedValueOnce(createdVideo());

    await act(async () => root.render(<UploadPage />));
    await setTitle(container, "create retry");
    const inputs = container.querySelectorAll<HTMLInputElement>('input[type="file"]');
    await selectFile(inputs[0], new File(["video"], "preview.mp4", { type: "video/mp4" }));
    await selectFile(inputs[1], new File(["cover"], "cover.jpg", { type: "image/jpeg" }));

    await submit(container);
    await submit(container);

    expect(uploadMediaFile).toHaveBeenCalledTimes(2);
    expect(createVideo).toHaveBeenCalledTimes(2);
    expect(vi.mocked(createVideo).mock.calls[1][2]).toBe(vi.mocked(createVideo).mock.calls[0][2]);
  });

  it("revokes replaced and unmounted local preview URLs", async () => {
    await act(async () => root.render(<UploadPage />));
    const inputs = container.querySelectorAll<HTMLInputElement>('input[type="file"]');
    await selectFile(inputs[0], new File(["first"], "first.mp4", { type: "video/mp4" }));
    await selectFile(inputs[1], new File(["cover"], "cover.jpg", { type: "image/jpeg" }));
    await selectFile(inputs[0], new File(["second"], "second.mp4", { type: "video/mp4" }));
    expect(revokedURLs).toContain("blob:first.mp4:0");

    await act(async () => root.unmount());
    expect(revokedURLs).toContain("blob:second.mp4:2");
    expect(revokedURLs).toContain("blob:cover.jpg:1");
    root = createRoot(container);
  });
});

async function selectFile(input: HTMLInputElement, file: File) {
  Object.defineProperty(input, "files", {
    configurable: true,
    value: [file]
  });
  await act(async () => input.dispatchEvent(new Event("change", { bubbles: true })));
}

async function setTitle(container: HTMLDivElement, value: string) {
  const title = container.querySelector<HTMLInputElement>('input[placeholder="输入视频标题"]');
  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, "value")?.set;
  await act(async () => {
    setter?.call(title, value);
    title?.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

async function submit(container: HTMLDivElement) {
  await act(async () => {
    container.querySelector("form")?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  });
}

function directUpload(assetID: number): MediaUploadResult {
  return { mode: "direct", assetID };
}

function createdVideo(): Video {
  return {
    id: 1,
    author_id: 1,
    title: "uploaded",
    description: "",
    media_url: "",
    cover_url: "",
    status: 5,
    visibility: "public",
    like_count: 0,
    comment_count: 0,
    favorite_count: 0,
    created_at: "2026-08-07T00:00:00Z",
    updated_at: "2026-08-07T00:00:00Z",
    media_status: "processing"
  };
}
