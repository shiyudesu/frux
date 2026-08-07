// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createVideo } from "../api/account";
import { uploadMediaFile } from "../api/upload";
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
  useSession: () => ({ token: "upload-token" })
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
